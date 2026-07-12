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
//   - Vault:     --root <id> [--id <id> ...]  publishes vault notes; --root is the landing page.
//   - Directory: --src <dir> [--root <name>]  publishes a directory of plain Markdown files outside any
//     vault. --root names the entry page and overrides the site's own "home" (<src>/site.yml); with
//     neither, a page named "index" is the entry.
func cmdExportSite(args []string) int {
	fs := flag.NewFlagSet("export-site", flag.ContinueOnError)
	src := fs.String("src", "", "build from a directory of Markdown files instead of vault notes")
	root := fs.String("root", "", "entry page: a note id (vault mode) or file base name (with --src)")
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
		res, err := site.BuildDir(*src, *root, *baseURL, *frontend, *out)
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
