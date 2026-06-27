package cli

import (
	"flag"
	"strconv"

	"github.com/ttak0422/track/internal/track/site"
)

// cmdExportSite renders a chosen set of notes as a self-contained static HTML site under --out.
//
// Two input modes:
//   - Vault:     --root <id> [--id <id> ...]  publishes vault notes; --root becomes index.html and each
//     --id note its own page. Wiki links between selected notes are navigable; links outside the
//     selection are flattened to inert text.
//   - Directory: --src <dir> [--root <name>]  publishes a directory of plain Markdown files (e.g.
//     repo-mounted help) that live outside any vault. --root names the entry file (default index).
//
// The live heatmap top page is never published; the root page is the site's entry point.
func cmdExportSite(args []string) int {
	fs := flag.NewFlagSet("export-site", flag.ContinueOnError)
	src := fs.String("src", "", "build from a directory of Markdown files instead of vault notes")
	root := fs.String("root", "", "entry page: a note id (vault mode) or file base name (with --src)")
	var ids idsFlag
	fs.Var(&ids, "id", "note id to include in vault mode (repeatable, comma-separated)")
	out := fs.String("out", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *out == "" {
		return fail("--out <dir> is required")
	}

	// Directory mode: repo-mounted Markdown, no vault or index needed.
	if *src != "" {
		res, err := site.BuildDir(*src, *root, *out)
		if err != nil {
			return fail("export-site: %v", err)
		}
		return emit(res)
	}

	// Vault mode.
	if *root == "" {
		return fail("--root <id> is required (entry note rendered as index.html)")
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

	resolve := func(key string) (int64, bool) {
		ref, ok, err := s.ResolveTerm(key)
		if err != nil || !ok {
			return 0, false
		}
		return ref.NoteID, true
	}

	res, err := site.Build(cfg, resolve, site.Options{Root: rootID, IDs: ids}, *out)
	if err != nil {
		return fail("export-site: %v", err)
	}
	return emit(res)
}
