package render

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/viewspec"
)

func resolved(typ viewspec.ChartType, title string, vals []float64) viewspec.Resolved {
	return viewspec.Resolved{
		Spec: viewspec.Spec{Title: title}, Chart: typ,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{{Label: "S", Values: vals}},
	}
}

func TestChartJSRenderEmbedsConfigAndCDN(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartLine, "Hello", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://cdn.jsdelivr.net/npm/chart.js@4",
		`"type":"line"`,
		`"labels":["a","b"]`,
		`"data":[1,2]`,
		`<canvas id="chart">`,
		"new Chart(",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestChartJSRenderNaNBecomesNull(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartLine, "", []float64{1, math.NaN()}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"data":[1,null]`) {
		t.Fatalf("NaN should marshal to null: %s", out)
	}
}

func TestChartJSHBarUsesBarWithIndexAxisY(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartHBar, "Ranking", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"bar"`) {
		t.Fatalf("hbar should map to Chart.js bar: %s", out)
	}
	if !strings.Contains(out, `"indexAxis":"y"`) {
		t.Fatalf("hbar should set indexAxis y: %s", out)
	}
	// A vertical bar must not set indexAxis, so it stays the default.
	vout, _ := ChartJS{}.Render(resolved(viewspec.ChartBar, "", []float64{1}))
	if strings.Contains(vout, "indexAxis") {
		t.Fatalf("vertical bar should not set indexAxis: %s", vout)
	}
}

func TestChartJSBubbleEmitsXYRPoints(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartBubble,
		Series: []viewspec.Series{{Label: "Sectors", Points: []viewspec.Point{
			{X: 12, Y: 40, R: 1000},
			{X: 1, Y: 2, R: math.NaN()}, // missing radius → default
			{X: math.NaN(), Y: 5, R: 3}, // incomplete → skipped
		}}},
	}
	out, err := ChartJS{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"bubble"`) {
		t.Fatalf("type should be bubble: %s", out)
	}
	if !strings.Contains(out, `"r":1000`) || !strings.Contains(out, `"x":12`) {
		t.Fatalf("first point missing: %s", out)
	}
	if !strings.Contains(out, `"r":4`) {
		t.Fatalf("missing radius should default to 4: %s", out)
	}
	if strings.Contains(out, `"y":5`) {
		t.Fatalf("incomplete point should be skipped: %s", out)
	}
}

func TestChartJSScatterPinsCategoryAxis(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartScatter, "", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"scatter"`) {
		t.Fatal("type should be scatter")
	}
	if !strings.Contains(out, `"scales":{"x":{"type":"category"}}`) {
		t.Fatalf("scatter should pin category x axis: %s", out)
	}
	if !strings.Contains(out, `"showLine":false`) {
		t.Fatalf("scatter should suppress line: %s", out)
	}
}

func TestChartJSDatasetsUseSharedPalette(t *testing.T) {
	// Datasets carry explicit colors from the palette shared with the SVG renderer, keyed by series
	// index, so the same spec is colored identically (and deterministically) by both renderers.
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: []string{"a"},
		Series: []viewspec.Series{
			{Label: "A", Values: []float64{1}},
			{Label: "B", Values: []float64{2}},
		},
	}
	out, err := ChartJS{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{seriesColor(0), seriesColor(1)} {
		if !strings.Contains(out, `"borderColor":"`+want+`"`) || !strings.Contains(out, `"backgroundColor":"`+want+`"`) {
			t.Errorf("dataset %d missing palette color %s: %s", i, want, out)
		}
	}
}

func TestChartJSRenderEscapesTitle(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartLine, "<script>x</script>", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<title><script>") {
		t.Fatalf("title not escaped: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("escaped title missing: %s", out)
	}
	// The config's title text rides inside the JSON; Go's json escapes < > to < so it cannot
	// break out of the <script> element.
	if strings.Contains(out, "<script>x</script></script>") {
		t.Fatalf("unescaped title leaked into script: %s", out)
	}
}

func TestChartJSRenderMarkersAddAnnotationPluginAndLines(t *testing.T) {
	res := resolved(viewspec.ChartLine, "Pressure", []float64{1, 2})
	res.Markers = []viewspec.Marker{{At: "b", Label: "event!"}}
	out, err := ChartJS{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "chartjs-plugin-annotation@3") {
		t.Fatalf("annotation plugin script missing: %s", out)
	}
	for _, want := range []string{`"annotation"`, `"scaleID":"x"`, `"value":"b"`, `"content":"event!"`} {
		if !strings.Contains(out, want) {
			t.Errorf("annotation config missing %q", want)
		}
	}
}

func TestChartJSRenderRefLinesAndBands(t *testing.T) {
	res := resolved(viewspec.ChartLine, "Pressure", []float64{1, 2})
	res.Lines = []viewspec.RefLine{{Y: 1.5, Axis: "y2", Label: "limit"}}
	res.Bands = []viewspec.Band{{From: "a", To: "b", Label: "Q1"}}
	out, err := ChartJS{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "chartjs-plugin-annotation@3") {
		t.Fatalf("annotation plugin script missing: %s", out)
	}
	for _, want := range []string{
		`"scaleID":"y2"`, `"value":1.5`, `"borderDash":[4,4]`, `"content":"limit"`, // reference line
		`"type":"box"`, `"xMin":"a"`, `"xMax":"b"`, `"content":"Q1"`, // band
	} {
		if !strings.Contains(out, want) {
			t.Errorf("annotation config missing %q", want)
		}
	}
}

func TestChartJSRenderNoMarkersOmitsPlugin(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartLine, "", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "chartjs-plugin-annotation") || strings.Contains(out, `"annotation"`) {
		t.Fatalf("plugin should be omitted when there are no markers: %s", out)
	}
}

func TestChartJSRenderSecondaryAxis(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "Close", Values: []float64{1, 2}, Axis: "y"},
			{Label: "Index", Values: []float64{10, 20}, Axis: "y2"},
		},
	}
	out, err := ChartJS{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"label":"Index","data":[10,20],"yAxisID":"y2"`) {
		t.Fatalf("y2 series missing yAxisID: %s", out)
	}
	if !strings.Contains(out, `"y2":{`) || !strings.Contains(out, `"position":"right"`) {
		t.Fatalf("y2 scale missing: %s", out)
	}
	// A primary-only series carries no yAxisID, so single-axis charts stay untouched.
	if strings.Contains(out, `"label":"Close","data":[1,2],"yAxisID"`) {
		t.Fatalf("primary series should not set yAxisID: %s", out)
	}
}

func TestChartJSRenderSingleAxisHasNoY2(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartLine, "", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"y2"`) {
		t.Fatalf("single-axis chart should not define y2: %s", out)
	}
}

func TestChartJSRejectsGridTypes(t *testing.T) {
	for _, typ := range []viewspec.ChartType{viewspec.ChartHeatmap, viewspec.ChartTimeline} {
		res := viewspec.Resolved{Spec: viewspec.Spec{}, Chart: typ, Grid: &viewspec.Grid{}}
		if _, err := (ChartJS{}).Render(res); err == nil {
			t.Errorf("chartjs should reject %s (svg-only)", typ)
		}
	}
}

func TestGetUnknownRenderer(t *testing.T) {
	if _, err := Get("nope"); err == nil {
		t.Fatal("expected error for unknown renderer")
	}
	if _, err := Get("chartjs"); err != nil {
		t.Fatalf("chartjs should be registered: %v", err)
	}
}
