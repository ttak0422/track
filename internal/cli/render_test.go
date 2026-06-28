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
	data := `{"version":1,"entity":"AAPL","time":"d1","close":10}` + "\n" +
		`{"version":1,"entity":"MSFT","time":"d1","close":99}` + "\n" +
		`{"version":1,"entity":"AAPL","time":"d2","close":12}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "prices.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `{"version":1,"type":"line","title":"AAPL","data":{"source":"prices.jsonl","kind":"price"},` +
		`"x":{"field":"time"},"y":[{"field":"close"}],"filter":{"field":"entity","equals":"AAPL"}}`
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
	if decoded["path"] != outPath || decoded["renderer"] != "chartjs" {
		t.Fatalf("unexpected result: %v", decoded)
	}

	html, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	// Filter kept only the two AAPL rows, aligned to their close values.
	if !strings.Contains(got, `"labels":["d1","d2"]`) || !strings.Contains(got, `"data":[10,12]`) {
		t.Fatalf("filtered/aligned data not rendered: %s", got)
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
