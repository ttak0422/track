package note

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// repeatNow is the completion moment the write-path test completes the repeating task at.
var repeatNow = time.Date(2026, 7, 11, 14, 30, 0, 0, time.UTC)

// TestApplyTaskStateRollsRepeat drives the shared write path end to end: completing a repeating
// task writes the rolled [sched:] date into the note file and appends the state transition to the
// sidecar task log — the history survives without the body needing a new token.
func TestApplyTaskStateRollsRepeat(t *testing.T) {
	cfg := &config.Config{
		VaultDir:   t.TempDir(),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
	}
	notePath := cfg.NotePath(700)
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Loop [0/1]\n\n- [ ] standup [rpt:1w] [sched:2026-07-06]\n"
	if err := os.WriteFile(notePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	tr, err := ApplyTaskState(cfg, notePath, 3, "DONE", "", repeatNow)
	if err != nil {
		t.Fatal(err)
	}
	if tr.To != "DONE" || tr.From != "TODO" {
		t.Fatalf("unexpected transition: %+v", tr)
	}
	got, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "- [x] standup [rpt:1w] [sched:2026-07-13] [done:2026-07-11]") {
		t.Fatalf("repeat not rolled on the write path: %q", got)
	}
	if !strings.Contains(string(got), "# Loop [1/1]") {
		t.Fatalf("cookie not recomputed: %q", got)
	}

	meta, found, err := ReadMetadata(cfg.MetadataPath(700))
	if err != nil || !found {
		t.Fatalf("read sidecar: found=%v err=%v", found, err)
	}
	if len(meta.TaskLog) != 1 || meta.TaskLog[0].To != "DONE" || meta.TaskLog[0].Text != "standup" {
		t.Fatalf("sidecar task log missing the transition: %+v", meta.TaskLog)
	}
}
