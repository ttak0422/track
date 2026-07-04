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
	return emit(map[string]any{"path": *out, "renderer": r.Name(), "records": resolvedCount(resolved)})
}

// resolvedCount reports how many data points a resolved spec produced, for the render summary: grid
// charts (heatmap/timeline) count cells, bubble counts its {x,y,r} points (it has no x labels), and
// the rest count x labels.
func resolvedCount(res viewspec.Resolved) int {
	switch {
	case res.Grid != nil:
		return len(res.Grid.Cells)
	case res.Chart == viewspec.ChartBubble && len(res.Series) > 0:
		return len(res.Series[0].Points)
	default:
		return len(res.Labels)
	}
}

// isArticle reports whether a spec is a composed article, detected by a non-empty top-level "blocks".
func isArticle(specJSON []byte) bool {
	var probe struct {
		Blocks json.RawMessage `json:"blocks"`
	}
	_ = json.Unmarshal(specJSON, &probe)
	return len(probe.Blocks) > 0
}

// resolveChart resolves the View Spec against its data: inline records when the spec carries them,
// otherwise a JSONL file read relative to the spec/article file. Overlay sources are read relative to
// the same file. It is shared by the single-chart and article render paths.
func resolveChart(specPath string, vs viewspec.Spec) (viewspec.Resolved, error) {
	records := vs.Data.Records
	if records == nil {
		var err error
		records, err = readJSONLRelative(specPath, vs.Data.Source)
		if err != nil {
			return viewspec.Resolved{}, err
		}
	}
	if err := dataset.ValidateRecords(vs.Data.Kind, records); err != nil {
		return viewspec.Resolved{}, fmt.Errorf("data: %w", err)
	}
	res := vs.Resolve(records)
	for i, ov := range vs.Overlays {
		if ov.Source == "" {
			continue // line/band overlays carry literal values; Resolve already placed them
		}
		ovRecords, err := readJSONLRelative(specPath, ov.Source)
		if err != nil {
			return viewspec.Resolved{}, fmt.Errorf("overlay[%d]: %w", i, err)
		}
		if err := dataset.ValidateRecords(ov.Kind, ovRecords); err != nil {
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
			if err := dataset.ValidateRecords(b.Table.Data.Kind, records); err != nil {
				return fail("blocks[%d]: data: %v", i, err)
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

// canonicalModelReference renders the input data format for `track render --help`: each canonical
// kind with its fields (required marked *), derived from the typed structs in dataset so help never
// drifts from the contract. render only draws this model — the data itself comes from track-fetch-*.
func canonicalModelReference() string {
	var b strings.Builder
	b.WriteString("Canonical Data Model (JSONL input — one object per line, one kind per file):\n")
	for _, k := range dataset.KnownKinds {
		fields := make([]string, 0, len(dataset.KindFields(k)))
		for _, f := range dataset.KindFields(k) {
			name := f.Name
			if f.Required {
				name += "*"
			}
			fields = append(fields, name)
		}
		fmt.Fprintf(&b, "  %-11s %s\n", string(k)+":", strings.Join(fields, " "))
		fmt.Fprintf(&b, "  %-11s — %s\n", "", k.Doc())
	}
	b.WriteString("  (* = required; every record also carries version. time is an RFC3339/date-like\n")
	b.WriteString("   string, treated as an opaque category label by the renderer.)\n")
	return b.String()
}

// viewSpecReference renders the View Spec notation for `track render --help`. The enumerated values
// (marks, channel types, data kinds, axes, renderers) are pulled from their defining packages so help
// never drifts from what the code actually accepts.
func viewSpecReference() string {
	kinds := make([]string, len(dataset.KnownKinds))
	for i, k := range dataset.KnownKinds {
		kinds[i] = string(k)
	}
	marks := make([]string, len(viewspec.Marks))
	for i, m := range viewspec.Marks {
		marks[i] = string(m)
	}
	chTypes := make([]string, len(viewspec.ChannelTypes))
	for i, t := range viewspec.ChannelTypes {
		chTypes[i] = string(t)
	}
	var b strings.Builder
	b.WriteString(canonicalModelReference())
	b.WriteString("\nView Spec (JSON) reference — mark names what to draw, encoding maps fields to channels:\n")
	fmt.Fprintf(&b, "  mark:            %s\n", strings.Join(marks, " | "))
	fmt.Fprintf(&b, "  data.kind:       %s\n", strings.Join(kinds, " | "))
	b.WriteString("  encoding.x:      {field, type?}   x channel\n")
	b.WriteString("  encoding.y[]:    {field, type?, title?, axis?}   one or more y series\n")
	fmt.Fprintf(&b, "  encoding.*.type: %s   (default quantitative; nominal = category)\n", strings.Join(chTypes, " | "))
	fmt.Fprintf(&b, "  y[].axis:        %s   (y2 = secondary right-hand axis)\n", strings.Join(viewspec.AxisOptions, " | "))
	fmt.Fprintf(&b, "  sort:            %s   on the category-axis channel (x, or y[0]\n", strings.Join(viewspec.SortOptions, " | "))
	b.WriteString("                   for a horizontal bar); value/-value order categories by series total\n")
	b.WriteString("  limit:           N   keep only the first N categories (after sort): top-N ranking\n")
	b.WriteString("  stack:           true   stack a bar's series; on the measure channel (y[0], or x\n")
	b.WriteString("                   for a horizontal bar)\n")
	b.WriteString("  encoding.color:  {field, type?}   rect: cell value; other marks: a nominal category\n")
	b.WriteString("                   that splits records into one colored series per value (single y)\n")
	b.WriteString("  encoding.size:   {field}   point radius (bubble / timeline dot)\n")
	b.WriteString("  filter:          {field, equals}  keep records where field == equals (shorthand)\n")
	fmt.Fprintf(&b, "                   {all:[{field, op, value}]}  AND conditions; op: %s (range/period)\n", strings.Join(viewspec.FilterOps, " | "))
	b.WriteString("  overlays[]:      one shape per entry:\n")
	b.WriteString("                   {source, kind, at=time, label=text}  vertical event/annotation markers\n")
	b.WriteString("                   {y, axis?, label?}  horizontal reference line at value y (threshold)\n")
	b.WriteString("                   {from, to, label?}  shaded x-range band (period highlight)\n")
	fmt.Fprintf(&b, "  renderers:       %s\n", strings.Join(render.Names(), " | "))
	b.WriteString("\nMarks cover the old chart types: bar+nominal-y = horizontal bar; point =\n")
	b.WriteString("scatter (nominal x) / bubble (quantitative x) / timeline (nominal y); rect = heatmap;\n")
	b.WriteString("area = line filled down to zero. candlestick draws OHLC bars from data.kind price\n")
	b.WriteString("(open/high/low/close are implied, so it takes no encoding.y; svg renderer only).\n")
	b.WriteString("\nExample:\n")
	b.WriteString(`  {
    "version": 2,
    "mark": "line",
    "title": "Price vs Index",
    "data": { "source": "metrics.jsonl", "kind": "metric" },
    "encoding": {
      "x": { "field": "time" },
      "y": [
        { "field": "close", "title": "Close", "axis": "y" },
        { "field": "index", "title": "Index", "axis": "y2" }
      ]
    },
    "overlays": [
      { "source": "events.jsonl", "kind": "event", "at": "time", "label": "title" },
      { "y": 100, "axis": "y2", "label": "threshold" },
      { "from": "2026-01-01", "to": "2026-02-01", "label": "tariff window" }
    ]
  }

Bubble is a point with a size (radius) channel:
  { "mark": "point", ..., "encoding": { "x": {"field":"ret"}, "y": [{"field":"vol"}], "size": {"field":"exposure"} } }

A nominal color splits records into one series per category (one line/bar/point group per value):
  { "mark": "line", ..., "encoding": { "x": {"field":"time"}, "y": [{"field":"value"}], "color": {"field":"entity","type":"nominal"} } }

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
