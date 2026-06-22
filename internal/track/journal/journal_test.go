package journal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

func setup(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:          vault,
		DBPath:            filepath.Join(vault, ".track", "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return cfg, s
}

func TestOpenCreatesThenReopens(t *testing.T) {
	cfg, s := setup(t)
	day := time.Date(2026, 6, 22, 9, 0, 0, 0, time.Local)

	res, err := Open(cfg, s, day, Options{
		CreateBody: func(name string, _ int64, _ time.Time) (string, error) {
			return "# " + name, nil
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !res.Created || res.Name != "20260622" || res.Date != "2026-06-22" || res.NoteID != 20260622 {
		t.Fatalf("unexpected create result: %+v", res)
	}
	// The create body is written with a trailing newline added.
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "# 20260622\n" {
		t.Fatalf("journal body = %q", raw)
	}
	// The month and year summaries link the day and month.
	monthRaw, err := os.ReadFile(cfg.JournalPath("202606"))
	if err != nil {
		t.Fatalf("month summary: %v", err)
	}
	if !strings.Contains(string(monthRaw), "[[20260622]]") {
		t.Fatalf("month summary missing day link: %q", monthRaw)
	}

	// Reopening is idempotent: not created, and CreateBody must not run.
	res2, err := Open(cfg, s, day, Options{
		CreateBody: func(string, int64, time.Time) (string, error) {
			t.Fatal("CreateBody should not run when reopening an existing journal")
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if res2.Created || res2.NoteID != res.NoteID {
		t.Fatalf("unexpected reopen result: %+v", res2)
	}
}

func TestOpenEmptyBodyWhenNoCreateBody(t *testing.T) {
	cfg, s := setup(t)
	day := time.Date(2026, 6, 23, 0, 0, 0, 0, time.Local)

	res, err := Open(cfg, s, day, Options{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatalf("journal with no CreateBody should be empty, got %q", raw)
	}
}
