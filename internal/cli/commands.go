package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func cmdReindex(args []string) int {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	fs.Bool("full", false, "full rebuild (default and only mode for now)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	start := time.Now()
	rep, err := index.New(cfg, s).Full()
	if err != nil {
		return fail("reindex: %v", err)
	}
	return emit(map[string]any{
		"indexed": rep.Indexed,
		"deleted": rep.Deleted,
		"links":   rep.Links,
		"took_ms": time.Since(start).Milliseconds(),
	})
}

func cmdNew(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	title := fs.String("title", "", "note title (also a link keyword)")
	id := fs.Int64("id", 0, "note id (unix timestamp in milliseconds); defaults to now")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	t := strings.TrimSpace(*title)
	if t == "" {
		return fail("--title is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Titles are link keywords, so a second note with the same title would make the keyword
	// ambiguous. new is the strict create: it refuses an existing title rather than minting a
	// duplicate. Use open to create-or-open idempotently.
	if _, found, err := s.ResolveTerm(t); err != nil {
		return fail("resolve: %v", err)
	} else if found {
		return fail("note already exists for title %q", t)
	}

	noteID := *id
	if noteID == 0 {
		// Auto id: start from the current millisecond and take the next free slot so a burst of
		// machine-generated notes in the same instant never overwrite one another.
		noteID, err = note.FreeID(cfg, time.Now().UnixMilli())
		if err != nil {
			return fail("allocate note id: %v", err)
		}
	}

	res, err := createTitledNote(cfg, s, noteID, t)
	if err != nil {
		return fail("%v", err)
	}
	return emit(res)
}

// cmdOpen resolves a title to its note, creating one only when none exists. Because it never makes a
// second note for a title that already resolves, repeated opens keep titles unique. The result carries
// "created" so callers can decide whether a reindex (to pick up new inbound links) is needed.
func cmdOpen(args []string) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	title := fs.String("title", "", "note title to open, or create when absent")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	t := strings.TrimSpace(*title)
	if t == "" {
		return fail("--title is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	ref, found, err := s.ResolveTerm(t)
	if err != nil {
		return fail("resolve: %v", err)
	}
	if found {
		return emit(map[string]any{"id": ref.NoteID, "path": ref.Path, "title": t, "created": false})
	}

	noteID, err := note.FreeID(cfg, time.Now().UnixMilli())
	if err != nil {
		return fail("allocate note id: %v", err)
	}
	res, err := createTitledNote(cfg, s, noteID, t)
	if err != nil {
		return fail("%v", err)
	}
	res["created"] = true
	return emit(res)
}

// createTitledNote writes a new note titled `title` at `noteID`, indexes it, and returns its summary.
// It guards against clobbering an existing file so an explicit id collision surfaces as an error.
func createTitledNote(cfg *config.Config, s *store.Store, noteID int64, title string) (map[string]any, error) {
	path := cfg.NotePath(noteID)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("note already exists: %s", path)
	}
	if err := os.MkdirAll(cfg.VaultDir, 0o755); err != nil {
		return nil, fmt.Errorf("create vault dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# "+title+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write note: %v", err)
	}
	if err := note.WriteMetadata(
		cfg.MetadataPath(noteID),
		note.Metadata{Title: title, Created: time.Now().Format(cfg.DateFormat)},
	); err != nil {
		return nil, fmt.Errorf("write metadata: %v", err)
	}
	if err := index.New(cfg, s).One(path); err != nil {
		return nil, fmt.Errorf("index note: %v", err)
	}
	return map[string]any{"id": noteID, "path": path, "title": title}, nil
}

func cmdJournal(args []string) int {
	fs := flag.NewFlagSet("journal", flag.ContinueOnError)
	offset := fs.Int("offset", 0, "day offset: 0=today, -1=yesterday, 1=tomorrow")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	day := startOfDay(time.Now()).AddDate(0, 0, *offset)
	name := day.Format(cfg.JournalDateFormat)
	noteID, err := note.IDFromName(name)
	if err != nil {
		return fail("journal id: %v", err)
	}
	date := day.Format(cfg.DateFormat)
	path := cfg.JournalPath(name)

	created := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
			return fail("create journal dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("# "+name+"\n"), 0o644); err != nil {
			return fail("write journal: %v", err)
		}
		if err := note.WriteMetadata(
			cfg.MetadataPath(noteID),
			note.Metadata{Title: name, Tags: []string{"journal"}, Created: date},
		); err != nil {
			return fail("write metadata: %v", err)
		}
		if err := index.New(cfg, s).One(path); err != nil {
			return fail("index journal: %v", err)
		}
		created = true
	}
	return emit(map[string]any{"id": noteID, "path": path, "created": created})
}

func cmdKeywords(args []string) int {
	_, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	kws, err := s.Keywords()
	if err != nil {
		return fail("keywords: %v", err)
	}
	if kws == nil {
		kws = []store.Keyword{}
	}
	return emit(map[string]any{"keywords": kws})
}

func cmdResolve(args []string) int {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	term := fs.String("term", "", "keyword to resolve")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *term == "" {
		return fail("--term is required")
	}

	_, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	ref, found, err := s.ResolveTerm(*term)
	if err != nil {
		return fail("resolve: %v", err)
	}
	if !found {
		return emit(map[string]any{"found": false})
	}
	return emit(map[string]any{"found": true, "note_id": ref.NoteID, "path": ref.Path})
}

func cmdSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	query := fs.String("query", "", "search query")
	limit := fs.Int("limit", 50, "max results")
	scope := fs.String("scope", string(store.SearchAll), "search scope: all, title, body")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *query == "" {
		return fail("--query is required")
	}

	_, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	results, err := s.SearchScoped(*query, *limit, store.SearchScope(*scope))
	if err != nil {
		return fail("search: %v", err)
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	return emit(map[string]any{"results": results})
}

func cmdBacklinks(args []string) int {
	fs := flag.NewFlagSet("backlinks", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	path := fs.String("path", "", "note path (alternative to --id)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	_, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	noteID := *id
	if noteID == 0 {
		if *path == "" {
			return fail("--id or --path is required")
		}
		parsed, err := note.IDFromPath(*path)
		if err != nil {
			return fail("invalid path: %v", err)
		}
		noteID = parsed
	}

	back, err := s.Backlinks(noteID)
	if err != nil {
		return fail("backlinks: %v", err)
	}
	if back == nil {
		back = []store.NoteRef{}
	}
	return emit(map[string]any{"backlinks": back})
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
