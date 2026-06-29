package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ttak0422/track/internal/track/article"
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

	specJSON, err := os.ReadFile(*spec)
	if err != nil {
		return fail("open spec: %v", err)
	}
	// A spec with a "blocks" array is a composed article (prose + multiple charts); otherwise it is a
	// single View Spec chart.
	if isArticle(specJSON) {
		return cmdRenderArticle(*spec, specJSON, *out)
	}

	r, err := render.Get(*renderer)
	if err != nil {
		return fail("%v", err)
	}
	vs, err := viewspec.Load(bytes.NewReader(specJSON))
	if err != nil {
		return fail("%v", err)
	}
	resolved, err := resolveChart(*spec, vs)
	if err != nil {
		return fail("%v", err)
	}
	doc, err := r.Render(resolved)
	if err != nil {
		return fail("render: %v", err)
	}
	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		return fail("write %s: %v", *out, err)
	}
	return emit(map[string]any{"path": *out, "renderer": r.Name(), "records": len(resolved.Labels) + len(resolved.Series)})
}

// isArticle reports whether a spec is a composed article, detected by a non-empty top-level "blocks".
func isArticle(specJSON []byte) bool {
	var probe struct {
		Blocks json.RawMessage `json:"blocks"`
	}
	_ = json.Unmarshal(specJSON, &probe)
	return len(probe.Blocks) > 0
}

// resolveChart reads a chart's data and overlay sources (relative to the spec/article file) and
// resolves the View Spec against them. It is shared by the single-chart and article render paths.
func resolveChart(specPath string, vs viewspec.Spec) (viewspec.Resolved, error) {
	records, err := readJSONLRelative(specPath, vs.Data.Source)
	if err != nil {
		return viewspec.Resolved{}, err
	}
	res := vs.Resolve(records)
	for i, ov := range vs.Overlays {
		ovRecords, err := readJSONLRelative(specPath, ov.Source)
		if err != nil {
			return viewspec.Resolved{}, fmt.Errorf("overlay[%d]: %w", i, err)
		}
		res.Markers = append(res.Markers, ov.Markers(ovRecords)...)
	}
	return res, nil
}

// cmdRenderArticle resolves each chart block in an article and composes prose + charts into one HTML
// page. Article output is always the Chart.js-based document renderer.
func cmdRenderArticle(specPath string, specJSON []byte, out string) int {
	a, err := article.Load(bytes.NewReader(specJSON))
	if err != nil {
		return fail("%v", err)
	}
	doc := render.Document{Title: a.Title}
	for i, b := range a.Blocks {
		if b.Chart != nil {
			res, err := resolveChart(specPath, *b.Chart)
			if err != nil {
				return fail("blocks[%d]: %v", i, err)
			}
			doc.Items = append(doc.Items, render.Item{Chart: &res})
			continue
		}
		if b.Table != nil {
			records, err := readJSONLRelative(specPath, b.Table.Data.Source)
			if err != nil {
				return fail("blocks[%d]: %v", i, err)
			}
			res := b.Table.Resolve(records)
			doc.Items = append(doc.Items, render.Item{Table: &res})
			continue
		}
		doc.Items = append(doc.Items, render.Item{Markdown: b.Markdown})
	}
	page, err := render.RenderDocument(doc)
	if err != nil {
		return fail("render: %v", err)
	}
	if err := os.WriteFile(out, []byte(page), 0o644); err != nil {
		return fail("write %s: %v", out, err)
	}
	return emit(map[string]any{"path": out, "renderer": "chartjs", "blocks": len(a.Blocks)})
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

Bubble adds size (radius):
  { "type": "bubble", ..., "x": {"field":"ret"}, "y": [{"field":"vol"}], "size": {"field":"exposure"} }

Article (composed document): a spec with a "blocks" array of prose, charts, and
tables is rendered as one HTML page (prose via marked, charts via Chart.js,
tables as server-side HTML). Each block sets exactly one of markdown/chart/table:
  {
    "version": 1,
    "title": "Market narrative",
    "blocks": [
      { "markdown": "# Overview\n\nNarrative text..." },
      { "chart": { <a View Spec as above> } },
      { "table": {
          "data": { "source": "trades.jsonl", "kind": "event" },
          "columns": [ { "field": "time", "label": "Date" }, { "field": "entity" } ],
          "filter": true
      } }
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
