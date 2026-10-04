package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardRecognizesEntirelyMissingSeries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.jsonl")
	if err := os.WriteFile(path, []byte(`{"version":1,"name":"demo","time":"2026-10-01","value":null,"missing_reason":"not published"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := loadMetricFile(dir, "demo.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["value"] != nil {
		t.Fatalf("missing series was dropped: %v", rows)
	}
	data := []byte(`{"title":"Demo","panels":[{"type":"timeseries","title":"Demo metric","datasource":{"type":"track","uid":"demo.jsonl"},"targets":[{"expr":"demo"}]}]}`)
	markdown, count, err := ResolveDashboard(data, dir)
	if err != nil || count != 1 || !strings.Contains(markdown, "demo.jsonl") {
		t.Fatalf("dashboard=%s count=%d err=%v", markdown, count, err)
	}
}

func TestDashboardMetricDiscoveryKeepsGapsWithoutAcceptingInvalidNulls(t *testing.T) {
	dir := t.TempDir()
	data := `{"name":"demo","time":"d1","value":0}
{"name":"demo","time":"d2","value":null,"missing_reason":"outage"}
{"name":"demo","time":"d3","value":null}
{"name":"demo","time":"d4","value":null,"missing_reason":42}
{"name":"demo","time":"d5","missing_reason":"outage"}
{"name":"demo","time":"d6","value":"broken"}
`
	if err := os.WriteFile(filepath.Join(dir, "demo.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := loadMetricFile(dir, "demo.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["time"] != "d1" || rows[1]["time"] != "d2" {
		t.Fatalf("discovered rows=%v", rows)
	}
}
