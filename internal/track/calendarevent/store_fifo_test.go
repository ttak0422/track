package calendarevent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// A separate process lets a regression fail without leaving a reader or a save
// lock blocked inside the test runner. No writer ever opens the synthetic FIFO.
func TestFIFORecords(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"get", "list", "save"} {
		t.Run(action, func(t *testing.T) {
			vault := t.TempDir()
			dir := filepath.Join(vault, "calendar", "events")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(filepath.Join(dir, testID+".json"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestFIFORecordProcessHelper$")
			cmd.Env = append(os.Environ(), "TRACK_EVENT_FIFO_VAULT="+vault, "TRACK_EVENT_FIFO_ACTION="+action)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("%s blocked on a FIFO without a writer: %v", action, ctx.Err())
			}
			if err != nil {
				t.Fatalf("%s: %v\n%s", action, err, out)
			}
		})
	}
}

func TestFIFORecordProcessHelper(t *testing.T) {
	vault := os.Getenv("TRACK_EVENT_FIFO_VAULT")
	if vault == "" {
		return
	}
	cfg := &config.Config{VaultDir: vault}
	var err error
	switch os.Getenv("TRACK_EVENT_FIFO_ACTION") {
	case "get":
		_, err = Get(cfg, testID)
	case "list":
		_, err = List(cfg, true)
	case "save":
		_, err = Save(cfg, testID, request(allDay(), 0, testOp))
	default:
		t.Fatal("unknown FIFO helper action")
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("non-regular record must report corruption, got %v", err)
	}
	// A failed save must release its exclusive lock so a later valid write works.
	if _, err := Save(cfg, op(300), request(allDay(), 0, op(301))); err != nil {
		t.Fatalf("valid save after FIFO rejection: %v", err)
	}
}
