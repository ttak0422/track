package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if err := store.Reset(cfg.DBPath); err != nil {
		return fail("reset index db: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
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
	id := fs.Int64("id", 0, "note id; defaults to current Unix second * 1000 plus a same-second sequence")
	template := fs.String("template", "", "template name or path")
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
		noteID, err = note.NewID(cfg, time.Now())
		if err != nil {
			return fail("allocate note id: %v", err)
		}
	}

	res, err := createTitledNote(cfg, s, noteID, t, *template)
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
	template := fs.String("template", "", "template name or path used when creating")
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
		return emit(map[string]any{"id": ref.NoteID, "path": cfg.PathForKind(ref.FileKind, ref.NoteID), "title": t, "created": false})
	}

	noteID, err := note.NewID(cfg, time.Now())
	if err != nil {
		return fail("allocate note id: %v", err)
	}
	res, err := createTitledNote(cfg, s, noteID, t, *template)
	if err != nil {
		return fail("%v", err)
	}
	res["created"] = true
	return emit(res)
}

// createTitledNote writes a new note titled `title` at `noteID`, indexes it, and returns its summary.
// It guards against clobbering an existing file so an explicit id collision surfaces as an error.
func createTitledNote(cfg *config.Config, s *store.Store, noteID int64, title string, template string) (map[string]any, error) {
	path := cfg.NotePath(noteID)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("note already exists: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create note dir: %v", err)
	}
	body := "# " + title + "\n"
	if strings.TrimSpace(template) != "" {
		rendered, err := renderTemplate(cfg, template, title, noteID, config.KindNote, time.Now())
		if err != nil {
			return nil, fmt.Errorf("render template: %v", err)
		}
		if h1 := note.FirstH1Title(rendered); h1 != title {
			return nil, fmt.Errorf("rendered template title %q does not match note title %q", h1, title)
		}
		body = rendered
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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
	template := fs.String("template", "", "template name or path used when creating")
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
			return fail("create vault dir: %v", err)
		}
		body := "# " + name + "\n"
		if strings.TrimSpace(*template) != "" {
			rendered, err := renderTemplate(cfg, *template, name, noteID, config.KindJournal, day)
			if err != nil {
				return fail("render template: %v", err)
			}
			if h1 := note.FirstH1Title(rendered); h1 != name {
				return fail("rendered template title %q does not match journal title %q", h1, name)
			}
			body = rendered
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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
	cfg, s, err := open()
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
	for i := range kws {
		kws[i].Path = cfg.PathForKind(kws[i].FileKind, kws[i].NoteID)
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

	cfg, s, err := open()
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
	return emit(map[string]any{"found": true, "note_id": ref.NoteID, "path": cfg.PathForKind(ref.FileKind, ref.NoteID)})
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

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	results, err := searchResults(cfg, s, *query, *limit, store.SearchScope(*scope))
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

	cfg, s, err := open()
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
	for i := range back {
		back[i].Path = cfg.PathForKind(back[i].FileKind, back[i].NoteID)
	}
	return emit(map[string]any{"backlinks": back})
}

func searchResults(cfg *config.Config, s *store.Store, query string, limit int, scope store.SearchScope) ([]store.SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	switch scope {
	case store.SearchTitle:
		results, err := s.SearchScoped(query, limit, scope)
		addSearchPaths(cfg, results)
		return results, err
	case store.SearchAll:
		results, err := s.SearchScoped(query, limit, scope)
		if err != nil {
			return nil, err
		}
		addSearchPaths(cfg, results)
		seen := make(map[int64]bool, len(results))
		for _, result := range results {
			seen[result.NoteID] = true
		}
		body, err := bodySearchResults(cfg, s, query, limit-len(results), seen)
		if err != nil {
			return nil, err
		}
		return append(results, body...), nil
	case store.SearchBody:
		return bodySearchResults(cfg, s, query, limit, nil)
	default:
		return nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

func addSearchPaths(cfg *config.Config, results []store.SearchResult) {
	for i := range results {
		results[i].Path = cfg.PathForKind(results[i].FileKind, results[i].NoteID)
	}
}

func bodySearchResults(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	notes, err := s.AllNotes()
	if err != nil {
		return nil, err
	}
	refs := make(map[int64]store.NoteRef, len(notes))
	for _, n := range notes {
		refs[n.NoteID] = n
	}
	paths, err := scanSearchFiles(cfg)
	if err != nil {
		return nil, err
	}
	var out []store.SearchResult
	for _, path := range paths {
		id, err := note.IDFromPath(path)
		ref, indexed := refs[id]
		if err != nil || !indexed || skip[id] {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		body, _, _ := note.SplitLegacyFootmatter(string(raw))
		line, snippet := bodyLineMatch(body, query)
		if line == 0 {
			continue
		}
		out = append(out, store.SearchResult{
			NoteID:   id,
			FileKind: ref.FileKind,
			Path:     cfg.PathForKind(ref.FileKind, id),
			Title:    ref.Title,
			Line:     line,
			Snippet:  snippet,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func scanSearchFiles(cfg *config.Config) ([]string, error) {
	var out []string
	for _, root := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if d == nil {
					return nil
				}
				return err
			}
			if d.IsDir() {
				if path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if slices.Contains(cfg.Extensions, filepath.Ext(path)) {
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

func bodyLineMatch(body, query string) (int, string) {
	lq := strings.ToLower(query)
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), lq) {
			return i + 1, truncateSearchSnippet(strings.TrimSpace(line), 120)
		}
	}
	return 0, ""
}

func truncateSearchSnippet(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
