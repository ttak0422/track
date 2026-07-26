package cli

import (
	"flag"
	"fmt"
	"os"
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
// --frontend points at the static-mode frontend build (Vite output) to copy into the site. Two input
// modes:
//   - Vault:     [--root <id>] (--all | [--id <id> ...])  publishes vault notes; --root is the landing
//     note's id, defaulting to the vault config's web.home (the same landing note the workspace opens),
//     and --all publishes the whole vault the way a directory published every file in it.
//   - Directory: --src <dir>  publishes every .md file in a directory of plain Markdown outside any vault.
//     Its entry page is the site's own "home" (<src>/site.yml); unset, or with no such file, a page named
//     "index". A site's front door does not change per deployment, so it lives with the content, not on
//     the command line — --root, like --id and --calendar, is a vault-mode flag, and passing one with
//     --src is an error, never a silent no-op.
func cmdExportSite(args []string) int {
	fs := flag.NewFlagSet("export-site", flag.ContinueOnError)
	src := fs.String("src", "", "build from a directory of Markdown files instead of vault notes")
	root := fs.String("root", "", "entry note id for the site landing page (vault mode)")
	var ids idsFlag
	fs.Var(&ids, "id", "note id to include in vault mode (repeatable, comma-separated)")
	all := fs.Bool("all", false, "publish every note in the vault (vault mode)")
	frontend := fs.String("frontend", "", "static-mode frontend build directory to copy into the site")
	out := fs.String("out", "", "output directory")
	calendar := fs.Bool("calendar", false, "include the calendar view and per-day pages (vault mode)")
	baseURL := fs.String("base-url", "", "absolute site origin (https://example.com/site) for og:image/og:url; omitted, those tags are skipped")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *out == "" {
		return fail("--out <dir> is required")
	}
	if *frontend == "" {
		return fail("--frontend <dir> is required (static-mode frontend build)")
	}

	// Directory mode: repo-mounted Markdown, no vault or index needed.
	if *src != "" {
		if *calendar {
			return fail("--calendar needs vault notes' activity days; a --src directory has none")
		}
		if *root != "" {
			return fail("--root is a vault-mode flag; a directory's entry page comes from its site.yml \"home\" (or the index convention)")
		}
		if len(ids) > 0 || *all {
			return fail("--id and --all are vault-mode flags; a --src directory publishes every .md file in it")
		}
		// Directory mode is on its way out: a vault publishes the same content with sidecar
		// metadata, stable ids, and the vault config, and keeping two publishing inputs means every
		// export feature is built twice. Warn on stderr so the JSON on stdout stays parseable.
		fmt.Fprintln(os.Stderr, "track export-site: --src (directory mode) is deprecated and will be removed; publish a vault instead (a note can pin its current URL with sidecar `slug:`)")
		res, err := site.BuildDir(*src, *baseURL, *frontend, *out)
		if err != nil {
			return fail("export-site: %v", err)
		}
		return emit(res)
	}

	// Vault mode.
	var rootID int64
	if *root != "" {
		id, err := strconv.ParseInt(*root, 10, 64)
		if err != nil {
			return fail("--root must be a note id in vault mode (got %q); use --src for a directory", *root)
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
