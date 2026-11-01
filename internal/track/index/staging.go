package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/vaultfile"
)

const StageBudget = 5 * time.Second

var ErrSuperseded = errors.New("staged index superseded by a newer mutation")

type Snapshot struct {
	Notes    []*note.Note
	State    store.ReadState
	Revision int64
	Activity map[int64]int64
}

// Stage does no writes. The callback exposes verified cold-cache items while
// other files are pending; it cannot certify a complete namespace or authorize edits.
func (ix *Indexer) Stage(ctx context.Context, progress func(*note.Note)) (*Snapshot, error) {
	var revision int64
	var err error
	existing, metaTimes := map[int64]int64{}, map[int64]int64{}
	var refs []store.NoteRef
	initialized := false
	if ix.store != nil {
		revision, err = ix.store.Revision()
		if err != nil {
			return nil, err
		}
		existing, err = ix.store.NoteMtimes()
		if err != nil {
			return nil, err
		}
		metaTimes, err = ix.store.NoteMetaMtimes()
		if err != nil {
			return nil, err
		}
		refs, err = ix.store.AllNotes()
		if err != nil {
			return nil, err
		}
		initialized, err = ix.store.Initialized()
		if err != nil {
			return nil, err
		}
	}
	snap := &Snapshot{Revision: revision, Activity: map[int64]int64{}, State: store.ReadState{CheckedAt: time.Now().UnixMilli()}}
	if info, err := vaultfile.StatContext(ctx, ix.cfg.VaultDir); err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, errors.New("vault root is not a directory")
	}
	var paths []string
	seen := map[int64]bool{}
	for _, root := range []string{ix.cfg.NoteDir(), ix.cfg.JournalDir()} {
		if info, err := vaultfile.StatContext(ctx, root); err == nil && !info.IsDir() {
			return nil, fmt.Errorf("vault content root is not a directory: %s", root)
		}
		entries, err := vaultfile.ReadDirContext(ctx, root)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				snap.State.Add(store.ReadIssue{Path: root, Reason: err.Error()})
			}
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && slices.Contains(ix.cfg.Extensions, filepath.Ext(entry.Name())) {
				path := filepath.Join(root, entry.Name())
				paths = append(paths, path)
				if id, err := note.IDFromPath(path); err == nil {
					seen[id] = true
				}
			}
		}
	}
	slices.SortFunc(paths, func(a, b string) int {
		ai, _ := note.IDFromPath(a)
		bi, _ := note.IDFromPath(b)
		ak := ix.previous[ai] != nil
		bk := ix.previous[bi] != nil
		if ak != bk {
			if !ak {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	type result struct {
		path string
		n    *note.Note
		err  error
	}
	scanCtx, stopScan := context.WithCancel(ctx)
	defer stopScan()
	stagedBytes := 0
	jobs := make(chan string)
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				itemCtx, cancel := context.WithTimeout(scanCtx, vaultfile.ReadTimeout)
				id, _ := note.IDFromPath(path)
				if previous := ix.previous[id]; previous != nil {
					err := vaultfile.VerifyNote(itemCtx, path, ix.cfg.MetadataPath(id), previous)
					if err == nil {
						cancel()
						select {
						case results <- result{path: path}:
						case <-scanCtx.Done():
							return
						}
						continue
					}
					if !vaultfile.InputChanged(err) {
						cancel()
						select {
						case results <- result{path: path, err: err}:
						case <-scanCtx.Done():
							return
						}
						continue
					}
				}
				n, err := ix.readNote(itemCtx, path, ix.cfg)
				cancel()
				select {
				case results <- result{path, n, err}:
				case <-scanCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, path := range paths {
			select {
			case jobs <- path:
			case <-scanCtx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	for item := range results {
		id, _ := note.IDFromPath(item.path)
		_, cached := existing[id]
		if item.n == nil && item.err == nil {
			continue
		}
		if item.err == nil && metaTimes[id] != 0 && item.n.MetaMtime == 0 {
			item.err = fmt.Errorf("known metadata unavailable")
		}
		if item.err == nil {
			item.err = config.ValidateLinkTitle(ix.cfg.LinkFormat, item.n.Meta.Title)
		}
		if item.err != nil {
			snap.State.Add(store.ReadIssue{NoteID: id, Path: item.path, Reason: item.err.Error(), Cached: cached, BodyMtime: existing[id], MetaMtime: metaTimes[id]})
			continue
		}
		if initialized && existing[id] != item.n.Mtime && item.n.Kind != config.KindJournal && item.n.Snapshot.Found {
			snap.Activity[id] = item.n.Mtime
		}
		size := len(item.n.Body) + len(item.n.Snapshot.Metadata) + len(item.n.Tasks)*512
		if stagedBytes+size > 64<<20 || len(snap.Notes) >= 10000 {
			snap.State.Add(store.ReadIssue{Path: ix.cfg.VaultDir, Reason: "staging budget reached; remaining files unverified", Cached: len(existing) > 0})
			stopScan()
			continue
		}
		stagedBytes += size
		snap.Notes = append(snap.Notes, item.n)
		if progress != nil && ctx.Err() == nil {
			progress(item.n)
		}
	}
	if ctx.Err() != nil {
		snap.State.Add(store.ReadIssue{Path: ix.cfg.VaultDir, Reason: "read pass deadline reached; remaining files unverified", Cached: len(existing) > 0})
	}
	for _, ref := range refs {
		if !seen[ref.NoteID] {
			snap.State.Add(store.ReadIssue{NoteID: ref.NoteID, Path: ix.cfg.PathForKind(ref.FileKind, ref.NoteID), Reason: "file not verified by enumeration; deletion not confirmed", Cached: true})
		}
	}
	return snap, nil
}

// Publish writes only the derived cache, never sidecars or activity dates. No
// filesystem call occurs inside its transaction, and incomplete passes never prune.
func (ix *Indexer) Publish(snap *Snapshot) error {
	return ix.store.Atomic(func(tx *store.Store) error {
		revision, err := tx.Revision()
		if err != nil {
			return err
		}
		if revision != snap.Revision {
			return ErrSuperseded
		}
		oldFormat, err := tx.IndexedLinkFormat()
		if err != nil {
			return err
		}
		initialized, err := tx.Initialized()
		if err != nil {
			return err
		}
		if initialized && oldFormat != ix.cfg.LinkFormat && snap.State.Partial {
			return errors.New("cannot change link format with an incomplete index")
		}
		for _, n := range snap.Notes {
			if err := tx.UpsertNote(n); err != nil {
				return err
			}
		}
		dict, err := New(ix.cfg, tx).keywordDict()
		if err != nil {
			return err
		}
		for _, n := range snap.Notes {
			targets, external := ix.resolveLinks(n.Body, dict)
			if err := tx.ReplaceLinks(n.ID, targets); err != nil {
				return err
			}
			if err := tx.ReplaceExtLinks(n.ID, external); err != nil {
				return err
			}
		}
		if err := tx.SetIndexedLinkFormat(ix.cfg.LinkFormat); err != nil {
			return err
		}
		return tx.SetReadState(snap.State)
	})
}

// ApplyActivity is an optional parent-owned mutation after successful read staging.
// It checks current input versions before touching sidecars and is never killed as a read.
func (ix *Indexer) ApplyActivity(ctx context.Context, snap *Snapshot) error {
	revision, err := ix.store.Revision()
	if err != nil {
		return err
	}
	if revision != snap.Revision {
		return ErrSuperseded
	}
	cached, err := ix.store.NoteMtimes()
	if err != nil {
		return err
	}
	verified := make([]*note.Note, 0, len(snap.Notes))
	writeUnsafe := false
	for _, n := range snap.Notes {
		reject := func(err error) {
			_, had := cached[n.ID]
			snap.State.Add(store.ReadIssue{NoteID: n.ID, Path: n.Path, Reason: err.Error(), Cached: had})
		}
		if err := vaultfile.VerifyNote(ctx, n.Path, ix.cfg.MetadataPath(n.ID), n.Snapshot); err != nil {
			reject(err)
			continue
		}
		if _, changed := snap.Activity[n.ID]; changed && !snap.State.Partial && !ix.cfg.Provisional {
			if writeUnsafe {
				reject(errors.New("activity update deferred after a write failure"))
				verified = append(verified, n)
				continue
			}
			current, found, err := note.ReadMetadata(ix.cfg.MetadataPath(n.ID))
			if err != nil {
				reject(err)
				continue
			}
			if !found || !reflect.DeepEqual(current, n.Meta) {
				reject(ErrSuperseded)
				continue
			}
			updated, changed := note.EnsureDay(current, ix.activityDay(n.Mtime))
			if changed {
				if err := note.WriteMetadataAtomic(ix.cfg.MetadataPath(n.ID), current, updated); err != nil {
					reject(fmt.Errorf("activity metadata write failed: %w", err))
					writeUnsafe = true
					continue
				}
				info, err := vaultfile.StatContext(ctx, ix.cfg.MetadataPath(n.ID))
				if err != nil {
					reject(err)
					continue
				}
				n.Meta = updated
				n.MetaMtime = info.ModTime().Unix()
			}
		}
		verified = append(verified, n)
	}
	snap.Notes = verified
	return nil
}

func (ix *Indexer) WithPrevious(previous map[int64]*vaultfile.NoteRead) *Indexer {
	ix.previous = previous
	return ix
}
