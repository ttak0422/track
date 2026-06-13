package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/note"
)

func cmdExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id/--title)")
	out := fs.String("out", "", "write to a file instead of stdout")
	frontmatter := fs.Bool("frontmatter", false, "prepend a YAML metadata block")
	exportsDefault := fs.String("exports-default", "code", "exports mode for blocks without :exports (code|results|both|none)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	switch *exportsDefault {
	case "code", "results", "both", "none":
	default:
		return fail("invalid --exports-default %q (code|results|both|none)", *exportsDefault)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	notePath, err := resolveNotePath(cfg, s, *id, *title, *path)
	if err != nil {
		return fail("%v", err)
	}

	n, err := note.ParseFile(notePath, cfg)
	if err != nil {
		return fail("read note: %v", err)
	}

	res, err := export.Export(n, export.NewMarkdownRenderer(), export.Options{
		Frontmatter:    *frontmatter,
		DefaultExports: *exportsDefault,
	})
	if err != nil {
		return fail("export: %v", err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "track export: "+w)
	}

	if *out != "" {
		if err := os.WriteFile(*out, []byte(res.Markdown), 0o644); err != nil {
			return fail("write %s: %v", *out, err)
		}
		return emit(map[string]any{"path": *out})
	}
	fmt.Print(res.Markdown)
	return 0
}
