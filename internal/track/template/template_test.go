package template

import (
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

func testCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		VaultDir:   t.TempDir(),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
	}
}

func TestDefaultSpecFallsBackToBuiltin(t *testing.T) {
	cfg := testCfg(t)
	// With no user templates and no config, the reserved builtin names resolve.
	if spec, err := DefaultSpec(cfg, config.KindNote); err != nil || spec != "default" {
		t.Fatalf("note default spec = %q, %v; want \"default\"", spec, err)
	}
	if spec, err := DefaultSpec(cfg, config.KindJournal); err != nil || spec != "journal" {
		t.Fatalf("journal default spec = %q, %v; want \"journal\"", spec, err)
	}
}

func TestConfiguredJournalTemplateWins(t *testing.T) {
	cfg := testCfg(t)
	cfg.JournalTemplate = "journal"
	if spec, err := DefaultSpec(cfg, config.KindJournal); err != nil || spec != "journal" {
		t.Fatalf("configured journal spec = %q, %v", spec, err)
	}
}

func TestRenderBuiltinJournal(t *testing.T) {
	cfg := testCfg(t)
	day := time.Date(2026, 6, 22, 0, 0, 0, 0, time.Local)
	body, err := Render(cfg, "journal", "20260622", 20260622, config.KindJournal, "", day)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// The builtin journal is just the title (the journal name is already the date), so the rendered
	// body is the expanded heading and nothing else.
	if body != "# 20260622\n" {
		t.Fatalf("unexpected rendered journal body: %q", body)
	}
}

func TestListEmptyVault(t *testing.T) {
	cfg := testCfg(t)
	refs, err := List(cfg)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no user templates, got %+v", refs)
	}
}
