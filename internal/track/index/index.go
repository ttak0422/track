// Package index keeps the SQLite store in sync with the notes on disk: parsing sidecar metadata into rows and computing the auto-link graph.
package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/journal"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	tmpl "github.com/ttak0422/track/internal/track/template"
	"github.com/ttak0422/track/internal/track/vaultfile"
)

type Indexer struct {
	cfg     *config.Config
	store   *store.Store
	readDir func(string) ([]os.DirEntry, error)
}

func New(cfg *config.Config, s *store.Store) *Indexer {
	return &Indexer{cfg: cfg, store: s, readDir: os.ReadDir}
}

// Report summarizes a rebuild. Partial data is explicitly described, even on a first run.
type Report struct {
	Indexed   int              `json:"indexed"`
	Deleted   int              `json:"deleted"`
	Links     int              `json:"links"`
	Skipped   int              `json:"skipped,omitempty"`
	ReadState *store.ReadState `json:"read_state,omitempty"`
}

// Full updates accessible files and retains unverified cached data. It never infers
// deletion from directory absence; ReconcileMissing is the explicit cleanup path.
func (ix *Indexer) Full() (Report, error) { return ix.full(false, nil) }

// FullStrict is a mutation precondition: incomplete backlink/namespace information
// must not authorize a rename, move, or published export.
func (ix *Indexer) FullStrict() (Report, error) {
	rep, err := ix.Full()
	if err == nil && rep.ReadState != nil {
		err = fmt.Errorf("index is partial; verify unavailable files before this operation")
	}
	return rep, err
}

// ReconcileMissing requires explicit confirmation of intentional external deletions.
// Any failed read/listing prevents all pruning and preserves the previous cache.
func (ix *Indexer) ReconcileMissing() (Report, error) { return ix.full(true, nil) }

func (ix *Indexer) full(prune bool, activity map[int64]int64) (Report, error) {
	var rep Report
	// An invalid content root is a vault-wide failure, not one unreadable item.
	roots := []string{ix.cfg.VaultDir, ix.cfg.NoteDir()}
	if !ix.cfg.JournalOff {
		roots = append(roots, ix.cfg.JournalDir())
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if os.IsNotExist(err) && root != ix.cfg.VaultDir {
			continue // A new vault may not have created either content directory yet.
		}
		if err != nil {
			return rep, fmt.Errorf("inspect vault directory: %w", err)
		}
		if !info.IsDir() {
			return rep, fmt.Errorf("vault content root is not a directory: %s", root)
		}
	}
	state := store.ReadState{CheckedAt: time.Now().UnixMilli()}
	existing, err := ix.store.NoteMtimes()
	if err != nil {
		return rep, err
	}
	metaTimes, err := ix.store.NoteMetaMtimes()
	if err != nil {
		return rep, err
	}
	cachedRefs, err := ix.store.AllNotes()
	if err != nil {
		return rep, err
	}
	expectedRoots := map[string]bool{}
	cachedPaths := map[int64]string{}
	for _, ref := range cachedRefs {
		cachedPaths[ref.NoteID] = ix.cfg.PathForKind(ref.FileKind, ref.NoteID)
		expectedRoots[filepath.Dir(cachedPaths[ref.NoteID])] = true
	}
	paths, scanIssues := ix.scanReadableFiles(expectedRoots)
	for _, issue := range scanIssues {
		state.Add(issue)
	}
	seen := map[int64]bool{}
	notes := make([]*note.Note, 0, len(paths))
	for _, path := range paths {
		id, idErr := note.IDFromPath(path)
		if idErr == nil {
			seen[id] = true
		}
		_, cached := existing[id]
		reject := func(reason string) {
			rep.Skipped++
			state.Add(store.FileReadIssue(id, path, ix.cfg.MetadataPath(id), reason, cached))
		}
		// Missing metadata for a known sidecar is unavailable data, not an empty title.
		if metaTimes[id] != 0 {
			if _, err := os.Stat(ix.cfg.MetadataPath(id)); err != nil {
				reject("metadata unavailable: " + err.Error())
				continue
			}
		}
		n, err := note.ReadFile(path, ix.cfg)
		if err != nil {
			var invalid *note.ParseError
			var local *vaultfile.Error
			if !errors.As(err, &invalid) && !errors.As(err, &local) {
				return rep, err
			}
			if invalid != nil {
				warnSkipped(invalid)
			} else {
				vaultfile.Warn(local.Path, local.Reason)
			}
			reject(err.Error())
			continue
		}
		if metaTimes[id] != 0 && n.MetaMtime == 0 {
			reject("known metadata disappeared during read")
			continue
		}
		if err := config.ValidateLinkTitle(ix.cfg.LinkFormat, n.Meta.Title); err != nil {
			return rep, err
		}
		// Activity writes are made only after both body and metadata were read. A write
		// failure stays fatal and never gets misclassified as an unavailable input.
		if _, changed := activity[id]; changed && n.Kind != config.KindJournal {
			if err := ix.recordActivity(map[int64]int64{id: n.Mtime}); err != nil {
				return rep, err
			}
			// Reparse after the activity write so the cache matches the exact saved metadata.
			n, err = note.ReadFile(path, ix.cfg)
			if err != nil {
				var invalid *note.ParseError
				var local *vaultfile.Error
				if !errors.As(err, &invalid) && !errors.As(err, &local) {
					return rep, err
				}
				reject(err.Error())
				continue
			}
			if metaTimes[id] != 0 && n.MetaMtime == 0 {
				reject("known metadata disappeared during read")
				continue
			}
		}
		vaultfile.Recovered(path)
		vaultfile.Recovered(ix.cfg.MetadataPath(id))
		notes = append(notes, n)
	}
	var missing []int64
	for id := range existing {
		if !seen[id] {
			missing = append(missing, id)
			if !prune {
				state.Add(store.ReadIssue{NoteID: id, Path: cachedPaths[id], Reason: "file absent from scan; deletion not confirmed", Cached: true})
			}
		}
	}
	slices.Sort(missing)
	if prune && state.Partial {
		return rep, fmt.Errorf("refusing to prune an incomplete scan")
	}
	if prune {
		// Recheck the absent IDs against the scanned baseline, not rows concurrently
		// added after enumeration. A disappeared root cannot confirm a deletion.
		for _, id := range missing {
			path := cachedPaths[id]
			if _, err := os.ReadDir(filepath.Dir(path)); err != nil {
				return rep, fmt.Errorf("refusing to prune unavailable directory: %w", err)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return rep, fmt.Errorf("missing file changed during pruning check: %s", path)
			}
		}
	}
	// A link-format change cannot safely mix incompatible cached dictionaries.
	previousFormat, err := ix.store.IndexedLinkFormat()
	if err != nil {
		return rep, err
	}
	initialized, err := ix.store.Initialized()
	if err != nil {
		return rep, err
	}
	if state.Partial && initialized && previousFormat != ix.cfg.LinkFormat {
		return rep, fmt.Errorf("cannot change link format with an incomplete index")
	}
	err = ix.store.Atomic(func(tx *store.Store) error {
		for _, n := range notes {
			if err := tx.UpsertNote(n); err != nil {
				return err
			}
			rep.Indexed++
		}
		if prune {
			for _, id := range missing {
				if err := tx.DeleteNote(id); err != nil {
					return err
				}
				rep.Deleted++
			}
		}
		indexed := New(ix.cfg, tx)
		dict, err := indexed.keywordDict()
		if err != nil {
			return err
		}
		for _, n := range notes {
			targets, ext := indexed.resolveLinks(n.Body, dict)
			if err := tx.ReplaceLinks(n.ID, targets); err != nil {
				return err
			}
			if err := tx.ReplaceExtLinks(n.ID, ext); err != nil {
				return err
			}
			rep.Links += countExcludingSelf(targets, n.ID)
		}
		if err := tx.SetIndexedLinkFormat(ix.cfg.LinkFormat); err != nil {
			return err
		}
		return tx.SetReadState(state)
	})
	if err != nil {
		return Report{}, err
	}
	if state.Partial {
		rep.ReadState = &state
	}
	return rep, nil
}

// RefreshIfStale keeps the existing API while publishing read status in the
// store. A partial refresh retries on the next request, even if mtimes agree.
func (ix *Indexer) RefreshIfStale() (bool, error) {
	if initialized, err := ix.EnsureInitialized(); initialized || err != nil {
		return initialized, err
	}
	previous, err := ix.store.ReadState()
	if err != nil {
		return false, err
	}

	format, err := ix.store.IndexedLinkFormat()
	if err != nil {
		return false, err
	}
	disk, journals, skipped, scanErr := ix.scanMtimes()
	indexed, err := ix.store.NoteMtimes()
	if err != nil {
		return false, err
	}
	metaIndexed, err := ix.store.NoteMetaMtimes()
	if err != nil {
		return false, err
	}
	// Retry unchanged failures at most once per 30 seconds. Healthy file changes
	// still trigger an immediate refresh, while retained rows stay explicitly stale.
	if previous.Partial && time.Now().UnixMilli()-previous.CheckedAt < 30000 && format == ix.cfg.LinkFormat && scanErr == nil {
		compareDisk, compareIndex := cloneMtimes(disk), cloneMtimes(indexed)
		compareMeta := ix.scanMetaMtimes(disk)
		retryChangedFailure := false
		for _, issue := range previous.Issues {
			if issue.NoteID != 0 && (disk[issue.NoteID] != issue.BodyMtime || compareMeta[issue.NoteID] != issue.MetaMtime) {
				retryChangedFailure = true
				break
			}
		}
		for _, issue := range previous.Issues {
			if issue.NoteID != 0 {
				delete(compareDisk, issue.NoteID)
				delete(compareIndex, issue.NoteID)
				delete(compareMeta, issue.NoteID)
				delete(metaIndexed, issue.NoteID)
			}
		}
		if !retryChangedFailure && sameMtimes(compareDisk, compareIndex) && sameMtimes(compareMeta, metaIndexed) {
			return false, nil
		}
	}
	if !previous.Partial && format == ix.cfg.LinkFormat && scanErr == nil && sameMtimes(disk, indexed) && sameMtimes(ix.scanMetaMtimes(disk), metaIndexed) && len(skipped) == 0 {
		return false, nil
	}
	changed := map[int64]int64{}
	for id, m := range disk {
		if !journals[id] {
			if im, ok := indexed[id]; !ok || im != m {
				changed[id] = m
			}
		}
	}
	_, err = ix.full(false, changed)
	return err == nil, err
}

// scanReadableFiles keeps a failed directory separate from the directories that
// can still answer. Missing directories are normal only when no cached IDs need them.
func (ix *Indexer) scanReadableFiles(expected map[string]bool) ([]string, []store.ReadIssue) {
	var paths []string
	var issues []store.ReadIssue
	for _, root := range []string{ix.cfg.NoteDir(), ix.cfg.JournalDir()} {
		entries, err := ix.readDir(root)
		if err != nil {
			if !os.IsNotExist(err) || expected[root] {
				issues = append(issues, store.ReadIssue{Path: root, Reason: err.Error()})
			}
			continue
		}
		for _, entry := range entries {
			if slices.Contains(ix.cfg.Extensions, filepath.Ext(entry.Name())) {
				paths = append(paths, filepath.Join(root, entry.Name()))
			}
		}
	}
	slices.Sort(paths)
	return paths, issues
}

// activityDay formats a file mtime as the local calendar day used for activity tracking, matching the
// format Created is stamped with so day strings compare consistently across the sidecar and index.
func (ix *Indexer) activityDay(mtime int64) string {
	return time.Unix(mtime, 0).In(time.Local).Format(ix.cfg.DateFormat)
}

// recordActivity stamps each note's mtime day into its sidecar Days set, writing the sidecar only when the
// day was not already recorded. Notes without a sidecar yet are skipped; Full creates one for them.
func (ix *Indexer) recordActivity(ids map[int64]int64) error {
	for id, mtime := range ids {
		path := ix.cfg.MetadataPath(id)
		meta, found, err := note.ReadMetadata(path)
		if err != nil {
			var invalid *note.ParseError
			if errors.As(err, &invalid) {
				continue // Full reports this file's parse failure in deterministic path order.
			}
			return err
		}
		if !found {
			continue
		}
		updated, changed := note.EnsureDay(meta, ix.activityDay(mtime))
		if !changed {
			continue
		}
		if err := note.WriteMetadata(path, updated); err != nil {
			return err
		}
	}
	return nil
}

// scanMetaMtimes maps note id -> sidecar mtime (0 when absent) for exactly the notes present on
// disk, mirroring how ParseFile records Note.MetaMtime. Restricting to disk ids keeps orphan
// sidecars out of the staleness comparison.
func (ix *Indexer) scanMetaMtimes(disk map[int64]int64) map[int64]int64 {
	out := make(map[int64]int64, len(disk))
	for id := range disk {
		out[id] = 0
		if fi, err := os.Stat(ix.cfg.MetadataPath(id)); err == nil {
			out[id] = fi.ModTime().Unix()
		}
	}
	return out
}

// scanMtimes maps note id -> file mtime (Unix seconds) for every note/journal file, mirroring how
// UpsertNote records mtime. Files whose basename is not a numeric note id are skipped, matching the ids
// the indexer assigns; reading mtimes never parses a file.
func (ix *Indexer) scanMtimes() (map[int64]int64, map[int64]bool, []*note.ParseError, error) {
	out := map[int64]int64{}
	journals := map[int64]bool{}
	var skipped []*note.ParseError
	paths, err := ix.scanFiles()
	if err != nil {
		return nil, nil, nil, err
	}
	for _, path := range paths {
		id, err := note.IDFromPath(path)
		if err != nil {
			skipped = append(skipped, &note.ParseError{Path: path, Err: fmt.Errorf("invalid note filename (expected a numeric id): %w", err)})
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, nil, err
		}
		out[id] = info.ModTime().Unix()
		if filepath.Dir(path) == ix.cfg.JournalDir() {
			journals[id] = true
		}
	}
	return out, journals, skipped, nil
}

func warnSkipped(err *note.ParseError) {
	reason := "invalid note metadata"
	if _, idErr := note.IDFromPath(err.Path); idErr != nil && filepath.Ext(err.Path) != ".yaml" {
		reason = "invalid note filename"
	}
	vaultfile.Warn(err.Path, reason)
}

func sameMtimes(disk, indexed map[int64]int64) bool {
	if len(disk) != len(indexed) {
		return false
	}
	for id, m := range disk {
		if im, ok := indexed[id]; !ok || im != m {
			return false
		}
	}
	return true
}

// One updates a single note and recomputes its outgoing links.
// Inbound links from other notes to this one are not refreshed here; run Full for that.
func (ix *Indexer) One(path string) error {
	if _, err := ix.EnsureInitialized(); err != nil {
		return err
	}
	format, err := ix.store.IndexedLinkFormat()
	if err != nil {
		return err
	}
	if format != ix.cfg.LinkFormat {
		if _, err := ix.FullStrict(); err != nil {
			return err
		}
	}
	id, idErr := note.IDFromPath(path)
	if idErr != nil {
		return idErr
	}
	if err := ix.store.RequireMetadata(id, ix.cfg.MetadataPath(id)); err != nil {
		return err
	}
	n, err := note.ReadFile(path, ix.cfg)
	if err != nil {
		return err
	}
	if err := config.ValidateLinkTitle(ix.cfg.LinkFormat, n.Meta.Title); err != nil {
		return err
	}
	knownMeta, err := ix.store.NoteMetaMtimes()
	if err != nil {
		return err
	}
	if knownMeta[n.ID] != 0 && n.MetaMtime == 0 {
		return fmt.Errorf("known metadata disappeared during read")
	}
	// A track mutation command (new/append/toggle/journal) just wrote this file, so stamp its mtime day
	// into the sidecar. The editor-edit path is handled separately by RefreshIfStale. Journals carry no
	// activity day (see note.ActivityDays), so stamping would only write dead metadata.
	if n.Kind != "journal" {
		if meta, changed := note.EnsureDay(n.Meta, ix.activityDay(n.Mtime)); changed {
			metaPath := ix.cfg.MetadataPath(n.ID)
			if err := note.WriteMetadata(metaPath, meta); err != nil {
				return err
			}
			n.Meta = meta
			// Re-stat so the stored sidecar mtime matches the write above; a stale value would make
			// the next RefreshIfStale see a phantom sidecar change and rebuild for nothing.
			if fi, err := os.Stat(metaPath); err == nil {
				n.MetaMtime = fi.ModTime().Unix()
			}
		}
	}
	if err := ix.store.UpsertNote(n); err != nil {
		return err
	}
	dict, err := ix.keywordDict()
	if err != nil {
		return err
	}
	targets, ext := ix.resolveLinks(n.Body, dict)
	if err := ix.store.ReplaceLinks(n.ID, targets); err != nil {
		return err
	}
	if err := ix.store.ReplaceExtLinks(n.ID, ext); err != nil {
		return err
	}
	// A note's activity day implies its journal exists: editing or creating a note (via any path that
	// reaches here — CLI, the LSP's didSave for nvim, or a web save) ensures that day's journal so it is
	// the aggregation hub for the day. Journals never trigger this, which also prevents recursion.
	if n.Kind != "journal" {
		return ix.ensureDayJournal(n.Mtime)
	}
	return nil
}

// ensureDayJournal makes sure the journal for the local day of mtime exists, creating it with the
// configured journal template (builtin default when unset) and indexing whatever journal.Open reports as
// changed. It is a no-op once the day's journal and summaries are in place.
func (ix *Indexer) ensureDayJournal(mtime int64) error {
	// A vault with journals turned off has no day hub to ensure; indexing carries on regardless.
	if ix.cfg.JournalOff {
		return nil
	}
	res, err := journal.Open(ix.cfg, time.Unix(mtime, 0), journal.Options{
		CreateBody: func(name string, id int64, d time.Time) (string, error) {
			spec, err := tmpl.DefaultSpec(ix.cfg, config.KindJournal)
			if err != nil {
				return "", err
			}
			if spec == "" {
				return "", nil
			}
			return tmpl.Render(ix.cfg, spec, name, id, config.KindJournal, "", d)
		},
	})
	if err != nil {
		return err
	}
	for _, p := range res.Reindex {
		if err := ix.One(p); err != nil {
			return err
		}
	}
	return nil
}

// keywordDict loads the auto-link dictionary once as term -> note id, so resolving each [[...]] is an O(1) map lookup.
func (ix *Indexer) keywordDict() (map[string]int64, error) {
	kws, err := ix.store.LinkKeywords(ix.cfg.LinkFormat)
	if err != nil {
		return nil, err
	}
	dict := make(map[string]int64, len(kws))
	for _, k := range kws {
		if _, ok := dict[k.Term]; !ok {
			dict[k.Term] = k.NoteID
		}
	}
	// A note may qualify a reference with its own vault's name — the active vault is in its own
	// registry (ADR 0051). That is not a cross-vault reference, so "self:Term" is registered as
	// another spelling of the local title: it resolves through the ordinary link path and gets a real
	// links row, visible to backlinks and the graph. A note literally titled that way keeps
	// precedence, hence the second pass.
	if self := ix.cfg.VaultName; self != "" {
		for _, k := range kws {
			key := self + ":" + k.Term
			if _, ok := dict[key]; !ok {
				dict[key] = k.NoteID
			}
		}
	}
	return dict, nil
}

// resolveLinks returns the deduplicated note ids referenced by body's [[...]] links, in first-seen
// order, plus the cross-vault references ([[vault:title]], gated on the *other* registered vault
// names). The qualifier gate runs before the local dictionary so a registered name always reads as
// a qualifier — doctor lints local titles that shadowing affects. The active vault's own name is
// not a qualifier: it falls through to the dictionary, which carries the "self:Title" spelling
// (keywordDict), so the edge is an ordinary local link. Unresolved local references (no matching
// title) are skipped.
func (ix *Indexer) resolveLinks(body string, dict map[string]int64) ([]int64, []store.ExtRef) {
	isVault := func(name string) bool {
		if name == ix.cfg.VaultName {
			return false
		}
		_, ok := ix.cfg.Vaults[name]
		return ok
	}
	var ids []int64
	var ext []store.ExtRef
	seen := make(map[int64]bool)
	seenExt := make(map[store.ExtRef]bool)
	for _, ref := range link.Refs(body) {
		if vault, title, ok := link.SplitVaultRef(ref.Text, isVault); ok {
			key, valid := store.NewExtRef(vault, title, ix.cfg.LinkFormat)
			if valid && !seenExt[key] {
				seenExt[key] = true
				ext = append(ext, key)
			}
			continue
		}
		id, ok := dict[ref.Text]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, ext
}

func (ix *Indexer) scanFiles() ([]string, error) {
	var out []string
	for _, root := range []string{ix.cfg.NoteDir(), ix.cfg.JournalDir()} {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue // note/journal dir may not exist yet
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && slices.Contains(ix.cfg.Extensions, filepath.Ext(entry.Name())) {
				out = append(out, filepath.Join(root, entry.Name()))
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

func countExcludingSelf(ids []int64, self int64) int {
	n := 0
	for _, id := range ids {
		if id != self {
			n++
		}
	}
	return n
}

func cloneMtimes(in map[int64]int64) map[int64]int64 {
	out := make(map[int64]int64, len(in))
	for id, m := range in {
		out[id] = m
	}
	return out
}
