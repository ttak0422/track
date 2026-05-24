package cli

import (
	"flag"
	"os"
	"time"

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
	id := fs.Int64("id", 0, "note id (unix timestamp); defaults to now")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *title == "" {
		return fail("--title is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	noteID := *id
	if noteID == 0 {
		noteID = time.Now().Unix()
	}
	path := cfg.NotePath(noteID)
	if _, err := os.Stat(path); err == nil {
		return fail("note already exists: %s", path)
	}

	if err := os.MkdirAll(cfg.VaultDir, 0o755); err != nil {
		return fail("create vault dir: %v", err)
	}
	content, err := note.UpsertFootmatter(
		"# "+*title,
		note.Footmatter{Title: *title, Created: time.Now().Format(cfg.DateFormat)},
		cfg.Footmatter,
	)
	if err != nil {
		return fail("render note: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fail("write note: %v", err)
	}

	if err := index.New(cfg, s).One(path); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{"id": noteID, "path": path, "title": *title})
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
	noteID := day.Unix()
	date := day.Format(cfg.DateFormat)
	path := cfg.NotePath(noteID)

	created := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.VaultDir, 0o755); err != nil {
			return fail("create vault dir: %v", err)
		}
		content, err := note.UpsertFootmatter(
			"# "+date,
			note.Footmatter{Title: date, Tags: []string{"journal"}, Created: date},
			cfg.Footmatter,
		)
		if err != nil {
			return fail("render journal: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fail("write journal: %v", err)
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

	results, err := s.Search(*query, *limit)
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
