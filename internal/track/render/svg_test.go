package render

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// updateGolden rewrites the testdata/*.svg golden files instead of comparing against them: run
// `go test ./internal/track/render -run SVGGolden -update` after an intentional rendering change.
var updateGolden = flag.Bool("update", false, "update SVG golden files")

func goldenCases() map[string]viewspec.Resolved {
	xy := []string{"a", "b", "c"}
	return map[string]viewspec.Resolved{
		"line": {
			Spec: viewspec.Spec{Title: "Line"}, Chart: viewspec.ChartLine,
			Labels: xy,
			Series: []viewspec.Series{{Label: "S1", Values: []float64{1, math.NaN(), 3}}},
		},
		"bar": {
			Spec: viewspec.Spec{Title: "Bar"}, Chart: viewspec.ChartBar,
			Labels: xy,
			Series: []viewspec.Series{
				{Label: "S1", Values: []float64{1, 2, 3}},
				{Label: "S2", Values: []float64{3, 2, 1}},
			},
		},
		"scatter": {
			Spec: viewspec.Spec{Title: "Scatter"}, Chart: viewspec.ChartScatter,
			Labels: xy,
			Series: []viewspec.Series{{Label: "S1", Values: []float64{1, 2, 3}}},
		},
		// Area: the line plus a translucent fill down to the zero baseline, broken at NaN gaps.
		"area": {
			Spec: viewspec.Spec{Title: "Area"}, Chart: viewspec.ChartArea,
			Labels: []string{"a", "b", "c", "d"},
			Series: []viewspec.Series{{Label: "S1", Values: []float64{1, 2, math.NaN(), 3}}},
		},
		// Candlestick: series come in the fixed open/high/low/close order; the second candle falls
		// (close < open) so it draws red, and the incomplete third candle is skipped.
		"candlestick": {
			Spec: viewspec.Spec{Title: "OHLC"}, Chart: viewspec.ChartCandlestick,
			Labels: xy,
			Series: []viewspec.Series{
				{Label: "open", Values: []float64{10, 13, 11}},
				{Label: "high", Values: []float64{14, 13, 12}},
				{Label: "low", Values: []float64{9, 8, math.NaN()}},
				{Label: "close", Values: []float64{13, 9, 11}},
			},
		},
		"bubble": {
			Spec: viewspec.Spec{Title: "Bubble"}, Chart: viewspec.ChartBubble,
			Series: []viewspec.Series{{Label: "S1", Points: []viewspec.Point{
				{X: 1, Y: 2, R: 5},
				{X: 3, Y: 4, R: 12},
				{X: 2, Y: math.NaN(), R: 4}, // missing y is skipped, not plotted at the origin
			}}},
		},
		"hbar": {
			Spec: viewspec.Spec{Title: "Ranking"}, Chart: viewspec.ChartHBar,
			Labels: xy,
			Series: []viewspec.Series{{Label: "S1", Values: []float64{3, 2, 1}}},
		},
		// Stacked bars: segments pile up per category (negatives grow down) and the value axis spans
		// the stack totals, not the individual values.
		"bar-stack": {
			Spec: viewspec.Spec{Title: "Stacked"}, Chart: viewspec.ChartBar, Stacked: true,
			Labels: xy,
			Series: []viewspec.Series{
				{Label: "S1", Values: []float64{1, 2, 3}},
				{Label: "S2", Values: []float64{3, math.NaN(), -1}},
			},
		},
		// The resolved shape of a color-channel split: one series per category, labeled with the
		// category value, aligned to the shared x axis with NaN gaps.
		"line-color": {
			Spec: viewspec.Spec{Title: "By entity"}, Chart: viewspec.ChartLine,
			Labels: xy,
			Series: []viewspec.Series{
				{Label: "A", Values: []float64{1, 2, math.NaN()}},
				{Label: "B", Values: []float64{10, math.NaN(), 30}},
			},
		},
		"line-overlay": {
			Spec: viewspec.Spec{Title: "Overlaid"}, Chart: viewspec.ChartLine,
			Labels:  xy,
			Series:  []viewspec.Series{{Label: "Index", Values: []float64{1, 2, 3}}},
			Markers: []viewspec.Marker{{At: "b", Label: "event"}},
			Lines: []viewspec.RefLine{
				{Y: 2.5, Axis: "y", Label: "limit"},
				{Y: 9, Axis: "y", Label: "off-scale"}, // outside the value range → skipped
			},
			Bands: []viewspec.Band{
				{From: "b", To: "c", Label: "period"},
				{From: "x", To: "c"}, // unknown category → skipped
			},
		},
		"heatmap": {
			Spec: viewspec.Spec{Title: "Heat"}, Chart: viewspec.ChartHeatmap,
			Grid: &viewspec.Grid{
				Cols:  []string{"Q1", "Q2"},
				Rows:  []string{"Tech", "Energy"},
				Cells: []viewspec.Cell{{Col: 0, Row: 0, Value: 1}, {Col: 1, Row: 0, Value: 9}, {Col: 0, Row: 1, Value: math.NaN()}},
			},
		},
		"timeline": {
			Spec: viewspec.Spec{Title: "Events"}, Chart: viewspec.ChartTimeline,
			Grid: &viewspec.Grid{
				Cols:  []string{"d1", "d2", "d3"},
				Rows:  []string{"AAPL", "MSFT"},
				Cells: []viewspec.Cell{{Col: 0, Row: 0, Value: 1}, {Col: 2, Row: 0, Value: 5}, {Col: 1, Row: 1, Value: math.NaN()}},
			},
		},
	}
}

func TestSVGGolden(t *testing.T) {
	for name, res := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			got, err := SVG{}.Render(res)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".svg")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if got != string(want) {
				t.Errorf("SVG output for %s differs from golden; run with -update if intended", name)
			}
		})
	}
}

func TestSVGBubbleRenders(t *testing.T) {
	out, err := SVG{}.Render(goldenCases()["bubble"])
	if err != nil {
		t.Fatalf("bubble should render: %v", err)
	}
	// The two finite points draw circles; the NaN-y point is skipped.
	if n := strings.Count(out, "<circle"); n != 2 {
		t.Fatalf("expected 2 bubble circles, got %d", n)
	}
}

func TestSVGThinsDenseCategoryLabels(t *testing.T) {
	// A daily series has far more categories than fit as axis labels; only every step-th is drawn.
	labels := make([]string, 90)
	values := make([]float64, 90)
	for i := range labels {
		labels[i] = fmt.Sprintf("2026-01-%02d", i)
		values[i] = float64(i)
	}
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels: labels,
		Series: []viewspec.Series{{Label: "S", Values: values}},
	}
	out, err := SVG{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	drawn := strings.Count(out, "2026-01-")
	if drawn >= 90 || drawn < 5 {
		t.Fatalf("dense labels should thin to a readable count, drew %d", drawn)
	}
	// The first category is always labeled.
	if !strings.Contains(out, ">2026-01-00<") {
		t.Fatalf("first label missing: %s", out)
	}
}

func TestSVGComboDrawsBarsAndLine(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartBar,
		Labels: []string{"a", "b", "c"},
		Series: []viewspec.Series{
			{Label: "vol", Values: []float64{1, 2, 3}, Mark: viewspec.ChartBar},
			{Label: "idx", Values: []float64{3, 4, 5}, Mark: viewspec.ChartLine},
		},
	}
	out, err := SVG{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	// Bars only for the bar series, plus a polyline for the line series.
	if n := strings.Count(out, "<rect"); n != 1+1+3+2 { // background + border + 3 bars + 2 legend swatches
		t.Fatalf("want 3 bar rects (plus background/border/legend), got %d rects: %s", n, out)
	}
	if !strings.Contains(out, "<polyline") {
		t.Fatalf("line series missing: %s", out)
	}
}

func TestSVGCalloutDrawsBubble(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartLine,
		Labels:   []string{"a", "b", "c"},
		Series:   []viewspec.Series{{Label: "S", Values: []float64{1, 5, 3}}},
		Callouts: []viewspec.Callout{{X: "b", Y: 5, Label: "peak"}, {X: "zz", Y: 5, Label: "unknown x"}},
	}
	out, err := SVG{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	// The known point draws a dot, a leader, and the bubble box with its text.
	for _, want := range []string{"<circle", `rx="3"`, ">peak<"} {
		if !strings.Contains(out, want) {
			t.Errorf("callout output missing %q: %s", want, out)
		}
	}
	// The callout whose x matches no category is skipped.
	if strings.Contains(out, "unknown x") {
		t.Fatalf("unknown-category callout should be skipped: %s", out)
	}
}

func TestSVGStackedHBarPilesSegments(t *testing.T) {
	res := viewspec.Resolved{
		Spec: viewspec.Spec{}, Chart: viewspec.ChartHBar, Stacked: true,
		Labels: []string{"a"},
		Series: []viewspec.Series{
			{Label: "S1", Values: []float64{2}},
			{Label: "S2", Values: []float64{3}},
		},
	}
	out, err := SVG{}.Render(res)
	if err != nil {
		t.Fatal(err)
	}
	// Both segments draw, in their series colors.
	for si := range res.Series {
		if !strings.Contains(out, `fill="`+seriesColor(si)+`"`) {
			t.Errorf("stacked hbar missing segment for series %d", si)
		}
	}
}

func TestSVGCandlestickColorsAndSkips(t *testing.T) {
	out, err := SVG{}.Render(goldenCases()["candlestick"])
	if err != nil {
		t.Fatal(err)
	}
	// Two complete candles: one up (green), one down (red); the NaN-low candle is skipped and the
	// OHLC component series never show up as a legend.
	if n := strings.Count(out, `<rect x="`); n != 3 { // plot border + 2 bodies
		t.Fatalf("expected 2 candle bodies, got %d rects", n-1)
	}
	if !strings.Contains(out, candleUp) || !strings.Contains(out, candleDown) {
		t.Fatalf("candles should color up/down: %s", out)
	}
	if strings.Contains(out, ">open<") || strings.Contains(out, ">close<") {
		t.Fatalf("OHLC components should not be legend entries: %s", out)
	}
}

func TestSVGSelfContainedNoCDN(t *testing.T) {
	out, err := SVG{}.Render(goldenCases()["line"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "cdn") || strings.Contains(out, "<script") {
		t.Fatalf("svg output must be static (no script/CDN): %s", out)
	}
	if !strings.HasPrefix(out, "<?xml") {
		t.Fatalf("svg output should start with xml decl: %.40s", out)
	}
}

func TestSVGRegistered(t *testing.T) {
	if _, err := Get("svg"); err != nil {
		t.Fatalf("svg should be registered: %v", err)
	}
}
