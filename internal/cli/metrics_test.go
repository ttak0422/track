package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, dir, name, data string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMetricsScrapeDeriveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exp := "# TYPE market_close gauge\nmarket_close{symbol=\"T\"} 100\n"
	in := writeTemp(t, dir, "in.txt", exp)
	out := filepath.Join(dir, "m.jsonl")
	raw, code := capture(t, func() int {
		return Run([]string{"metrics", "scrape", "--from", in, "--out", out, "--asof", "2026-09-05"})
	})
	if code != 0 {
		t.Fatalf("scrape failed: %q", raw)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("scrape output is not JSON: %q", raw)
	}
	if res["records"] != float64(1) {
		t.Fatalf("want 1 record, got %v", res)
	}

	prices := writeTemp(t, dir, "p.jsonl",
		`{"version":1,"entity":"T","time":"2026-09-01","open":9,"high":11,"low":9,"close":10}`+"\n"+
			`{"version":1,"entity":"T","time":"2026-09-02","open":10,"high":12,"low":10,"close":11}`+"\n")
	dout := filepath.Join(dir, "d.jsonl")
	raw, code = capture(t, func() int {
		return Run([]string{"metrics", "derive", "--prices", prices, "--out", dout, "--gauges", "change_pct"})
	})
	if code != 0 {
		t.Fatalf("derive failed: %q", raw)
	}
	body, _ := os.ReadFile(dout)
	if !strings.Contains(string(body), `"name":"change_pct"`) || !strings.Contains(string(body), `"value":10`) {
		t.Fatalf("bad derive output: %s", body)
	}
}

func TestMetricsDashboardRejectsOutsideSubset(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.Mkdir(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTemp(t, dataDir, "m.jsonl", `{"version":1,"name":"v","time":"d1","value":1}`+"\n")
	heat := writeTemp(t, dir, "heat.json", `{"title":"H","panels":[{"type":"heatmap","title":"H","datasource":{"type":"track","uid":"m.jsonl"},"targets":[{"expr":"v"}]}]}`)
	raw, code := capture(t, func() int {
		return Run([]string{"metrics", "dashboard", "--dashboard", heat, "--out", filepath.Join(dir, "n.md"), "--data-dir", dataDir})
	})
	if code == 0 {
		t.Fatalf("heatmap panel should fail, got %q", raw)
	}
}

func TestMetricsAlertCapturesToNote(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "init"); code != 0 {
		t.Fatal("init failed")
	}
	dataDir := filepath.Join(vault, "data") // created by init
	writeTemp(t, dataDir, "m.jsonl",
		`{"version":1,"name":"rsi14","entity":"T","time":"2026-09-03","value":28}`+"\n"+
			`{"version":1,"name":"rsi14","entity":"T","time":"2026-09-04","value":27}`+"\n")
	if _, code := runIn(t, vault, "new", "--title", "Monitor", "--body", "## Signals\n"); code != 0 {
		t.Fatal("new note failed")
	}
	rules := writeTemp(t, vault, "rules.yaml",
		"groups:\n- name: g\n  rules:\n  - alert: Oversold\n    expr: rsi14 < 30\n    for: 2\n")
	raw, code := runIn(t, vault, "metrics", "alert", "--rules", rules, "--capture", "Monitor#Signals")
	if code != 0 {
		t.Fatalf("alert failed: %v", raw)
	}
	if raw["count"] != float64(1) {
		t.Fatalf("want 1 firing, got %v", raw)
	}
	matches, err := filepath.Glob(filepath.Join(vault, "note", "*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("want 1 note, got %v %v", matches, err)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Oversold") || !strings.Contains(string(body), "## Signals") {
		t.Fatalf("signal not captured:\n%s", body)
	}
}
