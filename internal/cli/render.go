package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/render"
	"github.com/ttak0422/track/internal/track/viewspec"
)

// cmdRender resolves a View Spec against its JSONL data source and writes a rendered document to a
// file. It is independent of the note index/store: a spec and its data are plain files, so render
// works on any Canonical Data Model JSONL, inside a vault or not.
func cmdRender(args []string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	spec := fs.String("spec", "", "path to a View Spec JSON file")
	out := fs.String("out", "", "path to write the rendered output file")
	renderer := fs.String("renderer", "chartjs", "renderer name ("+strings.Join(render.Names(), "|")+")")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *spec == "" {
		return fail("--spec is required")
	}
	if *out == "" {
		return fail("--out is required")
	}

	r, err := render.Get(*renderer)
	if err != nil {
		return fail("%v", err)
	}

	specFile, err := os.Open(*spec)
	if err != nil {
		return fail("open spec: %v", err)
	}
	vs, err := viewspec.Load(specFile)
	specFile.Close()
	if err != nil {
		return fail("%v", err)
	}

	// Data source is resolved relative to the spec file so a spec is portable alongside its data.
	dataPath := vs.Data.Source
	if !filepath.IsAbs(dataPath) {
		dataPath = filepath.Join(filepath.Dir(*spec), dataPath)
	}
	dataFile, err := os.Open(dataPath)
	if err != nil {
		return fail("open data %s: %v", dataPath, err)
	}
	records, err := dataset.ReadJSONL(dataFile)
	dataFile.Close()
	if err != nil {
		return fail("read data %s: %v", dataPath, err)
	}

	doc, err := r.Render(vs.Resolve(records))
	if err != nil {
		return fail("render: %v", err)
	}

	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		return fail("write %s: %v", *out, err)
	}
	return emit(map[string]any{"path": *out, "renderer": r.Name(), "records": len(records)})
}
