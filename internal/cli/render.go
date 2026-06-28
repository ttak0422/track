package cli

import (
	"flag"
	"fmt"
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

	// Data and overlay sources are resolved relative to the spec file so a spec is portable alongside
	// its data.
	records, err := readJSONLRelative(*spec, vs.Data.Source)
	if err != nil {
		return fail("%v", err)
	}

	resolved := vs.Resolve(records)
	for i, ov := range vs.Overlays {
		ovRecords, err := readJSONLRelative(*spec, ov.Source)
		if err != nil {
			return fail("overlay[%d]: %v", i, err)
		}
		resolved.Markers = append(resolved.Markers, ov.Markers(ovRecords)...)
	}

	doc, err := r.Render(resolved)
	if err != nil {
		return fail("render: %v", err)
	}

	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		return fail("write %s: %v", *out, err)
	}
	return emit(map[string]any{"path": *out, "renderer": r.Name(), "records": len(records)})
}

// readJSONLRelative reads a JSONL data source named in a spec. A relative source is resolved against
// the spec file's directory so specs travel with their data; an absolute source is used as-is.
func readJSONLRelative(specPath, source string) ([]dataset.Record, error) {
	p := source
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(specPath), p)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open data %s: %w", p, err)
	}
	defer f.Close()
	records, err := dataset.ReadJSONL(f)
	if err != nil {
		return nil, fmt.Errorf("read data %s: %w", p, err)
	}
	return records, nil
}
