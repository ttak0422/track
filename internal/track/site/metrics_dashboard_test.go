package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/index"
)

func TestBuildMetricsDashboardSnapshot(t *testing.T) {
	cfg, s := vaultStore(t)
	if err := os.MkdirAll(cfg.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir(), "cpu.jsonl"), []byte("{\"version\":1,\"name\":\"cpu\",\"entity\":\"web-a\",\"time\":\"2026-09-19\",\"value\":85}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `{"title":"CPU","panels":[{"type":"stat","datasource":{"type":"track","uid":"cpu.jsonl"},"targets":[{"expr":"cpu"}],"fieldConfig":{"defaults":{"unit":"percent","decimals":1}}},{"type":"timeseries","datasource":{"type":"track","uid":"cpu.jsonl"},"targets":[{"expr":"cpu"}]},{"type":"table","datasource":{"type":"track","uid":"missing.jsonl"},"targets":[{"expr":"cpu"}]}]}`
	body := "```metrics-dashboard\n" + spec + "\n```\n\n```metrics-dashboard\n{}\n```\n\nafter\n"
	writeVaultNote(t, cfg, 100, "Metrics", body)
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100}, fakeFrontend(t), out); err != nil {
		t.Fatal(err)
	}
	page := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	for _, want := range []string{"```metrics-dashboard-snapshot", `"display":"85.0 %"`, `"echarts":`, `"error":`, `missing.jsonl`, "> Metrics dashboard error:", "after"} {
		if !strings.Contains(page.Note.Body, want) {
			t.Fatalf("missing %q in %s", want, page.Note.Body)
		}
	}
	if strings.Contains(page.Note.Body, "```metrics-dashboard\n") {
		t.Fatal("unresolved source fence")
	}
	if _, err := os.Stat(filepath.Join(out, "data", "cpu.jsonl")); !os.IsNotExist(err) {
		t.Fatal("source file must not be published")
	}
}
