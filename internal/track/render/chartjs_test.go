package render

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/viewspec"
)

func resolved(typ viewspec.ChartType, title string, vals []float64) viewspec.Resolved {
	return viewspec.Resolved{
		Spec:   viewspec.Spec{Version: 1, Type: typ, Title: title},
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

func TestChartJSRenderNoMarkersOmitsPlugin(t *testing.T) {
	out, err := ChartJS{}.Render(resolved(viewspec.ChartLine, "", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "chartjs-plugin-annotation") || strings.Contains(out, `"annotation"`) {
		t.Fatalf("plugin should be omitted when there are no markers: %s", out)
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
