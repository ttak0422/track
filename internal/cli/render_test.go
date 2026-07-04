package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCommandWritesHTML(t *testing.T) {
	dir := t.TempDir()
	data := `{"version":1,"entity":"AAPL","time":"d1","open":9,"high":11,"low":9,"close":10}` + "\n" +
		`{"version":1,"entity":"MSFT","time":"d1","open":98,"high":100,"low":97,"close":99}` + "\n" +
		`{"version":1,"entity":"AAPL","time":"d2","open":10,"high":13,"low":10,"close":12}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "prices.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `{"version":2,"mark":"line","title":"AAPL","data":{"source":"prices.jsonl","kind":"price"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"close"}]},"filter":{"field":"entity","equals":"AAPL"}}`
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.html")

	raw, code := capture(t, func() int { return Run([]string{"render", "--spec", specPath, "--out", outPath}) })
	if code != 0 {
		t.Fatalf("render failed: %q", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected JSON result, got %q", raw)
	}
	if decoded["path"] != outPath || decoded["renderer"] != "echarts" {
		t.Fatalf("unexpected result: %v", decoded)
	}

	html, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	// Filter kept only the two AAPL rows, aligned to their close values.
	if !strings.Contains(got, `"data":["d1","d2"]`) || !strings.Contains(got, `"data":[10,12]`) {
		t.Fatalf("filtered/aligned data not rendered: %s", got)
	}
}

func TestRenderCommandDrawsOverlayMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"),
		[]byte(`{"version":1,"name":"p","time":"d1","value":10}`+"\n"+`{"version":1,"name":"p","time":"d2","value":20}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"),
		[]byte(`{"version":1,"time":"d2","title":"launch"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `{"version":2,"mark":"line","title":"P","data":{"source":"metrics.jsonl","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"source":"events.jsonl","kind":"event","at":"time","label":"title"}]}`
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.html")

	raw, code := capture(t, func() int { return Run([]string{"render", "--spec", specPath, "--out", outPath}) })
	if code != 0 {
		t.Fatalf("render failed: %q", raw)
	}
	html, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, `"markLine"`) {
		t.Fatalf("overlay should emit a markLine: %s", got)
	}
	if !strings.Contains(got, `"xAxis":"d2"`) || !strings.Contains(got, `"formatter":"launch"`) {
		t.Fatalf("event marker not rendered: %s", got)
	}
}

func TestRenderCommandDrawsLineAndBandOverlays(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"),
		[]byte(`{"version":1,"name":"p","time":"d1","value":10}`+"\n"+`{"version":1,"name":"p","time":"d2","value":20}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Line and band overlays carry literal values — no second data file to read.
	spec := `{"version":2,"mark":"line","title":"P","data":{"source":"metrics.jsonl","kind":"metric"},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"y":15,"label":"limit"},{"from":"d1","to":"d2","label":"period"}]}`
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.html")

	raw, code := capture(t, func() int { return Run([]string{"render", "--spec", specPath, "--out", outPath}) })
	if code != 0 {
		t.Fatalf("render failed: %q", raw)
	}
	html, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{`"yAxis":15`, `"formatter":"limit"`, `"markArea"`, `"xAxis":"d1"`, `"xAxis":"d2"`} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay output missing %q", want)
		}
	}
}

func TestRenderArticleComposesDocument(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metrics.jsonl"),
		[]byte(`{"version":1,"name":"p","time":"d1","value":10}`+"\n"+`{"version":1,"name":"p","time":"d2","value":20}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	art := `{"version":1,"title":"Doc","blocks":[` +
		`{"markdown":"# Intro"},` +
		`{"chart":{"version":2,"mark":"line","data":{"source":"metrics.jsonl","kind":"metric"},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}}` +
		`]}`
	artPath := filepath.Join(dir, "article.json")
	if err := os.WriteFile(artPath, []byte(art), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.html")

	raw, code := capture(t, func() int { return Run([]string{"render", "--spec", artPath, "--out", outPath}) })
	if code != 0 {
		t.Fatalf("render article failed: %q", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected JSON result, got %q", raw)
	}
	if decoded["blocks"] != float64(2) {
		t.Fatalf("expected 2 blocks, got %v", decoded["blocks"])
	}
	html, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, `id="chart-0"`) || !strings.Contains(got, `class="prose"`) {
		t.Fatalf("article structure missing: %s", got)
	}
	if !strings.Contains(got, `"data":[10,20]`) {
		t.Fatalf("chart data not resolved in article: %s", got)
	}
}

func TestRenderHelpListsNotationAndExits0(t *testing.T) {
	out, code := capture(t, func() int { return Run([]string{"render", "--help"}) })
	if code != 0 {
		t.Fatalf("--help should exit 0, got %d: %q", code, out)
	}
	for _, want := range []string{
		"line | bar | point | area | rect",      // marks from viewspec.Marks
		"quantitative | nominal",                // channel types from viewspec.ChannelTypes
		"event | price | metric",                // kinds from dataset.KnownKinds
		"Canonical Data Model",                  // input data format section
		"entity* time* open* high* low* close*", // price fields from dataset.KindFields (required marked)
		"y[].axis:",                             // secondary axis notation
		"overlays[]:",                           // overlay notation
		"echarts | svg",                         // renderer names
		`"source": "metrics.jsonl"`,             // example spec
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestRenderCommandRequiresSpecAndOut(t *testing.T) {
	out, code := capture(t, func() int { return Run([]string{"render", "--out", "x.html"}) })
	if code == 0 || !strings.Contains(out, "spec") {
		t.Fatalf("missing --spec should fail: %q", out)
	}
	out, code = capture(t, func() int { return Run([]string{"render", "--spec", "x.json"}) })
	if code == 0 || !strings.Contains(out, "out") {
		t.Fatalf("missing --out should fail: %q", out)
	}
}
