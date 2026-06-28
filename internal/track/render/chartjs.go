package render

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// chartJSCDN is the Chart.js bundle the generated page loads. A CDN reference keeps output simple and
// dependency-free for the MVP; switching to a bundled/pinned asset later is a renderer-local change.
const chartJSCDN = "https://cdn.jsdelivr.net/npm/chart.js@4"

// annotationCDN is the chartjs-plugin-annotation UMD build, which self-registers when loaded after
// Chart.js. It is only included when a spec has overlay markers to draw, so plain charts stay lean.
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
	YAxisID  string `json:"yAxisID,omitempty"`
}

type chartOption struct {
	Responsive bool           `json:"responsive"`
	IndexAxis  string         `json:"indexAxis,omitempty"`
	Plugins    map[string]any `json:"plugins,omitempty"`
	Scales     map[string]any `json:"scales,omitempty"`
}

// chartJSType maps a View Spec chart type to the Chart.js type name. hbar is a horizontal bar, which
// in Chart.js is a "bar" with indexAxis "y" (set on options in Render); all others pass through.
func chartJSType(t viewspec.ChartType) string {
	if t == viewspec.ChartHBar {
		return "bar"
	}
	return string(t)
}

// Render builds the Chart.js config from the resolved spec and embeds it in a complete HTML document.
func (ChartJS) Render(res viewspec.Resolved) (string, error) {
	cfg := chartConfig{
		Type:    chartJSType(res.Spec.Type),
		Data:    chartData{Labels: res.Labels},
		Options: chartOption{Responsive: true},
	}
	if res.Spec.Type == viewspec.ChartHBar {
		cfg.Options.IndexAxis = "y"
	}
	if res.Spec.Title != "" {
		cfg.Options.Plugins = map[string]any{
			"title": map[string]any{"display": true, "text": res.Spec.Title},
		}
	}
	// Chart.js's scatter type defaults to a linear x-axis; pin it to category so the resolved x
	// labels are honored, and suppress the connecting line.
	if res.Spec.Type == viewspec.ChartScatter {
		cfg.Options.Scales = map[string]any{"x": map[string]any{"type": "category"}}
	}
	usesY2 := false
	for _, s := range res.Series {
		ds := dataset{Label: s.Label, Data: floatsToJSON(s.Values)}
		if res.Spec.Type == viewspec.ChartScatter {
			no := false
			ds.ShowLine = &no
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
		if cfg.Options.Scales == nil {
			cfg.Options.Scales = map[string]any{}
		}
		cfg.Options.Scales["y"] = map[string]any{"type": "linear", "position": "left"}
		cfg.Options.Scales["y2"] = map[string]any{
			"type":     "linear",
			"position": "right",
			"grid":     map[string]any{"drawOnChartArea": false},
		}
	}

	// Overlay markers (events/annotations) become vertical lines via chartjs-plugin-annotation.
	if len(res.Markers) > 0 {
		if cfg.Options.Plugins == nil {
			cfg.Options.Plugins = map[string]any{}
		}
		cfg.Options.Plugins["annotation"] = map[string]any{"annotations": markerAnnotations(res.Markers)}
	}

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal chart config: %w", err)
	}

	title := res.Spec.Title
	if title == "" {
		title = "track chart"
	}
	scripts := []string{chartJSCDN}
	if len(res.Markers) > 0 {
		scripts = append(scripts, annotationCDN)
	}
	return renderPage(html.EscapeString(title), string(cfgJSON), scripts), nil
}

// markerAnnotations builds the chartjs-plugin-annotation `annotations` object: one vertical line per
// marker, pinned to the category x-axis at the marker's value, labeled with its text. Keys are stable
// (m0, m1, ...) so output is deterministic.
func markerAnnotations(markers []viewspec.Marker) map[string]any {
	out := make(map[string]any, len(markers))
	for i, m := range markers {
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
