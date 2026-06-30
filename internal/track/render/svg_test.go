package render

import (
	"flag"
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
			Spec:   viewspec.Spec{Version: 1, Type: viewspec.ChartLine, Title: "Line"},
			Labels: xy,
			Series: []viewspec.Series{{Label: "S1", Values: []float64{1, math.NaN(), 3}}},
		},
		"bar": {
			Spec:   viewspec.Spec{Version: 1, Type: viewspec.ChartBar, Title: "Bar"},
			Labels: xy,
			Series: []viewspec.Series{
				{Label: "S1", Values: []float64{1, 2, 3}},
				{Label: "S2", Values: []float64{3, 2, 1}},
			},
		},
		"scatter": {
			Spec:   viewspec.Spec{Version: 1, Type: viewspec.ChartScatter, Title: "Scatter"},
			Labels: xy,
			Series: []viewspec.Series{{Label: "S1", Values: []float64{1, 2, 3}}},
		},
		"bubble": {
			Spec: viewspec.Spec{Version: 1, Type: viewspec.ChartBubble, Title: "Bubble"},
			Series: []viewspec.Series{{Label: "S1", Points: []viewspec.Point{
				{X: 1, Y: 2, R: 5},
				{X: 3, Y: 4, R: 12},
				{X: 2, Y: math.NaN(), R: 4}, // missing y is skipped, not plotted at the origin
			}}},
		},
		"hbar": {
			Spec:   viewspec.Spec{Version: 1, Type: viewspec.ChartHBar, Title: "Ranking"},
			Labels: xy,
			Series: []viewspec.Series{{Label: "S1", Values: []float64{3, 2, 1}}},
		},
		"line-overlay": {
			Spec:    viewspec.Spec{Version: 1, Type: viewspec.ChartLine, Title: "Pressure"},
			Labels:  xy,
			Series:  []viewspec.Series{{Label: "Index", Values: []float64{1, 2, 3}}},
			Markers: []viewspec.Marker{{At: "b", Label: "event"}},
		},
		"heatmap": {
			Spec: viewspec.Spec{Version: 1, Type: viewspec.ChartHeatmap, Title: "Heat"},
			Grid: &viewspec.Grid{
				Cols:  []string{"Q1", "Q2"},
				Rows:  []string{"Tech", "Energy"},
				Cells: []viewspec.Cell{{Col: 0, Row: 0, Value: 1}, {Col: 1, Row: 0, Value: 9}, {Col: 0, Row: 1, Value: math.NaN()}},
			},
		},
		"timeline": {
			Spec: viewspec.Spec{Version: 1, Type: viewspec.ChartTimeline, Title: "Events"},
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
