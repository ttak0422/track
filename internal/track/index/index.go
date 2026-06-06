// Package index keeps the SQLite store in sync with the notes on disk: parsing sidecar metadata into rows and computing the auto-link graph.
package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
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
// It is the authoritative rebuild; single-file updates can't see new inbound links, so callers run Full after creating notes.
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

// One updates a single note and recomputes its outgoing links.
// Inbound links from other notes to this one are not refreshed here; run Full for that.
func (ix *Indexer) One(path string) error {
	n, err := note.ParseFile(path, ix.cfg)
	if err != nil {
		return err
	}
	if err := ix.store.UpsertNote(n); err != nil {
		return err
	}
	dict, err := ix.keywordDict()
	if err != nil {
		return err
	}
	return ix.store.ReplaceLinks(n.ID, resolveLinks(n.Body, dict))
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
// Unresolved references (no matching title or alias) are skipped.
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
