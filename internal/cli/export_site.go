package cli

import (
	"flag"
	"strconv"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/site"
)

// cmdExportSite publishes a chosen set of notes as a self-contained static site under --out: the React
// web frontend (built in static mode) running against a pre-generated JSON bundle, so the published
// site keeps track's sidebar, graph, and hover previews without a server.
//
// --frontend points at the static-mode frontend build (Vite output) to copy into the site. Two input
// modes:
//   - Vault:     --root <id> [--id <id> ...]  publishes vault notes; --root is the landing note's id.
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
		if len(ids) > 0 {
			return fail("--id is a vault-mode flag; a --src directory publishes every .md file in it")
		}
		res, err := site.BuildDir(*src, *baseURL, *frontend, *out)
		if err != nil {
			return fail("export-site: %v", err)
		}
		return emit(res)
	}

	// Vault mode.
	if *root == "" {
		return fail("--root <id> is required (entry note for the site landing page)")
	}
	rootID, err := strconv.ParseInt(*root, 10, 64)
	if err != nil {
		return fail("--root must be a note id in vault mode (got %q); use --src for a directory", *root)
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

	res, err := site.Build(cfg, s, site.Options{Root: rootID, IDs: ids, Calendar: *calendar, BaseURL: *baseURL}, *frontend, *out)
	if err != nil {
		return fail("export-site: %v", err)
	}
	return emit(res)
}
