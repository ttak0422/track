package journal

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

func testCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		VaultDir:          t.TempDir(),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
}

func TestOpenCreatesThenReopens(t *testing.T) {
	cfg := testCfg(t)
	day := time.Date(2026, 6, 22, 9, 0, 0, 0, time.Local)

	res, err := Open(cfg, day, Options{
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
	// The journal and both summaries are reported for indexing by the caller.
	if !slices.Contains(res.Reindex, cfg.JournalPath("20260622")) ||
		!slices.Contains(res.Reindex, cfg.JournalPath("202606")) ||
		!slices.Contains(res.Reindex, cfg.JournalPath("2026")) {
		t.Fatalf("Reindex should list the journal and summaries: %v", res.Reindex)
	}
	// The create body is written with a trailing newline added.
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "# 20260622\n" {
		t.Fatalf("journal body = %q", raw)
	}

	// Reopening is idempotent: not created, nothing to reindex, and CreateBody must not run.
	res2, err := Open(cfg, day, Options{
		CreateBody: func(string, int64, time.Time) (string, error) {
			t.Fatal("CreateBody should not run when reopening an existing journal")
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if res2.Created || res2.NoteID != res.NoteID || len(res2.Reindex) != 0 {
		t.Fatalf("unexpected reopen result: %+v", res2)
	}
}

func TestOpenEmptyBodyWhenNoCreateBody(t *testing.T) {
	cfg := testCfg(t)
	day := time.Date(2026, 6, 23, 0, 0, 0, 0, time.Local)

	res, err := Open(cfg, day, Options{})
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
