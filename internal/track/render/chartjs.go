package render

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// chartJSCDN is the Chart.js bundle the generated page loads. A CDN reference keeps output simple and
// dependency-free for the MVP; switching to a bundled/pinned asset later is a renderer-local change.
const chartJSCDN = "https://cdn.jsdelivr.net/npm/chart.js@4"

// annotationCDN is the chartjs-plugin-annotation UMD build, which self-registers when loaded after
// Chart.js. It is only included when a spec has overlays (markers, reference lines, or bands) to
// draw, so plain charts stay lean.
const annotationCDN = "https://cdn.jsdelivr.net/npm/chartjs-plugin-annotation@3"

func init() { Register(ChartJS{}) }

// ChartJS renders a resolved View Spec as a self-contained HTML page that draws the chart with
// Chart.js loaded from a CDN. line and bar map directly to Chart.js types over a category x-axis;
// scatter uses the same category axis with the line suppressed.
type ChartJS struct{}

// Name identifies this renderer for selection (track render --renderer chartjs).
func (ChartJS) Name() string { return "chartjs" }

// chartConfig mirrors the subset of Chart.js's config object the MVP emits. It is marshaled to JSON
// and handed to `new Chart(ctx, config)` in the page. Go's json escapes <, >, & to \uXXXX, so the
// marshaled config is safe to inline inside a <script> element.
type chartConfig struct {
	Type    string      `json:"type"`
	Data    chartData   `json:"data"`
	Options chartOption `json:"options"`
}

type chartData struct {
	Labels   []string  `json:"labels"`
	Datasets []dataset `json:"datasets"`
}

type dataset struct {
	Label    string `json:"label"`
	Data     []any  `json:"data"`
	ShowLine *bool  `json:"showLine,omitempty"`
	Fill     any    `json:"fill,omitempty"` // area: fill down to the zero baseline ("origin")
	YAxisID  string `json:"yAxisID,omitempty"`
	// Explicit colors from the shared seriesPalette, so the same spec draws its series in the same
	// deterministic colors as the SVG renderer (Chart.js's own defaults would otherwise apply).
	BorderColor     string `json:"borderColor,omitempty"`
	BackgroundColor string `json:"backgroundColor,omitempty"`
}

type chartOption struct {
	Responsive bool           `json:"responsive"`
	IndexAxis  string         `json:"indexAxis,omitempty"`
	Plugins    map[string]any `json:"plugins,omitempty"`
	Scales     map[string]any `json:"scales,omitempty"`
}

// scaleOption sets one option on a named scale, creating the scale maps as needed, so independent
// features (category pinning, y2, stacking) can each touch scales without clobbering one another.
func (o *chartOption) scaleOption(id, key string, v any) {
	if o.Scales == nil {
		o.Scales = map[string]any{}
	}
	m, ok := o.Scales[id].(map[string]any)
	if !ok {
		m = map[string]any{}
		o.Scales[id] = m
	}
	m[key] = v
}

// chartJSType maps a View Spec chart type to the Chart.js type name. hbar is a horizontal bar, which
// in Chart.js is a "bar" with indexAxis "y" (set on options in Render); an area is a "line" whose
// datasets fill to the origin; all others pass through.
func chartJSType(t viewspec.ChartType) string {
	switch t {
	case viewspec.ChartHBar:
		return "bar"
	case viewspec.ChartArea:
		return "line"
	}
	return string(t)
}

// svgOnlyChart reports whether a drawing form has no Chart.js equivalent: Chart.js has no built-in
// candlestick type (a lookalike would misread as OHLC) and no grid forms, so those render only as SVG.
// A composed document falls back to an inline SVG for them (RenderDocument).
func svgOnlyChart(t viewspec.ChartType) bool {
	return t == viewspec.ChartHeatmap || t == viewspec.ChartTimeline || t == viewspec.ChartCandlestick
}

// Render builds the Chart.js config from the resolved spec and embeds it in a complete HTML document.
func (ChartJS) Render(res viewspec.Resolved) (string, error) {
	if svgOnlyChart(res.Chart) {
		return "", fmt.Errorf("chartjs renderer: %s is only supported by --renderer svg", res.Chart)
	}
	cfgJSON, usesAnnotation, err := chartJSConfigJSON(res)
	if err != nil {
		return "", err
	}
	title := res.Spec.Title
	if title == "" {
		title = "track chart"
	}
	scripts := []string{chartJSCDN}
	if usesAnnotation {
		scripts = append(scripts, annotationCDN)
	}
	return renderPage(html.EscapeString(title), cfgJSON, scripts), nil
}

// chartJSConfigJSON builds the Chart.js config JSON for a resolved spec and reports whether it uses
// the annotation plugin (so a page can decide to load that CDN). It is the shared core behind both the
// single-chart page (Render) and embedded charts in a composed document (RenderDocument).
func chartJSConfigJSON(res viewspec.Resolved) (string, bool, error) {
	cfg := chartConfig{
		Type:    chartJSType(res.Chart),
		Data:    chartData{Labels: res.Labels},
		Options: chartOption{Responsive: true},
	}
	if res.Chart == viewspec.ChartHBar {
		cfg.Options.IndexAxis = "y"
	}
	if res.Spec.Title != "" {
		cfg.Options.Plugins = map[string]any{
			"title": map[string]any{"display": true, "text": res.Spec.Title},
		}
	}
	// Chart.js's scatter type defaults to a linear x-axis; pin it to category so the resolved x
	// labels are honored, and suppress the connecting line.
	if res.Chart == viewspec.ChartScatter {
		cfg.Options.scaleOption("x", "type", "category")
	}
	// Stacked bars: Chart.js stacks datasets when both scales are marked stacked (the same pair works
	// for a horizontal bar, whose category axis is y via indexAxis).
	if res.Stacked {
		cfg.Options.scaleOption("x", "stacked", true)
		cfg.Options.scaleOption("y", "stacked", true)
	}
	usesY2 := false
	for i, s := range res.Series {
		ds := dataset{Label: s.Label, BorderColor: seriesColor(i), BackgroundColor: seriesColor(i)}
		if res.Chart == viewspec.ChartBubble {
			ds.Data = pointsToJSON(s.Points)
		} else {
			ds.Data = floatsToJSON(s.Values)
		}
		if res.Chart == viewspec.ChartScatter {
			no := false
			ds.ShowLine = &no
		}
		// An area is a line dataset filled to the zero baseline, with the fill translucent so
		// overlapping series stay readable (matching the SVG renderer's fill-opacity).
		if res.Chart == viewspec.ChartArea {
			ds.Fill = "origin"
			ds.BackgroundColor = seriesFillColor(i)
		}
		if s.Axis == "y2" {
			ds.YAxisID = "y2"
			usesY2 = true
		}
		cfg.Data.Datasets = append(cfg.Data.Datasets, ds)
	}

	// A secondary axis: keep the default left "y" and add a right "y2" whose gridlines stay off the
	// chart area so the two scales don't visually collide.
	if usesY2 {
		cfg.Options.scaleOption("y", "type", "linear")
		cfg.Options.scaleOption("y", "position", "left")
		cfg.Options.scaleOption("y2", "type", "linear")
		cfg.Options.scaleOption("y2", "position", "right")
		cfg.Options.scaleOption("y2", "grid", map[string]any{"drawOnChartArea": false})
	}

	// Overlays (event/annotation markers, reference lines, bands) draw via chartjs-plugin-annotation.
	usesAnnotation := len(res.Markers)+len(res.Lines)+len(res.Bands) > 0
	if usesAnnotation {
		if cfg.Options.Plugins == nil {
			cfg.Options.Plugins = map[string]any{}
		}
		cfg.Options.Plugins["annotation"] = map[string]any{"annotations": overlayAnnotations(res)}
	}

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", false, fmt.Errorf("marshal chart config: %w", err)
	}
	return string(cfgJSON), usesAnnotation, nil
}

// overlayAnnotations builds the chartjs-plugin-annotation `annotations` object: one vertical line per
// marker (pinned to the category x-axis), one dashed horizontal line per reference line (pinned to its
// y/y2 axis), and one shaded box per band (spanning its x range over the full plot height). Keys are
// stable (m0…, l0…, b0…) so output is deterministic.
func overlayAnnotations(res viewspec.Resolved) map[string]any {
	out := make(map[string]any, len(res.Markers)+len(res.Lines)+len(res.Bands))
	for i, m := range res.Markers {
		ann := map[string]any{
			"type":        "line",
			"scaleID":     "x",
			"value":       m.At,
			"borderColor": "rgba(220,53,69,0.7)",
			"borderWidth": 1,
		}
		if m.Label != "" {
			ann["label"] = map[string]any{
				"content":  m.Label,
				"display":  true,
				"position": "start",
				"rotation": 90,
			}
		}
		out[fmt.Sprintf("m%d", i)] = ann
	}
	for i, l := range res.Lines {
		ann := map[string]any{
			"type":        "line",
			"scaleID":     l.Axis,
			"value":       l.Y,
			"borderColor": "rgba(220,53,69,0.7)",
			"borderWidth": 1,
			"borderDash":  []int{4, 4},
		}
		if l.Label != "" {
			ann["label"] = map[string]any{
				"content":  l.Label,
				"display":  true,
				"position": "end",
			}
		}
		out[fmt.Sprintf("l%d", i)] = ann
	}
	for i, bd := range res.Bands {
		ann := map[string]any{
			"type":            "box",
			"xMin":            bd.From,
			"xMax":            bd.To,
			"backgroundColor": "rgba(108,117,125,0.15)",
			"borderWidth":     0,
		}
		if bd.Label != "" {
			ann["label"] = map[string]any{
				"content":  bd.Label,
				"display":  true,
				"position": "start",
			}
		}
		out[fmt.Sprintf("b%d", i)] = ann
	}
	return out
}

// seriesFillColor is the shared palette color at the same 30% opacity the SVG renderer fills areas
// with, so an area chart reads identically in HTML and SVG output.
func seriesFillColor(i int) string {
	hex := seriesColor(i) // "#rrggbb" from the shared palette
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return hex
	}
	return fmt.Sprintf("rgba(%d,%d,%d,0.3)", (v>>16)&0xff, (v>>8)&0xff, v&0xff)
}

// pointsToJSON converts bubble points to Chart.js {x,y,r} objects. A point missing its x or y is
// skipped (an incomplete datum), and a missing/non-positive radius falls back to a small visible
// default so the point still shows.
func pointsToJSON(ps []viewspec.Point) []any {
	out := make([]any, 0, len(ps))
	for _, p := range ps {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) {
			continue
		}
		r := p.R
		if math.IsNaN(r) || r <= 0 {
			r = 4
		}
		out = append(out, map[string]any{"x": p.X, "y": p.Y, "r": r})
	}
	return out
}

// floatsToJSON converts series values to a JSON-marshalable slice, mapping NaN (a missing value) to
// null so Chart.js renders a gap instead of a false zero. JSON has no NaN, so this also avoids a
// marshal error.
func floatsToJSON(vs []float64) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			out[i] = nil
			continue
		}
		out[i] = v
	}
	return out
}

// renderPage wraps a marshaled Chart.js config in a minimal, self-contained HTML document. escapedTitle
// must already be HTML-escaped; configJSON must be valid JSON safe to inline in a <script>; scripts are
// the script srcs to load in order (Chart.js first, then any plugins).
func renderPage(escapedTitle, configJSON string, scripts []string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n")
	b.WriteString(`<html lang="en">` + "\n")
	b.WriteString("<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>" + escapedTitle + "</title>\n")
	for _, src := range scripts {
		b.WriteString(`<script src="` + src + `"></script>` + "\n")
	}
	b.WriteString("<style>html,body{margin:0;height:100%}#chart-wrap{box-sizing:border-box;padding:16px;height:100%}</style>\n")
	b.WriteString("</head>\n")
	b.WriteString("<body>\n")
	b.WriteString(`<div id="chart-wrap"><canvas id="chart"></canvas></div>` + "\n")
	b.WriteString("<script>\n")
	b.WriteString("const config = " + configJSON + ";\n")
	b.WriteString(`new Chart(document.getElementById("chart"), config);` + "\n")
	b.WriteString("</script>\n")
	b.WriteString("</body>\n</html>\n")
	return b.String()
}
