// Package index keeps the SQLite store in sync with the notes on disk: parsing sidecar metadata into rows and computing the auto-link graph.
package index

import (
	"io/fs"
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
)

type Indexer struct {
	cfg   *config.Config
	store *store.Store
}

func New(cfg *config.Config, s *store.Store) *Indexer {
	return &Indexer{cfg: cfg, store: s}
}

// Report summarizes a reindex run.
type Report struct {
	Indexed int `json:"indexed"`
	Deleted int `json:"deleted"`
	Links   int `json:"links"`
}

// Full re-parses every note in the vault, reconciles deletions, and recomputes the entire link graph.
// It is the authoritative rebuild; single-file updates can't see new inbound links, so use Full for
// bulk repair or when previously unresolved links should become backlinks after creating a title.
func (ix *Indexer) Full() (Report, error) {
	var rep Report

	paths, err := ix.scanFiles()
	if err != nil {
		return rep, err
	}

	existing, err := ix.store.NoteMtimes()
	if err != nil {
		return rep, err
	}

	notes := make([]*note.Note, 0, len(paths))
	seen := make(map[int64]bool, len(paths))
	for _, p := range paths {
		n, err := note.ParseFile(p, ix.cfg)
		if err != nil {
			return rep, err
		}
		if err := ix.store.UpsertNote(n); err != nil {
			return rep, err
		}
		notes = append(notes, n)
		seen[n.ID] = true
		rep.Indexed++
	}

	for id := range existing {
		if !seen[id] {
			if err := ix.store.DeleteNote(id); err != nil {
				return rep, err
			}
			if err := os.Remove(ix.cfg.MetadataPath(id)); err != nil && !os.IsNotExist(err) {
				return rep, err
			}
			rep.Deleted++
		}
	}

	dict, err := ix.keywordDict()
	if err != nil {
		return rep, err
	}
	for _, n := range notes {
		targets := resolveLinks(n.Body, dict)
		if err := ix.store.ReplaceLinks(n.ID, targets); err != nil {
			return rep, err
		}
		rep.Links += countExcludingSelf(targets, n.ID)
	}

	return rep, nil
}

// RefreshIfStale compares the note and journal files on disk against the indexed mtimes and, when they
// diverge (a note added, changed, or removed), runs Full to bring the index back in sync. It reports
// whether a reindex happened. The common "nothing changed" path only reads directory entries and their
// mtimes, so it is cheap enough to call before serving a query. This is how a long-lived process (the
// web server, a second editor's CLI) picks up edits it never observed as an event — a write by another
// process, or an external/cloud-sync change that raised no filesystem notification.
func (ix *Indexer) RefreshIfStale() (bool, error) {
	disk, err := ix.scanMtimes()
	if err != nil {
		return false, err
	}
	indexed, err := ix.store.NoteMtimes()
	if err != nil {
		return false, err
	}
	if sameMtimes(disk, indexed) {
		return false, nil
	}
	// Record the activity day on each note that was added or changed (mtime diverged) before the rebuild,
	// so Full picks the new days into note_days. Removed ids are skipped. This is how an editor's direct
	// body save — which never goes through a track mutation command — gets its day stamped into the sidecar.
	changed := map[int64]int64{}
	for id, m := range disk {
		if im, ok := indexed[id]; !ok || im != m {
			changed[id] = m
		}
	}
	if err := ix.recordActivity(changed); err != nil {
		return false, err
	}
	if _, err := ix.Full(); err != nil {
		return false, err
	}
	return true, nil
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

// scanMtimes maps note id -> file mtime (Unix seconds) for every note/journal file, mirroring how
// UpsertNote records mtime. Files whose basename is not a numeric note id are skipped, matching the ids
// the indexer assigns; reading mtimes never parses a file.
func (ix *Indexer) scanMtimes() (map[int64]int64, error) {
	out := map[int64]int64{}
	for _, root := range []string{ix.cfg.NoteDir(), ix.cfg.JournalDir()} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if d == nil {
					return nil // note/journal dir may not exist yet
				}
				return err
			}
			if d.IsDir() {
				if path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if !slices.Contains(ix.cfg.Extensions, filepath.Ext(path)) {
				return nil
			}
			id, err := note.IDFromPath(path)
			if err != nil {
				return nil // not an indexed note id
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[id] = info.ModTime().Unix()
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
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
	n, err := note.ParseFile(path, ix.cfg)
	if err != nil {
		return err
	}
	// A track mutation command (new/append/toggle/journal) just wrote this file, so stamp its mtime day
	// into the sidecar. The editor-edit path is handled separately by RefreshIfStale. Journals carry no
	// activity day: their note_days rows are excluded anyway (see store.UpsertNote), so stamping would
	// only write dead metadata.
	if n.Kind != "journal" {
		if meta, changed := note.EnsureDay(n.Meta, ix.activityDay(n.Mtime)); changed {
			if err := note.WriteMetadata(ix.cfg.MetadataPath(n.ID), meta); err != nil {
				return err
			}
			n.Meta = meta
		}
	}
	if err := ix.store.UpsertNote(n); err != nil {
		return err
	}
	dict, err := ix.keywordDict()
	if err != nil {
		return err
	}
	if err := ix.store.ReplaceLinks(n.ID, resolveLinks(n.Body, dict)); err != nil {
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
	kws, err := ix.store.Keywords()
	if err != nil {
		return nil, err
	}
	dict := make(map[string]int64, len(kws))
	for _, k := range kws {
		if _, ok := dict[k.Term]; !ok {
			dict[k.Term] = k.NoteID
		}
	}
	return dict, nil
}

// resolveLinks returns the deduplicated note ids referenced by body's [[...]] links, in first-seen order.
// Unresolved references (no matching title) are skipped.
func resolveLinks(body string, dict map[string]int64) []int64 {
	var ids []int64
	seen := make(map[int64]bool)
	for _, ref := range link.Refs(body) {
		id, ok := dict[ref.Text]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func (ix *Indexer) scanFiles() ([]string, error) {
	var out []string
	for _, root := range []string{ix.cfg.NoteDir(), ix.cfg.JournalDir()} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if d == nil {
					return nil // note/journal dir may not exist yet
				}
				return err
			}
			if d.IsDir() {
				if path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if slices.Contains(ix.cfg.Extensions, filepath.Ext(path)) {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
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
