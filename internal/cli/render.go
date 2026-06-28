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
	// --help prints the flag list plus the View Spec notation reference to stdout, so the available
	// chart types, data kinds, axes, and overlay fields are discoverable without reading the docs.
	if wantsHelp(args) {
		fmt.Print(renderUsage(fs))
		return 0
	}
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

// wantsHelp reports whether the args request help, so the command can print usage instead of failing
// on the missing required flags.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			return true
		}
	}
	return false
}

// renderUsage builds the full help text: the usage line, the flag defaults, and the View Spec
// notation reference. PrintDefaults is captured into the builder so the whole help is one stdout write.
func renderUsage(fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString("Usage: track render --spec <spec.json> --out <file> [--renderer NAME]\n\nFlags:\n")
	fs.SetOutput(&b)
	fs.PrintDefaults()
	fs.SetOutput(os.Stderr)
	b.WriteString("\n" + viewSpecReference())
	return b.String()
}

// viewSpecReference renders the View Spec notation for `track render --help`. The enumerated values
// (chart types, data kinds, axes, renderers) are pulled from their defining packages so help never
// drifts from what the code actually accepts.
func viewSpecReference() string {
	kinds := make([]string, len(dataset.KnownKinds))
	for i, k := range dataset.KnownKinds {
		kinds[i] = string(k)
	}
	types := make([]string, len(viewspec.RenderableTypes))
	for i, t := range viewspec.RenderableTypes {
		types[i] = string(t)
	}
	var b strings.Builder
	b.WriteString("View Spec (JSON) reference:\n")
	fmt.Fprintf(&b, "  type:        %s\n", strings.Join(types, " | "))
	fmt.Fprintf(&b, "  data.kind:   %s\n", strings.Join(kinds, " | "))
	b.WriteString("  x.field:     record field for x-axis labels\n")
	b.WriteString("  y[].field:   numeric record field (one or more series)\n")
	fmt.Fprintf(&b, "  y[].axis:    %s   (y2 = secondary right-hand axis)\n", strings.Join(viewspec.AxisOptions, " | "))
	b.WriteString("  filter:      {field, equals}  keep records where field == equals\n")
	b.WriteString("  overlays[]:  {source, kind, at=time, label=text}  vertical event/annotation markers\n")
	fmt.Fprintf(&b, "  renderers:   %s\n", strings.Join(render.Names(), " | "))
	b.WriteString("\nExample:\n")
	b.WriteString(`  {
    "version": 1,
    "type": "line",
    "title": "Price vs Index",
    "data": { "source": "metrics.jsonl", "kind": "metric" },
    "x": { "field": "time" },
    "y": [
      { "field": "close", "label": "Close", "axis": "y" },
      { "field": "index", "label": "Index", "axis": "y2" }
    ],
    "overlays": [
      { "source": "events.jsonl", "kind": "event", "at": "time", "label": "title" }
    ]
  }
`)
	return b.String()
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
