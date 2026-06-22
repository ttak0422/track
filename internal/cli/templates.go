package cli

import (
	"flag"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	tmpl "github.com/ttak0422/track/internal/track/template"
)

func cmdTemplate(args []string) int {
	if len(args) == 0 {
		return fail("template subcommand is required: new, open, list")
	}
	switch args[0] {
	case "new":
		return cmdTemplateNew(args[1:])
	case "open":
		return cmdTemplateOpen(args[1:])
	case "list":
		return cmdTemplateList(args[1:])
	default:
		return fail("unknown template subcommand %q", args[0])
	}
}

func cmdTemplateNew(args []string) int {
	fs := flagSet("template new")
	name := fs.String("name", "", "template name")
	id := fs.Int64("id", 0, "template id; defaults to current Unix second * 1000 plus a same-second sequence")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	t, err := tmpl.Create(cfg, strings.TrimSpace(*name), *id, false)
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "created": true})
}

func cmdTemplateOpen(args []string) int {
	fs := flagSet("template open")
	name := fs.String("name", "", "template name")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	n := strings.TrimSpace(*name)
	if n == "" {
		return fail("--name is required")
	}
	if t, found, err := tmpl.FindByName(cfg, n); err != nil {
		return fail("find template: %v", err)
	} else if found {
		return emit(map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "created": false})
	}
	t, err := tmpl.Create(cfg, n, 0, false)
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "created": true})
}

func cmdTemplateList(args []string) int {
	fs := flagSet("template list")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	templates, err := tmpl.List(cfg)
	if err != nil {
		return fail("list templates: %v", err)
	}
	if templates == nil {
		templates = []tmpl.Ref{}
	}
	return emit(map[string]any{"templates": templates})
}

func flagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}
