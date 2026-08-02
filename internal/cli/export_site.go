package cli

import (
	"flag"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/site"
	"github.com/ttak0422/track/internal/track/store"
)

// cmdExportSite publishes a chosen set of notes as a self-contained static site under --out: the React
// web frontend (built in static mode) running against a pre-generated JSON bundle, so the published
// site keeps track's sidebar, graph, and hover previews without a server.
//
// --frontend points at the static-mode frontend build (Vite output) to copy into the site. The input is
// a vault: --all publishes every note in it, or --id selects. --root is the landing note's id and
// defaults to the vault config's web.home, the same landing note the workspace opens — a site's front
// door does not change per deployment, so it lives with the content rather than on the command line.
func cmdExportSite(args []string) int {
	fs := flag.NewFlagSet("export-site", flag.ContinueOnError)
	root := fs.String("root", "", "entry note id for the site landing page (defaults to the vault config's web.home)")
	var ids idsFlag
	fs.Var(&ids, "id", "note id to publish (repeatable, comma-separated)")
	all := fs.Bool("all", false, "publish every note in the vault")
	frontend := fs.String("frontend", "", "static-mode frontend build directory to copy into the site")
	out := fs.String("out", "", "output directory")
	calendar := fs.Bool("calendar", false, "include the calendar view and per-day pages")
	baseURL := fs.String("base-url", "", "absolute site origin (https://example.com/site) for og:image/og:url; omitted, those tags are skipped")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	if *out == "" {
		return fail("--out <dir> is required")
	}
	if *frontend == "" {
		return fail("--frontend <dir> is required (static-mode frontend build)")
	}

	var rootID int64
	if *root != "" {
		id, err := strconv.ParseInt(*root, 10, 64)
		if err != nil {
			return fail("--root must be a note id (got %q)", *root)
		}
		rootID = id
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Reindex so the published link graph reflects every note's current links: a note that links to one
	// created later only gets that edge on a full reindex, which export should not miss.
	if _, err := index.New(cfg, s).Full(); err != nil {
		return fail("reindex: %v", err)
	}

	// --all is how a vault publishes what a directory published: every note in it. Selecting notes by
	// id stays available, so the two are mutually exclusive rather than additive — a command that says
	// both "all of them" and "these three" means one of the two was a mistake.
	//
	// Journals are not notes here. They are day hubs indexing creates as a side effect, so a vault
	// accumulates them without anyone writing one, and the set of them is a record of which days their
	// author worked (ADR 0055). Publishing that must be something the caller asks for by id, never
	// something "all" hands over.
	if *all {
		if len(ids) > 0 {
			return fail("--all publishes every note; drop --id to use it, or drop --all to publish a selection")
		}
		refs, err := s.AllNotes()
		if err != nil {
			return fail("list notes: %v", err)
		}
		for _, ref := range refs {
			if ref.FileKind == config.KindJournal {
				continue
			}
			ids = append(ids, ref.NoteID)
		}
	}

	// A site's front door does not change per deployment, so a vault that names one in its config
	// (web.home, the same landing note the workspace opens) needs no --root. The flag still wins when
	// given, and is still required for a vault that names none. Resolving it needs the index, so it
	// happens after the reindex above.
	if rootID == 0 {
		if rootID = homeNoteID(cfg, s); rootID == 0 {
			return fail("--root <id> is required (entry note for the site landing page), or set web.home in the vault config")
		}
	}

	res, err := site.Build(cfg, s, site.Options{Root: rootID, IDs: ids, Calendar: *calendar, BaseURL: *baseURL}, *frontend, *out)
	if err != nil {
		return fail("export-site: %v", err)
	}
	return emit(res)
}

// homeNoteID resolves the vault config's web.home (a note title or a numeric id) to a note id, or 0
// when it is unset or names nothing. It mirrors the web workspace's resolution so the landing note is
// the same page live and published.
func homeNoteID(cfg *config.Config, s *store.Store) int64 {
	home := strings.TrimSpace(cfg.WebHome)
	if home == "" {
		return 0
	}
	if ref, found, err := s.ResolveTerm(home); err == nil && found {
		return ref.NoteID
	}
	if id, err := strconv.ParseInt(home, 10, 64); err == nil {
		return id
	}
	return 0
}
