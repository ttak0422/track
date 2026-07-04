package render

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/viewspec"
)

func resolvedChart(t viewspec.ChartType, label string, vals []float64) viewspec.Resolved {
	return viewspec.Resolved{
		Spec: viewspec.Spec{Title: "T"}, Chart: t,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{{Label: label, Values: vals}},
	}
}

func TestEChartsRenderPageLoadsCDN(t *testing.T) {
	out, err := ECharts{}.Render(resolvedChart(viewspec.ChartLine, "S", []float64{1, 2}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"echarts@5", "echarts.init", `"type":"line"`, `"data":["a","b"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestEChartsNaNBecomesNull(t *testing.T) {
	out, err := EChartsOptionJSON(resolvedChart(viewspec.ChartLine, "S", []float64{1, math.NaN()}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"data":[1,null]`) {
		t.Fatalf("NaN should marshal as null (a gap): %s", out)
	}
}

func TestEChartsAreaFillsAndBarStacks(t *testing.T) {
	area, _ := EChartsOptionJSON(resolvedChart(viewspec.ChartArea, "S", []float64{1, 2}))
	if !strings.Contains(area, `"areaStyle"`) || !strings.Contains(area, "rgba(78,121,167,0.3)") {
		t.Fatalf("area should fill at the shared palette opacity: %s", area)
	}
	stacked := resolvedChart(viewspec.ChartBar, "S", []float64{1, 2})
	stacked.Stacked = true
	stack, _ := EChartsOptionJSON(stacked)
	if !strings.Contains(stack, `"stack":"total"`) {
		t.Fatalf("stacked bar should stack series: %s", stack)
	}
}

func TestEChartsSecondaryAxis(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: []string{"a"},
		Series: []viewspec.Series{
			{Label: "P", Values: []float64{1}},
			{Label: "Q", Values: []float64{2}, Axis: "y2"},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"yAxisIndex":1`) {
		t.Fatalf("y2 series should bind to the secondary axis: %s", out)
	}
	if strings.Count(out, `"type":"value"`) < 2 {
		t.Fatalf("expected two value axes: %s", out)
	}
}

func TestEChartsHBarInvertsCategoryAxis(t *testing.T) {
	out, err := EChartsOptionJSON(resolvedChart(viewspec.ChartHBar, "S", []float64{3, 2}))
	if err != nil {
		t.Fatal(err)
	}
	// Categories run down the y axis with the first (top-ranked) label on top.
	if !strings.Contains(out, `"inverse":true`) || !strings.Contains(out, `"type":"bar"`) {
		t.Fatalf("hbar should invert its category y axis: %s", out)
	}
}

func TestEChartsBubbleSizesPerPoint(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartBubble,
		Series: []viewspec.Series{{Label: "S", Points: []viewspec.Point{
			{X: 1, Y: 2, R: 5},
			{X: 2, Y: math.NaN(), R: 4}, // incomplete → skipped
			{X: 3, Y: 4, R: math.NaN()}, // missing radius → default
		}}},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"symbolSize":10`) || !strings.Contains(out, `"symbolSize":8`) {
		t.Fatalf("per-point symbol sizes missing: %s", out)
	}
	if strings.Count(out, `"symbolSize"`) != 2 {
		t.Fatalf("incomplete point should be skipped: %s", out)
	}
}

func TestEChartsTimelineAndHeatmapGrids(t *testing.T) {
	grid := &viewspec.Grid{
		Cols:  []string{"d1", "d2"},
		Rows:  []string{"r1", "r2"},
		Cells: []viewspec.Cell{{Col: 0, Row: 0, Value: 1}, {Col: 1, Row: 1, Value: 9}},
	}
	tl, err := EChartsOptionJSON(viewspec.Resolved{Spec: viewspec.Spec{}, Chart: viewspec.ChartTimeline, Grid: grid})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tl, `"type":"scatter"`) || !strings.Contains(tl, `"symbolSize"`) {
		t.Fatalf("timeline should be a sized category scatter: %s", tl)
	}
	hm, err := EChartsOptionJSON(viewspec.Resolved{Spec: viewspec.Spec{}, Chart: viewspec.ChartHeatmap, Grid: grid})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hm, `"type":"heatmap"`) || !strings.Contains(hm, `"visualMap"`) {
		t.Fatalf("heatmap should use a visualMap: %s", hm)
	}
}

func TestEChartsCandlestickDataOrderAndColors(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartCandlestick,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "open", Values: []float64{10, 13}},
			{Label: "high", Values: []float64{14, 13}},
			{Label: "low", Values: []float64{9, math.NaN()}},
			{Label: "close", Values: []float64{13, 9}},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	// ECharts item order is [open, close, lowest, highest]; the NaN candle is a null gap.
	if !strings.Contains(out, `[10,13,9,14]`) || !strings.Contains(out, `"data":[[10,13,9,14],null]`) {
		t.Fatalf("candle data misordered: %s", out)
	}
	if !strings.Contains(out, candleUp) || !strings.Contains(out, candleDown) {
		t.Fatalf("candle colors should match the SVG renderer: %s", out)
	}
}

func TestEChartsOverlays(t *testing.T) {
	res := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res.Markers = []viewspec.Marker{{At: "b", Label: "ev"}}
	res.Lines = []viewspec.RefLine{{Y: 1.5, Axis: "y", Label: "limit"}}
	res.Bands = []viewspec.Band{{From: "a", To: "b", Label: "Q1"}}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"markLine"`, `"xAxis":"b"`, `"formatter":"ev"`, `"yAxis":1.5`, `"markArea"`, `"name":"Q1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay output missing %q: %s", want, out)
		}
	}
}

func TestEChartsY2RefLineNeedsY2Series(t *testing.T) {
	// A y2 reference line rides a y2-bound series; with none it has no scale and is dropped.
	res := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res.Lines = []viewspec.RefLine{{Y: 5, Axis: "y2"}}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"markLine"`) {
		t.Fatalf("y2 line without a y2 series should be dropped: %s", out)
	}

	res.Series = append(res.Series, viewspec.Series{Label: "Q", Values: []float64{1, 2}, Axis: "y2"})
	out2, _ := EChartsOptionJSON(res)
	if !strings.Contains(out2, `"markLine"`) || !strings.Contains(out2, `"yAxis":5`) {
		t.Fatalf("y2 line should ride the y2 series: %s", out2)
	}
}

func TestEChartsRegistered(t *testing.T) {
	if _, err := Get("echarts"); err != nil {
		t.Fatalf("echarts should be registered: %v", err)
	}
	if _, err := Get("chartjs"); err == nil {
		t.Fatal("chartjs was replaced by echarts and should be gone")
	}
}
