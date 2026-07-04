package render

import (
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/viewspec"
)

func lineChart(label string, vals []float64) viewspec.Resolved {
	return viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{{Label: label, Values: vals}},
	}
}

func TestRenderDocumentComposesProseAndCharts(t *testing.T) {
	c1 := lineChart("One", []float64{1, 2})
	c2 := lineChart("Two", []float64{3, 4})
	out, err := RenderDocument(Document{
		Title: "Doc",
		Items: []Item{
			{Markdown: "# Heading"},
			{Chart: &c1},
			{Markdown: "between"},
			{Chart: &c2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, `canvas id="chart-`) != 2 {
		t.Fatalf("want 2 canvases: %s", out)
	}
	if strings.Count(out, `class="prose"`) != 2 {
		t.Fatalf("want 2 prose blocks: %s", out)
	}
	for _, want := range []string{"chart.js@4", "marked@12", `"# Heading"`, `"between"`, "<title>Doc</title>"} {
		if !strings.Contains(out, want) {
			t.Errorf("document missing %q", want)
		}
	}
	// Both chart configs are inlined in order.
	if !strings.Contains(out, `"label":"One"`) || !strings.Contains(out, `"label":"Two"`) {
		t.Fatalf("chart configs missing: %s", out)
	}
}

func TestRenderDocumentChartsOnlyOmitsMarked(t *testing.T) {
	c := lineChart("One", []float64{1, 2})
	out, err := RenderDocument(Document{Items: []Item{{Chart: &c}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "marked@12") {
		t.Fatalf("no prose → marked should be omitted: %s", out)
	}
}

func TestRenderDocumentLoadsAnnotationOnlyWithMarkers(t *testing.T) {
	plain := lineChart("One", []float64{1, 2})
	out, _ := RenderDocument(Document{Items: []Item{{Chart: &plain}}})
	if strings.Contains(out, "chartjs-plugin-annotation") {
		t.Fatalf("no markers → annotation plugin should be omitted: %s", out)
	}

	marked := lineChart("Two", []float64{1, 2})
	marked.Markers = []viewspec.Marker{{At: "a", Label: "ev"}}
	out2, _ := RenderDocument(Document{Items: []Item{{Chart: &marked}}})
	if !strings.Contains(out2, "chartjs-plugin-annotation") {
		t.Fatalf("markers → annotation plugin should load: %s", out2)
	}
}

func TestRenderDocumentInlinesSVGOnlyCharts(t *testing.T) {
	line := lineChart("One", []float64{1, 2})
	candle := viewspec.Resolved{
		Spec: viewspec.Spec{Title: "OHLC"}, Chart: viewspec.ChartCandlestick,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "open", Values: []float64{1, 2}}, {Label: "high", Values: []float64{3, 4}},
			{Label: "low", Values: []float64{0, 1}}, {Label: "close", Values: []float64{2, 3}},
		},
	}
	out, err := RenderDocument(Document{Items: []Item{{Chart: &line}, {Chart: &candle}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `class="chart-wrap chart-wrap-svg"`) || !strings.Contains(out, "<svg") {
		t.Fatalf("candlestick should inline an SVG: %s", out)
	}
	if strings.Contains(out, "<?xml") {
		t.Fatalf("inline SVG must not carry the XML prolog: %s", out)
	}
	// Only the line chart gets a canvas; the SVG chart takes no chart index.
	if strings.Count(out, `canvas id="chart-`) != 1 {
		t.Fatalf("want 1 canvas: %s", out)
	}
}

func TestRenderDocumentSVGOnlyChartsOmitChartJS(t *testing.T) {
	timeline := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartTimeline,
		Grid: &viewspec.Grid{Cols: []string{"a"}, Rows: []string{"r"}, Cells: []viewspec.Cell{{Col: 0, Row: 0, Value: 1}}},
	}
	out, err := RenderDocument(Document{Items: []Item{{Chart: &timeline}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "chart.js@4") {
		t.Fatalf("no canvas chart → Chart.js CDN should be omitted: %s", out)
	}
}

func TestRenderDocumentTable(t *testing.T) {
	tbl := viewspec.ResolvedTable{
		Columns: []string{"Sym", "Qty"},
		Rows:    [][]string{{"AAPL", "10"}, {"<x>", "20"}},
		Filter:  true,
	}
	out, err := RenderDocument(Document{Items: []Item{{Table: &tbl}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<th>Sym</th>", "<td>AAPL</td>", "&lt;x&gt;", `class="table-filter"`, "table-filter\").forEach"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q: %s", want, out)
		}
	}
	// No prose/chart CDNs for a table-only document.
	if strings.Contains(out, "marked@12") {
		t.Fatalf("table only → marked should be omitted")
	}
}

func TestRenderDocumentTableFilterScriptOnlyWhenFiltered(t *testing.T) {
	tbl := viewspec.ResolvedTable{Columns: []string{"A"}, Rows: [][]string{{"1"}}}
	out, _ := RenderDocument(Document{Items: []Item{{Table: &tbl}}})
	if strings.Contains(out, "table-filter\").forEach") {
		t.Fatalf("unfiltered table → filter script should be omitted: %s", out)
	}
}

func TestRenderDocumentEscapesTitle(t *testing.T) {
	out, _ := RenderDocument(Document{Title: "<b>x</b>", Items: []Item{{Markdown: "y"}}})
	if strings.Contains(out, "<title><b>x</b></title>") {
		t.Fatalf("title not escaped: %s", out)
	}
}
