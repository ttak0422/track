package cli

import (
	"flag"

	"github.com/ttak0422/track/internal/track/site"
)

// cmdExportSite renders a chosen set of notes as a self-contained static HTML site under --out.
// The --root note becomes index.html (the site's entry page, since the live heatmap top page is not
// published); each additional --id note becomes its own page. Wiki links between selected notes are
// navigable; links to notes outside the selection are flattened to inert text.
func cmdExportSite(args []string) int {
	fs := flag.NewFlagSet("export-site", flag.ContinueOnError)
	root := fs.Int64("root", 0, "entry note id, rendered as index.html")
	var ids idsFlag
	fs.Var(&ids, "id", "note id to include (repeatable, comma-separated)")
	out := fs.String("out", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *root == 0 {
		return fail("--root <id> is required (entry note rendered as index.html)")
	}
	if *out == "" {
		return fail("--out <dir> is required")
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

	res, err := site.Build(cfg, resolve, site.Options{Root: *root, IDs: ids}, *out)
	if err != nil {
		return fail("export-site: %v", err)
	}
	return emit(res)
}
