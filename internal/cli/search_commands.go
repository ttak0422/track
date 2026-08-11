package cli

import (
	"flag"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/search"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/vaultref"
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
	term := fs.String("term", "", "keyword to resolve (or pass it as the first argument). The match is exact and\ncase-sensitive on the title - no substring, no ranking - which is what makes\nthis, and not search, the way to confirm a title before linking to it.\n\"<vault>:Title\" resolves in a registered vault and adds \"vault\" to the reply")
	if code, ok := parseArgs(fs, args); !ok {
		return code
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

	// A "vault:title" keyword whose prefix is another registered vault's name resolves in that vault.
	// The active vault's own name is not a qualifier (it is in its own registry, ADR 0051): fold it
	// away so "self:Title" resolves locally, exactly like the plain title it names.
	r := vaultref.New(cfg)
	defer r.Close()
	if _, title, ok := link.SplitVaultRef(keyword, func(name string) bool { return name == r.SelfName() }); ok {
		keyword = title
	}
	if vault, title, ok := link.SplitVaultRef(keyword, r.IsVault); ok {
		resolved, found, err := r.Resolve(vault, title)
		if err != nil {
			return fail("resolve %s:%s: %v", vault, title, err)
		}
		if !found {
			return emit(map[string]any{"found": false, "vault": vault})
		}
		return emit(map[string]any{"found": true, "vault": vault, "note_id": resolved.NoteID, "path": resolved.Path})
	}

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
	query := fs.String("query", "", "search query: space-separated terms are ANDed, an uppercase OR splits\nalternatives ('a b OR c' is (a AND b) OR c), and a lowercase and/or is an\nordinary term. There is no negation, no quoted phrase and no field: prefix.\nMatching is case-insensitive substring, so a short term over-matches -\nlengthen it rather than quoting. '#tag' filters tags (hierarchically: #a\nmatches a/b, never ab) on the title path only")
	limit := fs.Int("limit", 50, "max results, shared by the title and body groups under --scope all: a query\nmatching this many titles leaves no room for full-text hits")
	scope := fs.String("scope", string(store.SearchAll), "search scope: all, title, body, path. 'all' is the title hits, ranked, then the\nbody hits, ranked separately - one list, two scales - and last the note whose\nfile the query names, if it named one. 'path' asks only that: a whole note id,\nwith or without a directory and a .md suffix. A '#tag' term is a tag filter only\non the title path; under 'body' it is hunted as literal body text, and tags live\nin sidecars, so it matches nothing")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *query == "" {
		return fail("--query is required")
	}

	// With a vault registry (and no --vault selection), search crosses the active and every
	// registered vault: one federated connection, results labeled with their vault.
	targets, cross, err := crossVaultTargets()
	if err != nil {
		return fail("%v", err)
	}
	if cross {
		out, err := federatedSearchResults(targets, *query, *limit, store.SearchScope(*scope))
		if err != nil {
			return fail("search: %v", err)
		}
		return emit(out)
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

	results, err := search.Scoped(cfg, s, *query, *limit, store.SearchScope(*scope))
	if err != nil {
		return fail("search: %v", err)
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	return emit(map[string]any{"results": results})
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
	if code, ok := parseArgs(fs, args); !ok {
		return code
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
	search.AddPaths(cfg, notes)
	search.Sort(notes)
	if *limit > 0 && len(notes) > *limit {
		notes = notes[:*limit]
	}
	return emit(map[string]any{"notes": notes})
}

func cmdBacklinks(args []string) int {
	fs := flag.NewFlagSet("backlinks", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	path := fs.String("path", "", "note path (alternative to --id)")
	if code, ok := parseArgs(fs, args); !ok {
		return code
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
	out := map[string]any{"backlinks": back}

	// With a registry, inbound references may also live in other vaults as [[name:title]] edges.
	// They are keyed by this note's title, and a vault that cannot be consulted is reported —
	// a missing backlink must be distinguishable from a missing vault.
	if len(cfg.Vaults) > 0 {
		meta, found, err := note.ReadMetadata(cfg.MetadataPath(noteID))
		if err == nil && found && meta.Title != "" {
			r := vaultref.New(cfg)
			defer r.Close()
			external, unavailable := r.Inbound(meta.Title)
			if external == nil {
				external = []vaultref.ExternalRef{}
			}
			if unavailable == nil {
				unavailable = []vaultref.Unavailable{}
			}
			out["external"] = external
			out["unavailable"] = unavailable
		}
	}
	return emit(out)
}

// cmdNav prints a note's hierarchy navigation, built from the "up" relation property: the ancestor
// trail (root first) and the children (notes whose "up" points at this note). This is the same data
// the web note view renders as breadcrumbs.
// navUsage spells out the hierarchy contract: how a parent is written, and the two ways nav quietly
// returns nothing — a parent title that does not match exactly, and an index that has not caught up.
const navUsage = `Usage: track nav (--id N | --path P)

Prints {"trail":[…],"children":[…]} — the "up" ancestors, root first and excluding this note, and
the notes whose "up" points here, newest first. Takes no --title, unlike its sibling read commands.

A parent is written as an inline body line, "up:: [[Parent]]", or as an "up" sidecar prop holding a
[[link]] — a plain string value is not a parent. Prefer the inline form: it is an ordinary wikilink
as well, so the parent gains a backlink and track rename rewrites it. The title must match exactly,
case included; a near miss is stored happily and produces no parent and no error. Several parents
are legal but the trail follows the first.

Unlike search/notes/query/agenda/tasks, nav does not self-heal a stale index, so an "up" written by
an editor rather than by track shows up only after the next reindex.
`

func cmdNav(args []string) int {
	fs := flag.NewFlagSet("nav", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	path := fs.String("path", "", "note path (alternative to --id)")
	if code, ok := parseArgs(fs, args, navUsage); !ok {
		return code
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
	if code, ok := parseArgs(fs, args); !ok {
		return code
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
	if code, ok := parseArgs(fs, args); !ok {
		return code
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
