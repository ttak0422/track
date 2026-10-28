package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/calendarevent"
)

func TestEventCLIRoundtrip(t *testing.T) {
	vault := t.TempDir()
	id := "30000000-0000-4000-8000-000000000001"
	op := "30000000-0000-4000-8000-000000000002"
	fields := calendarevent.Fields{Title: "Review", AllDay: true, Start: "2026-10-29", End: "2026-11-01", Status: "confirmed"}
	encode := func() string {
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	out, code := runInWithStdin(t, vault, encode(), "event", "save", "--id", id, "--expect", "0", "--operation", op)
	if code != 0 || out["event"].(map[string]any)["revision"] != float64(1) {
		t.Fatalf("%d %v", code, out)
	}
	if out, code = runInWithStdin(t, vault, encode(), "event", "save", "--id", id, "--expect", "0", "--operation", op); code != 0 || out["event"].(map[string]any)["revision"] != float64(1) {
		t.Fatalf("retry %d %v", code, out)
	}
	if out, code = runIn(t, vault, "event", "get", "--id", id); code != 0 || out["event"].(map[string]any)["title"] != "Review" {
		t.Fatalf("get %d %v", code, out)
	}
	fields.Deleted = true
	if out, code = runInWithStdin(t, vault, encode(), "event", "save", "--id", id, "--expect", "1", "--operation", "30000000-0000-4000-8000-000000000003"); code != 0 {
		t.Fatalf("delete %d %v", code, out)
	}
	if out, code = runIn(t, vault, "event", "list"); code != 0 || len(out["events"].([]any)) != 0 {
		t.Fatalf("list %d %v", code, out)
	}
	if out, code = runIn(t, vault, "event", "list", "--deleted"); code != 0 || len(out["events"].([]any)) != 1 {
		t.Fatalf("trash %d %v", code, out)
	}
	fields.Deleted = false
	if out, code = runInWithStdin(t, vault, encode(), "event", "save", "--id", id, "--expect", "2", "--operation", "30000000-0000-4000-8000-000000000004"); code != 0 || out["event"].(map[string]any)["revision"] != float64(3) {
		t.Fatalf("restore %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(vault, "note")); !os.IsNotExist(err) {
		t.Fatal("event CLI created note directory")
	}
	if _, err := os.Stat(filepath.Join(vault, ".track", "notes")); !os.IsNotExist(err) {
		t.Fatal("event CLI wrote note metadata")
	}
}

func TestEventCLIRejectsInvalidInput(t *testing.T) {
	vault := t.TempDir()
	id := "30000000-0000-4000-8000-000000000001"
	op := "30000000-0000-4000-8000-000000000002"
	for _, args := range [][]string{{"event"}, {"event", "purge"}, {"event", "get"}, {"event", "get", "--id", "../outside"}, {"event", "list", "--id", id}, {"event", "save", "--id", id}, {"event", "save", "--id", id, "--expect", "0", "--operation", op}} {
		out, code := runInWithStdin(t, vault, "{", args...)
		if code == 0 || out["error"] == nil {
			t.Fatalf("%v: %d %v", args, code, out)
		}
	}
	input, err := os.CreateTemp(t.TempDir(), "oversize-input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString(strings.Repeat(" ", calendarevent.MaxRequestBytes+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = input
	out, code := runIn(t, vault, "event", "save", "--id", id, "--expect", "0", "--operation", op)
	os.Stdin = previous
	if code == 0 || !strings.Contains(out["error"].(string), "exceeds") {
		t.Fatalf("%d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(vault, "calendar")); !os.IsNotExist(err) {
		t.Fatal("invalid CLI input wrote storage")
	}
}
