package render

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/viewspec"
)

func echartsOptionForTest(t *testing.T, res viewspec.Resolved) map[string]any {
	t.Helper()
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	var opt map[string]any
	if err := json.Unmarshal([]byte(out), &opt); err != nil {
		t.Fatalf("decode option: %v", err)
	}
	return opt
}

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
	for _, want := range []string{echartsCDN, "echarts.init", `"type":"line"`, `"data":["a","b"]`, `"showSymbol":false`} {
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
	colorArea := resolvedChart(viewspec.ChartArea, "buy", []float64{1, 2})
	colorArea.Spec.Encoding.Color = &viewspec.Channel{Field: "entity", Type: viewspec.Nominal, Colors: map[string]string{"buy": "#2d6a4f"}}
	colorArea.ColorSeries = 1
	colored, _ := EChartsOptionJSON(colorArea)
	if !strings.Contains(colored, "rgba(45,106,79,0.3)") {
		t.Fatalf("area should fill with the explicit series color: %s", colored)
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

func TestEChartsHBarAndBubbleApplySeriesStyle(t *testing.T) {
	op := 0.42
	hbar := viewspec.Resolved{
		Spec: viewspec.Spec{Encoding: viewspec.Encoding{Color: &viewspec.Channel{
			Field: "entity", Type: viewspec.Nominal, Opacity: &op,
			Colors: map[string]string{"buy": "#2d6a4f"},
		}}}, Chart: viewspec.ChartHBar,
		Labels: []string{"a"}, ColorSeries: 1,
		Series: []viewspec.Series{{Label: "buy", Values: []float64{3}}},
	}
	hbarOpt := echartsOptionForTest(t, hbar)
	hbarSeries := hbarOpt["series"].([]any)[0].(map[string]any)
	if hbarSeries["color"] != "#2d6a4f" || hbarSeries["itemStyle"].(map[string]any)["opacity"] != op {
		t.Fatalf("hbar style = %#v", hbarSeries)
	}

	bubble := viewspec.Resolved{
		Spec: viewspec.Spec{Encoding: viewspec.Encoding{
			Y: []viewspec.Channel{{Field: "value", AxisName: "score"}},
			Color: &viewspec.Channel{Field: "entity", Type: viewspec.Nominal, Opacity: &op,
				Colors: map[string]string{"buy": "#2d6a4f"}},
		}}, Chart: viewspec.ChartBubble,
		ColorSeries: 1,
		Series:      []viewspec.Series{{Label: "buy", Points: []viewspec.Point{{X: 1, Y: 2, R: 4}}}},
	}
	bubbleOpt := echartsOptionForTest(t, bubble)
	bubbleSeries := bubbleOpt["series"].([]any)[0].(map[string]any)
	if bubbleSeries["color"] != "#2d6a4f" || bubbleSeries["itemStyle"].(map[string]any)["opacity"] != op {
		t.Fatalf("bubble style = %#v", bubbleSeries)
	}
	if bubbleOpt["yAxis"].(map[string]any)["name"] != "score" {
		t.Fatalf("bubble axis name = %#v", bubbleOpt["yAxis"])
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

// treemapResolved builds the resolved shape of the treemap demo: grouped leaves with a diverging
// color value, one node skippable per rule (no size / no value).
func treemapResolved(scale string) viewspec.Resolved {
	return viewspec.Resolved{
		Spec: viewspec.Spec{
			Encoding: viewspec.Encoding{Color: &viewspec.Channel{Field: "change", Scale: scale}},
		},
		Chart: viewspec.ChartTreemap,
		Tree: &viewspec.Tree{Nodes: []viewspec.TreeNode{
			{Label: "AAA", Group: "Tech", Size: 300, Value: 2},
			{Label: "BBB", Group: "Energy", Size: 120, Value: -4},
			{Label: "CCC", Group: "Tech", Size: 80, Value: 1},
			{Label: "DDD", Group: "Tech", Size: math.NaN(), Value: 1},    // no area → skipped
			{Label: "EEE", Group: "Energy", Size: 50, Value: math.NaN()}, // no value → skipped
		}},
	}
}

func TestEChartsTreemapGroupsAndVisualMap(t *testing.T) {
	out, err := EChartsOptionJSON(treemapResolved(viewspec.ScaleDiverging))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"type":"treemap"`,
		// Groups nest their leaves, in first-seen order (Tech before Energy).
		`{"children":[{"name":"AAA","value":[300,2]},{"name":"CCC","value":[80,1]}],"name":"Tech"}`,
		`{"children":[{"name":"BBB","value":[120,-4]}],"name":"Energy"}`,
		// Diverging: domain symmetric around zero over dimension 1, market red→neutral→green.
		`"dimension":1`, `"min":-4`, `"max":4`,
		`"color":["` + candleDown + `","` + divergeNeutral + `","` + candleUp + `"]`,
		`"breadcrumb":{"show":false}`, `"roam":true`, `"upperLabel"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("treemap option missing %q: %s", want, out)
		}
	}
	// Skipped nodes (no size / no value) never reach the option.
	for _, gone := range []string{"DDD", "EEE"} {
		if strings.Contains(out, gone) {
			t.Errorf("undrawable node %s should be skipped: %s", gone, out)
		}
	}
	// Not a cartesian form: no axes, no grid, no zoom.
	for _, gone := range []string{`"xAxis"`, `"yAxis"`, `"grid"`, `"dataZoom"`} {
		if strings.Contains(out, gone) {
			t.Errorf("treemap should not carry %s: %s", gone, out)
		}
	}
}

func TestEChartsTreemapSequentialAndFlat(t *testing.T) {
	// Default scale: the heatmap's sequential ramp over the real value range.
	res := treemapResolved("")
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"min":-4`) || !strings.Contains(out, `"max":2`) {
		t.Fatalf("sequential scale should span the data range: %s", out)
	}
	if !strings.Contains(out, `"color":["`+heatColor(0)+`","`+heatColor(1)+`"]`) {
		t.Fatalf("sequential scale should reuse the heatmap ramp: %s", out)
	}
	// Flat treemap (no groups): leaves sit at the top level, no upperLabel band.
	flat := treemapResolved("")
	for i := range flat.Tree.Nodes {
		flat.Tree.Nodes[i].Group = ""
	}
	out2, _ := EChartsOptionJSON(flat)
	if strings.Contains(out2, `"children"`) || strings.Contains(out2, `"upperLabel"`) {
		t.Fatalf("flat treemap should keep leaves at the top level: %s", out2)
	}
	if !strings.Contains(out2, `{"name":"AAA","value":[300,2]}`) {
		t.Fatalf("flat leaves missing: %s", out2)
	}
}

func TestEChartsHeatmapDivergingRamp(t *testing.T) {
	// The scale option is shared with rect: a diverging heatmap centers its domain on zero.
	res := viewspec.Resolved{
		Spec: viewspec.Spec{
			Encoding: viewspec.Encoding{Color: &viewspec.Channel{Field: "v", Scale: viewspec.ScaleDiverging}},
		},
		Chart: viewspec.ChartHeatmap,
		Grid: &viewspec.Grid{Cols: []string{"a"}, Rows: []string{"r"},
			Cells: []viewspec.Cell{{Col: 0, Row: 0, Value: -3}, {Col: 0, Row: 0, Value: 1}}},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"min":-3`) || !strings.Contains(out, `"max":3`) {
		t.Fatalf("diverging heatmap should center on zero: %s", out)
	}
	if !strings.Contains(out, candleDown) || !strings.Contains(out, candleUp) {
		t.Fatalf("diverging heatmap should use the market ramp: %s", out)
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

func TestEChartsCandlestickExtras(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartCandlestick,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "open", Values: []float64{10, 13}},
			{Label: "high", Values: []float64{14, 13}},
			{Label: "low", Values: []float64{9, 8}},
			{Label: "close", Values: []float64{13, 9}},
			{Label: "MA2", Values: []float64{math.NaN(), 11}, Axis: "y", Mark: viewspec.ChartLine},
			{Label: "Vol", Values: []float64{100, 200}, Axis: "y2", Mark: viewspec.ChartBar, Rise: []int8{1, -1}},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	// The MA line rides the primary axis; the volume bars ride a label-less y2 whose max is inflated
	// 4x so they hug the bottom band; each bar datum adopts its candle's up/down color.
	for _, want := range []string{
		`"name":"MA2"`, `"type":"line"`,
		`"name":"Vol"`, `"type":"bar"`, `"yAxisIndex":1`, `"max":800`,
		`{"itemStyle":{"color":"` + candleUp + `"},"value":100}`,
		`{"itemStyle":{"color":"` + candleDown + `"},"value":200}`,
		`"legend"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("candlestick extras missing %q: %s", want, out)
		}
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

func TestEChartsCalloutBecomesMarkPoint(t *testing.T) {
	res := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res.Callouts = []viewspec.Callout{{X: "b", Y: 2, Label: "peak here"}}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"markPoint"`, `"coord":["b",2]`, `"formatter":"peak here"`, `"backgroundColor"`} {
		if !strings.Contains(out, want) {
			t.Errorf("callout output missing %q: %s", want, out)
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

func TestEChartsComboDrawsPerSeriesForms(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartBar,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "vol", Values: []float64{1, 2}, Mark: viewspec.ChartBar},
			{Label: "idx", Values: []float64{3, 4}, Axis: "y2", Mark: viewspec.ChartLine},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"type":"bar"`) || !strings.Contains(out, `"type":"line"`) {
		t.Fatalf("combo should mix series types: %s", out)
	}
}

func TestEChartsCategoryAxesFollowSeriesForms(t *testing.T) {
	t.Run("explicit domain pins the value axis", func(t *testing.T) {
		res := resolvedChart(viewspec.ChartLine, "index", []float64{87, 101})
		res.Spec.Encoding.Y = []viewspec.Channel{{Field: "value", Domain: []float64{80, 120}}}
		opt := echartsOptionForTest(t, res)
		y := opt["yAxis"].([]any)[0].(map[string]any)
		if y["min"] != float64(80) || y["max"] != float64(120) || y["scale"] != true {
			t.Fatalf("explicit domain not applied: %v", y)
		}
		gap := y["boundaryGap"].([]any)
		if gap[0] != float64(0) || gap[1] != float64(0) {
			t.Fatalf("explicit domain should not receive automatic padding: %v", gap)
		}
	})

	t.Run("line uses its data range", func(t *testing.T) {
		opt := echartsOptionForTest(t, resolvedChart(viewspec.ChartLine, "index", []float64{98, 102}))
		x := opt["xAxis"].(map[string]any)
		y := opt["yAxis"].([]any)[0].(map[string]any)
		if x["boundaryGap"] != false || y["scale"] != true {
			t.Fatalf("line should use an edge-to-edge category axis and scaled value axis: x=%v y=%v", x, y)
		}
		gap := y["boundaryGap"].([]any)
		if gap[0] != "6%" || gap[1] != "12%" {
			t.Fatalf("scaled line axis should have breathing room on both ends: %v", gap)
		}
	})

	t.Run("area keeps its baseline", func(t *testing.T) {
		opt := echartsOptionForTest(t, resolvedChart(viewspec.ChartArea, "amount", []float64{98, 102}))
		x := opt["xAxis"].(map[string]any)
		y := opt["yAxis"].([]any)[0].(map[string]any)
		if x["boundaryGap"] != true {
			t.Fatalf("area should retain category-edge padding: %v", x)
		}
		if _, ok := y["scale"]; ok {
			t.Fatalf("area should retain its zero baseline: %v", y)
		}
		if got := y["boundaryGap"].([]any)[0]; got != float64(0) {
			t.Fatalf("baseline form should not add lower value-axis padding: %v", y)
		}
	})

	t.Run("candlestick keeps the price range", func(t *testing.T) {
		res := viewspec.Resolved{
			Spec: viewspec.Spec{}, Chart: viewspec.ChartCandlestick, Labels: []string{"a", "b"},
			Series: []viewspec.Series{
				{Label: "open", Values: []float64{97, 101}},
				{Label: "high", Values: []float64{103, 104}},
				{Label: "low", Values: []float64{95, 99}},
				{Label: "close", Values: []float64{101, 100}},
			},
		}
		y := echartsOptionForTest(t, res)["yAxis"].([]any)[0].(map[string]any)
		if y["scale"] != true {
			t.Fatalf("prices are not measured from zero: %v", y)
		}
	})

	t.Run("each value axis is classified independently", func(t *testing.T) {
		res := viewspec.Resolved{
			Spec: viewspec.Spec{}, Chart: viewspec.ChartBar, Labels: []string{"a", "b"},
			Series: []viewspec.Series{
				{Label: "index", Values: []float64{98, 102}, Mark: viewspec.ChartLine},
				{Label: "volume", Values: []float64{10, 20}, Axis: "y2", Mark: viewspec.ChartBar},
			},
		}
		opt := echartsOptionForTest(t, res)
		axes := opt["yAxis"].([]any)
		primary := axes[0].(map[string]any)
		secondary := axes[1].(map[string]any)
		if primary["scale"] != true {
			t.Fatalf("line-only primary axis should scale: %v", primary)
		}
		if _, ok := secondary["scale"]; ok {
			t.Fatalf("bar-bearing secondary axis should keep its baseline: %v", secondary)
		}
	})
}

func TestEChartsLegendAndSingleSeriesLabel(t *testing.T) {
	t.Run("single series moves its label to its value axis", func(t *testing.T) {
		primary := echartsOptionForTest(t, resolvedChart(viewspec.ChartLine, "index", []float64{1, 2}))
		if _, ok := primary["legend"]; ok {
			t.Fatalf("single-series chart should not spend a row on a legend: %v", primary["legend"])
		}
		if got := primary["yAxis"].([]any)[0].(map[string]any)["name"]; got != "index" {
			t.Fatalf("single series should label its primary value axis, got %v", got)
		}

		secondaryRes := resolvedChart(viewspec.ChartLine, "rate", []float64{1, 2})
		secondaryRes.Series[0].Axis = "y2"
		secondary := echartsOptionForTest(t, secondaryRes)
		axes := secondary["yAxis"].([]any)
		if _, ok := axes[0].(map[string]any)["name"]; ok {
			t.Fatalf("a y2 series must not label the primary axis: %v", axes[0])
		}
		if got := axes[1].(map[string]any)["name"]; got != "rate" {
			t.Fatalf("single y2 series should label the secondary value axis, got %v", got)
		}
	})

	t.Run("multiple series use a compact scrolling legend", func(t *testing.T) {
		res := resolvedChart(viewspec.ChartLine, "a", []float64{1, 2})
		res.Series = append(res.Series, viewspec.Series{Label: "b", Values: []float64{2, 3}})
		opt := echartsOptionForTest(t, res)
		legend := opt["legend"].(map[string]any)
		grid := opt["grid"].(map[string]any)
		if legend["type"] != "scroll" || legend["top"] != float64(34) || grid["top"] != float64(62) {
			t.Fatalf("title legend should scroll in the compact header: legend=%v grid=%v", legend, grid)
		}
	})
}

func TestEChartsGridBottomLeavesRoomOnlyForSlider(t *testing.T) {
	sparse := echartsOptionForTest(t, resolvedChart(viewspec.ChartLine, "S", []float64{1, 2}))
	if got := sparse["grid"].(map[string]any)["bottom"]; got != float64(24) {
		t.Fatalf("non-slider chart should use compact bottom spacing, got %v", got)
	}

	labels := make([]string, dataZoomSliderThreshold+1)
	values := make([]float64, len(labels))
	dense := echartsOptionForTest(t, viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine, Labels: labels,
		Series: []viewspec.Series{{Label: "S", Values: values}},
	})
	if got := dense["grid"].(map[string]any)["bottom"]; got != float64(64) {
		t.Fatalf("slider should retain its larger bottom spacing, got %v", got)
	}
}

func TestEChartsDerivesZoomFromDensity(t *testing.T) {
	// Category-x charts always zoom from the wheel; only dense ones get the visible slider.
	sparse, _ := EChartsOptionJSON(resolvedChart(viewspec.ChartLine, "S", []float64{1, 2}))
	// The inside zoom must leave the plain wheel to page scrolling (Shift gates wheel-zoom).
	if !strings.Contains(sparse, `"zoomOnMouseWheel":"shift"`) {
		t.Fatalf("inside zoom should gate wheel-zoom behind Shift: %s", sparse)
	}
	if !strings.Contains(sparse, `"type":"inside"`) || strings.Contains(sparse, `"type":"slider"`) {
		t.Fatalf("sparse chart should zoom inside-only: %s", sparse)
	}

	labels := make([]string, 60)
	values := make([]float64, 60)
	for i := range labels {
		labels[i] = string(rune('a' + i%26))
		values[i] = float64(i)
	}
	dense, _ := EChartsOptionJSON(viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: labels,
		Series: []viewspec.Series{{Label: "S", Values: values}},
	})
	if !strings.Contains(dense, `"type":"slider"`) {
		t.Fatalf("dense chart should add a range slider: %s", dense)
	}

	// Grid/value-axis forms stay unzoomed.
	hm, _ := EChartsOptionJSON(viewspec.Resolved{Spec: viewspec.Spec{}, Chart: viewspec.ChartHeatmap,
		Grid: &viewspec.Grid{Cols: []string{"a"}, Rows: []string{"r"}, Cells: []viewspec.Cell{{Value: 1}}}})
	if strings.Contains(hm, `"dataZoom"`) {
		t.Fatalf("heatmap should not zoom: %s", hm)
	}
}

// TestEChartsSeriesCarryExtras pins the provenance contract: a datum with extras becomes a {value,...}
// object whose href/detail land in event/tooltip params, the series cursor shows the click affordance,
// and a series without extras keeps its plain scalar items.
func TestEChartsSeriesCarryExtras(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartScatter,
		Labels: []string{"d1", "d2"},
		Series: []viewspec.Series{{
			Label:  "amount",
			Values: []float64{120, 80},
			Extras: []viewspec.PointExtra{
				{Href: "https://example.com/t1", Note: "1700000000000", Detail: []viewspec.KV{{Label: "what", Value: "buy"}}},
				{},
			},
		}},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"href":"https://example.com/t1"`) {
		t.Fatalf("datum should carry its source link: %s", out)
	}
	if !strings.Contains(out, `"detail":[{"label":"what","value":"buy"}]`) {
		t.Fatalf("datum should carry its detail rows: %s", out)
	}
	if !strings.Contains(out, `"note":"1700000000000"`) {
		t.Fatalf("datum should carry its note reference: %s", out)
	}
	if !strings.Contains(out, `"cursor":"pointer"`) {
		t.Fatalf("a linked series should show the pointer cursor: %s", out)
	}
	if !strings.Contains(out, `{"value":80}`) {
		t.Fatalf("a datum without extras stays a bare value object: %s", out)
	}

	plain, _ := EChartsOptionJSON(resolvedChart(viewspec.ChartLine, "S", []float64{1, 2}))
	if !strings.Contains(plain, `"data":[1,2]`) || strings.Contains(plain, `"cursor"`) {
		t.Fatalf("a series without extras keeps scalar items: %s", plain)
	}
}

// TestEChartsMarkersCarryProvenance pins the overlay contract: event url/note ride on the markLine
// items, and a linked group stops being silent so it accepts clicks.
func TestEChartsMarkersCarryProvenance(t *testing.T) {
	res := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res.Markers = []viewspec.Marker{
		{At: "a", Label: "ev", Href: "https://example.com/n", Note: "1700000000000"},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"href":"https://example.com/n"`) || !strings.Contains(out, `"note":"1700000000000"`) {
		t.Fatalf("marker provenance should ride on the markLine item: %s", out)
	}
	if !strings.Contains(out, `"silent":false`) {
		t.Fatalf("a linked marker group must accept clicks: %s", out)
	}

	plain, _ := EChartsOptionJSON(resolvedChart(viewspec.ChartLine, "S", []float64{1, 2}))
	res2 := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res2.Markers = []viewspec.Marker{{At: "a", Label: "ev"}}
	unlinked, _ := EChartsOptionJSON(res2)
	if !strings.Contains(unlinked, `"silent":true`) || strings.Contains(plain, `"markLine"`) {
		t.Fatalf("unlinked markers stay silent: %s", unlinked)
	}
}

// TestEChartsBoxMarkersCarryPayload pins the annotation-box contract (ADR 0028): a box-mode marker's
// markLine item gains an engine-resolved "box" payload (date + source host), items are emitted sorted
// by category index, non-http(s) hrefs are scrubbed, an unplaceable marker gets no payload, and the
// classic label stays so bare-setOption consumers keep today's look.
func TestEChartsBoxMarkersCarryPayload(t *testing.T) {
	res := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res.Markers = []viewspec.Marker{
		{At: "b", Label: "second", Href: "https://www.example.com/n", Box: true},
		{At: "a", Label: "first", Href: "javascript:alert(1)", Note: "1700000000000", Box: true},
		{At: "zz", Label: "unplaced", Box: true},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"box":{"date":"a"}`) {
		t.Fatalf("box payload should carry the raw x value as its date: %s", out)
	}
	if !strings.Contains(out, `"box":{"date":"b","host":"example.com"}`) {
		t.Fatalf("box payload should carry the www-stripped source host: %s", out)
	}
	if strings.Contains(out, "javascript:") {
		t.Fatalf("non-http(s) hrefs must be scrubbed engine-side: %s", out)
	}
	if a, b := strings.Index(out, `"first"`), strings.Index(out, `"second"`); a > b {
		t.Fatalf("box items should be sorted by category index: %s", out)
	}
	if u, b := strings.Index(out, `"unplaced"`), strings.Index(out, `"second"`); u < b {
		t.Fatalf("unplaceable box markers sort last: %s", out)
	}
	if strings.Count(out, `"box":{`) != 2 {
		t.Fatalf("an unplaceable marker gets no box payload: %s", out)
	}
	if !strings.Contains(out, `"formatter":"first"`) {
		t.Fatalf("the classic label must stay for bare-setOption consumers: %s", out)
	}
	if !strings.Contains(out, `"silent":false`) {
		t.Fatalf("a note-linked box group must accept clicks: %s", out)
	}

	// RFC3339 timestamps trim to their day in the payload (the anchor keeps the raw value).
	res2 := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res2.Labels = []string{"2026-01-02T00:00:00Z", "b"}
	res2.Markers = []viewspec.Marker{{At: "2026-01-02T00:00:00Z", Label: "ev", Box: true}}
	out2, _ := EChartsOptionJSON(res2)
	if !strings.Contains(out2, `"box":{"date":"2026-01-02"}`) || !strings.Contains(out2, `"xAxis":"2026-01-02T00:00:00Z"`) {
		t.Fatalf("RFC3339 at should trim to a day, anchor unchanged: %s", out2)
	}

	// Additive guarantee: no display box, no box key anywhere.
	res3 := resolvedChart(viewspec.ChartLine, "S", []float64{1, 2})
	res3.Markers = []viewspec.Marker{{At: "a", Label: "ev", Href: "https://example.com/n"}}
	out3, _ := EChartsOptionJSON(res3)
	if strings.Contains(out3, `"box"`) {
		t.Fatalf("no box key without display box: %s", out3)
	}
}

func TestEChartsAxisPointerByForm(t *testing.T) {
	bar, _ := EChartsOptionJSON(resolvedChart(viewspec.ChartBar, "S", []float64{1, 2}))
	if !strings.Contains(bar, `"axisPointer":{"type":"shadow"}`) {
		t.Fatalf("bar tooltip should shadow the hovered band: %s", bar)
	}
	line, _ := EChartsOptionJSON(resolvedChart(viewspec.ChartLine, "S", []float64{1, 2}))
	if !strings.Contains(line, `"axisPointer":{"type":"cross"}`) {
		t.Fatalf("line tooltip should crosshair: %s", line)
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

func TestEChartsColorSplitWithOverlayLine(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartBar,
		Labels:      []string{"d1", "d2"},
		ColorSeries: 2,
		Series: []viewspec.Series{
			{Label: "a", Values: []float64{1, 3}},
			{Label: "b", Values: []float64{2, 4}},
			{Label: "index", Values: []float64{10, 20}, Mark: viewspec.ChartLine},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type":"bar"`, `"type":"line"`, `"name":"index"`, `"data":[10,20]`} {
		if !strings.Contains(out, want) {
			t.Errorf("color split + overlay line missing %q: %s", want, out)
		}
	}
}

func TestEChartsVBandBecomesMarkArea(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{{Label: "S", Values: []float64{1, 2}}},
		VBands: []viewspec.VBand{{From: 10, To: 18, Label: "high"}, {From: -9, To: 0}},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"markArea"`, `"yAxis":10`, `"yAxis":18`, `"name":"high"`, `"yAxis":-9`} {
		if !strings.Contains(out, want) {
			t.Errorf("vband markArea missing %q: %s", want, out)
		}
	}
}

func TestEChartsVBandsFollowTheirValueAxis(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "primary", Values: []float64{1, 2}},
			{Label: "secondary", Values: []float64{10, 20}, Axis: "y2"},
		},
		VBands: []viewspec.VBand{
			{From: 1, To: 2, Axis: "y", Label: "primary band"},
			{From: 10, To: 20, Axis: "y2", Label: "secondary band"},
		},
	}
	opt := echartsOptionForTest(t, res)
	series := opt["series"].([]any)
	primary := series[0].(map[string]any)
	secondary := series[1].(map[string]any)
	primaryArea, _ := json.Marshal(primary["markArea"])
	secondaryArea, _ := json.Marshal(secondary["markArea"])
	if !strings.Contains(string(primaryArea), "primary band") || strings.Contains(string(primaryArea), "secondary band") {
		t.Fatalf("primary vband attached to wrong series: %s", primaryArea)
	}
	if !strings.Contains(string(secondaryArea), "secondary band") || strings.Contains(string(secondaryArea), "primary band") {
		t.Fatalf("secondary vband attached to wrong series: %s", secondaryArea)
	}
}

func TestEChartsSeriesColorsAndStyle(t *testing.T) {
	op := 0.42
	w := 2.4
	res := viewspec.Resolved{
		Spec: viewspec.Spec{Encoding: viewspec.Encoding{
			Color: &viewspec.Channel{Field: "entity", Type: viewspec.Nominal, Opacity: &op,
				Colors: map[string]string{"buy": "#2d6a4f"}},
			Y: []viewspec.Channel{{Field: "v"}, {Field: "idx", Mark: viewspec.MarkLine, Width: &w, Dash: "dashed", AxisName: "z"}},
		}}, Chart: viewspec.ChartBar,
		Labels:      []string{"a"},
		ColorSeries: 1,
		Series: []viewspec.Series{
			{Label: "buy", Values: []float64{1}},
			{Label: "idx", Values: []float64{9}, Mark: viewspec.ChartLine},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"color":"#2d6a4f"`, `"opacity":0.42`, `"width":2.4`, `"type":"dashed"`, `"name":"z"`} {
		if !strings.Contains(out, want) {
			t.Errorf("series style missing %q: %s", want, out)
		}
	}
}

func TestEChartsCandlestickExtraAppliesSeriesStyleAndAxisName(t *testing.T) {
	opacity := 0.42
	width := 2.4
	res := viewspec.Resolved{
		Spec: viewspec.Spec{Encoding: viewspec.Encoding{Y: []viewspec.Channel{{
			Field: "ma", Mark: viewspec.MarkLine, Opacity: &opacity, Width: &width, Dash: "dashed", AxisName: "price",
		}}}}, Chart: viewspec.ChartCandlestick,
		Labels: []string{"a"},
		Series: []viewspec.Series{
			{Label: "open", Values: []float64{10}}, {Label: "high", Values: []float64{12}},
			{Label: "low", Values: []float64{8}}, {Label: "close", Values: []float64{11}},
			{Label: "ma", Values: []float64{10}, Mark: viewspec.ChartLine},
		},
	}
	opt := echartsOptionForTest(t, res)
	axis := opt["yAxis"].([]any)[0].(map[string]any)
	if axis["name"] != "price" {
		t.Fatalf("candlestick axis name = %#v", axis)
	}
	extra := opt["series"].([]any)[1].(map[string]any)
	lineStyle := extra["lineStyle"].(map[string]any)
	if lineStyle["width"] != width || lineStyle["type"] != "dashed" || lineStyle["opacity"] != opacity {
		t.Fatalf("candlestick extra style = %#v", extra)
	}
}

func TestEChartsThirdAxisY3(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartBar,
		Labels: []string{"a", "b"},
		Series: []viewspec.Series{
			{Label: "count", Values: []float64{1, 2}},
			{Label: "first", Values: []float64{100, 110}, Mark: viewspec.ChartLine, Axis: "y2"},
			{Label: "second", Values: []float64{10, 12}, Mark: viewspec.ChartLine, Axis: "y3"},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	opt := echartsOptionForTest(t, res)
	yAxes := opt["yAxis"].([]any)
	if len(yAxes) != 3 {
		t.Fatalf("want 3 value axes, got %d: %s", len(yAxes), out)
	}
	y3 := yAxes[2].(map[string]any)
	if y3["position"] != "right" || y3["offset"] != 60.0 {
		t.Fatalf("y3 axis should sit right with an offset: %+v", y3)
	}
	for _, s := range opt["series"].([]any) {
		sm := s.(map[string]any)
		switch sm["name"] {
		case "second":
			if sm["yAxisIndex"] != float64(2) {
				t.Fatalf("y3 series should target axis index 2: %+v", sm)
			}
		case "first":
			if sm["yAxisIndex"] != float64(1) {
				t.Fatalf("y2 series should target axis index 1: %+v", sm)
			}
		case "count":
			if _, has := sm["yAxisIndex"]; has {
				t.Fatalf("primary series should not set yAxisIndex: %+v", sm)
			}
		}
	}
}

func TestEChartsTimelinePointColors(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{Encoding: viewspec.Encoding{
			Color: &viewspec.Channel{Field: "side", Type: viewspec.Nominal,
				Colors: map[string]string{"buy": "#2d6a4f"}},
		}}, Chart: viewspec.ChartTimeline,
		Grid: &viewspec.Grid{
			Cols: []string{"d1", "d2"}, Rows: []string{"L1", "L2"},
			Cells: []viewspec.Cell{
				{Col: 0, Row: 0, Value: 1, Color: "buy"},
				{Col: 1, Row: 0, Value: 2, Color: "sell"},
			},
		},
	}
	out, err := EChartsOptionJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"itemStyle":{"color":"#2d6a4f"}`) {
		t.Fatalf("buy dot should use its explicit color: %s", out)
	}
	if !strings.Contains(out, `"itemStyle":{"color":"#4e79a7"}`) {
		t.Fatalf("sell dot should take the next palette slot: %s", out)
	}
}
