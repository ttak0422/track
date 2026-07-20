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
	"github.com/ttak0422/track/internal/track/similar"
	"github.com/ttak0422/track/internal/track/store"
)

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
	term := fs.String("term", "", "keyword to resolve (or pass it as the first argument)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	keyword := strings.TrimSpace(*term)
	if keyword == "" && fs.NArg() > 0 {
		keyword = strings.TrimSpace(fs.Arg(0))
	}
	if keyword == "" {
		return fail("--term (or a keyword argument) is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	ref, found, err := s.ResolveTerm(keyword)
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
	query := fs.String("query", "", "search query; #tag filters tags")
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

	// Self-heal before reading: the index is a cache keyed by (cache_dir, vault), so another editor's
	// CLI, the web server, or an external/cloud sync may have changed notes this process never indexed.
	// A cheap mtime scan reconciles those before search, so results match the files on disk.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}

	results, err := searchResults(cfg, s, *query, *limit, store.SearchScope(*scope))
	if err != nil {
		return fail("search: %v", err)
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	return emit(map[string]any{"results": results})
}

// cmdSimilar lists the notes semantically closest to a note by cosine similarity of their embedding
// vectors. Vectors come from the configured embedder command (heavy lifting outside the engine) and are
// cached by content hash, so only new or changed notes are re-embedded. With no embedder configured it
// prints how to set one up and exits 0, so callers never see this optional feature as a hard failure.
func cmdSimilar(args []string) int {
	fs := flag.NewFlagSet("similar", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id to find related notes for")
	limit := fs.Int("limit", 10, "max related notes")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *id == 0 {
		return fail("--id is required")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	embed, ok := similar.CommandEmbedder(cfg)
	if !ok {
		return emit(map[string]any{
			"embedder": false,
			"message": "no embedder configured. Set `embedder` in config.yml (or the TRACK_EMBEDDER env) to a " +
				"command that reads a note's text on stdin and prints a JSON array of floats on stdout, e.g. " +
				"`embedder: track-embed --model all-minilm`. See the CLI help page for details.",
		})
	}

	// Self-heal the index, then embed any note whose text changed since it was last embedded. Unchanged
	// notes are skipped by content hash, so repeated calls stay cheap.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}
	if _, err := similar.Ensure(cfg, s, embed); err != nil {
		return fail("embed: %v", err)
	}

	all, err := s.AllEmbeddings()
	if err != nil {
		return fail("load embeddings: %v", err)
	}
	results, err := similar.Nearest(all, *id, *limit)
	if err != nil {
		return fail("similar: %v", err)
	}
	for i := range results {
		results[i].Path = cfg.PathForKind(results[i].FileKind, results[i].NoteID)
	}
	if results == nil {
		results = []similar.Result{}
	}
	return emit(map[string]any{"embedder": true, "id": *id, "results": results})
}

// cmdNotes lists indexed notes as JSON, most recently updated first — the CLI counterpart of the web
// workspace's note listing. --untagged narrows it to notes that carry no tags, so a curation pass (human
// or agent) can pull exactly the notes that still need tagging and add them with `track append --tag`.
// Journals are date-titled aggregation hubs with their own surfaces (agenda/journal) and are expected to
// be untagged, so they are omitted from this note-curation listing.
func cmdNotes(args []string) int {
	fs := flag.NewFlagSet("notes", flag.ContinueOnError)
	untagged := fs.Bool("untagged", false, "only notes that carry no tags")
	limit := fs.Int("limit", 0, "max results (0 = no limit)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Self-heal before reading so the listing — and its untagged filter — reflects tags on disk, including
	// ones an editor wrote directly since this process last indexed.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}

	refs, err := s.SearchRefs()
	if err != nil {
		return fail("notes: %v", err)
	}
	notes := make([]store.SearchResult, 0, len(refs))
	for _, r := range refs {
		if r.FileKind != "note" {
			continue
		}
		if *untagged && len(r.Tags) > 0 {
			continue
		}
		notes = append(notes, r)
	}
	addSearchPaths(cfg, notes)
	sortSearchResults(notes)
	if *limit > 0 && len(notes) > *limit {
		notes = notes[:*limit]
	}
	return emit(map[string]any{"notes": notes})
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

// cmdNav prints a note's hierarchy navigation, built from the "up" relation property: the ancestor
// trail (root first) and the children (notes whose "up" points at this note). This is the same data
// the web note view renders as breadcrumbs.
func cmdNav(args []string) int {
	fs := flag.NewFlagSet("nav", flag.ContinueOnError)
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

	trail, err := s.Trail(noteID)
	if err != nil {
		return fail("trail: %v", err)
	}
	children, err := s.ChildNotes(noteID)
	if err != nil {
		return fail("children: %v", err)
	}
	if trail == nil {
		trail = []store.NoteRef{}
	}
	if children == nil {
		children = []store.NoteRef{}
	}
	for _, refs := range [][]store.NoteRef{trail, children} {
		for i := range refs {
			refs[i].Path = cfg.PathForKind(refs[i].FileKind, refs[i].NoteID)
		}
	}
	return emit(map[string]any{"trail": trail, "children": children})
}

// cmdAgenda lists the notes active (created or updated) on a given local calendar day, derived from the
// activity days recorded in each note's sidecar. It powers "what did I work on that day" lookups from a
// day's journal and, later, a calendar.
func cmdAgenda(args []string) int {
	fs := flag.NewFlagSet("agenda", flag.ContinueOnError)
	date := fs.String("date", "", "calendar day (default: today)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Self-heal before reading so an editor's direct save (recorded into the sidecar by RefreshIfStale)
	// is reflected in today's agenda.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}

	day := *date
	if day == "" {
		day = time.Now().Format(cfg.DateFormat)
	}

	notes, err := s.NotesOnDay(day)
	if err != nil {
		return fail("agenda: %v", err)
	}
	if notes == nil {
		notes = []store.NoteRef{}
	}
	for i := range notes {
		notes[i].Path = cfg.PathForKind(notes[i].FileKind, notes[i].NoteID)
	}
	return emit(map[string]any{"date": day, "notes": notes})
}

func cmdGraph(args []string) int {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	path := fs.String("path", "", "note path (alternative to --id)")
	orphans := fs.Bool("orphans", false, "report notes with no inbound links and notes with a missing parent scope (ignores --id/--path)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	if *orphans {
		// Self-heal so orphan status reflects links/titles as they are on disk, not a stale index.
		if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
			return fail("refresh index: %v", err)
		}
		report, err := s.Orphans()
		if err != nil {
			return fail("graph orphans: %v", err)
		}
		for i := range report.Orphans {
			report.Orphans[i].Path = cfg.PathForKind(report.Orphans[i].FileKind, report.Orphans[i].NoteID)
		}
		return emit(report)
	}

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

	graph, err := s.LocalGraph(noteID)
	if err != nil {
		return fail("graph: %v", err)
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Path = cfg.PathForKind(graph.Nodes[i].FileKind, graph.Nodes[i].NoteID)
	}
	return emit(map[string]any{"graph": graph})
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

// bodySearchResults finds notes whose body matches every query term. It prefers the FTS5 index (fast,
// bm25-ranked) and falls back to a per-file scan only for queries with a term too short to form a
// trigram (see store.BodyQueryUsesFTS), so short and two-character CJK queries still work. Either path
// then locates the first matching line in the file to preserve the line-number + snippet contract.
func bodySearchResults(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	groups := store.BodyGroups(query)
	if len(groups) == 0 {
		return []store.SearchResult{}, nil
	}
	if store.BodyQueryUsesFTS(query) {
		return bodySearchFTS(cfg, s, query, limit, skip)
	}
	return bodySearchScan(cfg, s, groups, limit, skip)
}

// bodySearchFTS serves a body query from the FTS index, keeping its relevance order and reading only
// the matched files to attach a line number and snippet.
func bodySearchFTS(cfg *config.Config, s *store.Store, query string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	// Over-fetch by the skip count so notes already returned as title hits do not shrink the page.
	hits, err := s.SearchBodyFTS(query, limit+len(skip))
	if err != nil {
		return nil, err
	}
	groups := store.BodyGroups(query)
	out := make([]store.SearchResult, 0, len(hits))
	for _, hit := range hits {
		if skip[hit.NoteID] {
			continue
		}
		hit.Path = cfg.PathForKind(hit.FileKind, hit.NoteID)
		hit.Line, hit.Snippet = fileLineMatchGroups(hit.Path, groups)
		out = append(out, hit)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// bodySearchScan is the short-term fallback: it scans note files for one containing every term, then
// sorts by recency (bm25 is unavailable off the index). It is only reached for queries the trigram
// index cannot serve, so full scans stay off the common path.
func bodySearchScan(cfg *config.Config, s *store.Store, groups [][]string, limit int, skip map[int64]bool) ([]store.SearchResult, error) {
	notes, err := s.SearchRefs()
	if err != nil {
		return nil, err
	}
	refs := make(map[int64]store.SearchResult, len(notes))
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
		if !bodyMatchesAnyGroup(body, groups) {
			continue
		}
		ref.Path = cfg.PathForKind(ref.FileKind, id)
		ref.Line, ref.Snippet = bodyLineMatchGroups(body, groups)
		out = append(out, ref)
	}
	sortSearchResults(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortSearchResults(results []store.SearchResult) {
	slices.SortFunc(results, func(a, b store.SearchResult) int {
		if a.Mtime != b.Mtime {
			return cmpDesc(a.Mtime, b.Mtime)
		}
		return cmpDesc(a.NoteID, b.NoteID)
	})
}

func cmpDesc[T ~int64](a, b T) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
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

// bodyContainsAll reports whether body contains every term as a case-insensitive substring (implicit
// AND), matching the trigram FTS semantics so the scan fallback agrees with the indexed path.
func bodyContainsAll(body string, terms []string) bool {
	lowerBody := strings.ToLower(body)
	for _, term := range terms {
		if !strings.Contains(lowerBody, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

// bodyMatchesAnyGroup reports whether body satisfies any one OR group (all of that group's terms
// present), mirroring the FTS "(a AND b) OR (c)" semantics for the scan fallback.
func bodyMatchesAnyGroup(body string, groups [][]string) bool {
	for _, terms := range groups {
		if bodyContainsAll(body, terms) {
			return true
		}
	}
	return false
}

// bodyLineMatchGroups returns the 1-based line and snippet best representing the match: the first line
// that contains every term of some satisfied OR group (the tightest match), else the first line
// containing any query term. It returns (0, "") when no line holds a term — the title-only sentinel,
// reached only when a group's terms straddle line breaks.
func bodyLineMatchGroups(body string, groups [][]string) (int, string) {
	anyLine, anyText := 0, ""
	for i, line := range strings.Split(body, "\n") {
		lowerLine := strings.ToLower(line)
		for _, terms := range groups {
			all, any := len(terms) > 0, false
			for _, term := range terms {
				if strings.Contains(lowerLine, strings.ToLower(term)) {
					any = true
				} else {
					all = false
				}
			}
			if all {
				return i + 1, truncateSearchSnippet(strings.TrimSpace(line), 120)
			}
			if any && anyLine == 0 {
				anyLine, anyText = i+1, truncateSearchSnippet(strings.TrimSpace(line), 120)
			}
		}
	}
	return anyLine, anyText
}

// fileLineMatchGroups reads path and locates the best matching line for the query's OR groups. A read
// error yields the title-only sentinel rather than failing the search: the FTS hit is authoritative.
func fileLineMatchGroups(path string, groups [][]string) (int, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, ""
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	return bodyLineMatchGroups(body, groups)
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
