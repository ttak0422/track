package rename

import (
	"fmt"
	"os"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Result reports what a rename did. BacklinksUpdated counts rewritten [[link]] occurrences, not files.
type Result struct {
	OldTitle         string
	NewTitle         string
	BacklinksUpdated int
}

// Do renames a note's title through the engine's single rename path: uniqueness check against the
// index, backlink rewrite in referencing note files, sidecar title write, history append, and a
// full reindex. It is shared by `track rename` and the metadata-document apply path (`track meta
// --edit`, the web meta editor). Renaming to the current title is a no-op.
func Do(cfg *config.Config, s *store.Store, noteID int64, to string) (Result, error) {
	meta, found, err := note.ReadMetadata(cfg.MetadataPath(noteID))
	if err != nil {
		return Result{}, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		return Result{}, fmt.Errorf("no metadata for note %d", noteID)
	}
	oldTitle := meta.Title
	res := Result{OldTitle: oldTitle, NewTitle: to}
	if oldTitle == to {
		return res, nil
	}
	if ref, ok, err := s.ResolveTerm(to); err != nil {
		return Result{}, fmt.Errorf("resolve: %w", err)
	} else if ok && ref.NoteID != noteID {
		return Result{}, fmt.Errorf("title %q already in use by note %d", to, ref.NoteID)
	}

	backlinks, err := s.Backlinks(noteID)
	if err != nil {
		return Result{}, fmt.Errorf("backlinks: %w", err)
	}
	for _, src := range backlinks {
		srcPath := cfg.PathForKind(src.FileKind, src.NoteID)
		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return Result{}, fmt.Errorf("read backlink %d: %w", src.NoteID, err)
		}
		rewritten, n := link.ReplaceRefKey(string(raw), oldTitle, to)
		if n == 0 {
			continue
		}
		if err := os.WriteFile(srcPath, []byte(rewritten), 0o644); err != nil {
			return Result{}, fmt.Errorf("write backlink %d: %w", src.NoteID, err)
		}
		res.BacklinksUpdated += n
	}

	meta.Title = to
	if err := note.WriteMetadata(cfg.MetadataPath(noteID), meta); err != nil {
		return Result{}, fmt.Errorf("write metadata: %w", err)
	}
	if err := Append(cfg.RenamesPath(), Entry{From: oldTitle, To: to, NoteID: noteID}); err != nil {
		return Result{}, fmt.Errorf("write rename history: %w", err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		return Result{}, fmt.Errorf("reindex: %w", err)
	}
	return res, nil
}
