package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPartialReindexRetainsCacheAndExplicitPrune(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "note"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"1", "2"} {
		if err := os.WriteFile(filepath.Join(vault, "note", name+".md"), []byte("xy needle"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if out, code := runIn(t, vault, "reindex"); code != 0 {
		t.Fatal(out)
	}
	if err := os.Remove(filepath.Join(vault, "note", "2.md")); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "reindex")
	if code != 0 || out["read_state"] == nil {
		t.Fatalf("missing partial status: %+v %d", out, code)
	}
	out, code = runIn(t, vault, "notes")
	if code != 0 || len(out["notes"].([]any)) != 2 || out["read_state"] == nil {
		t.Fatalf("cached row lost: %+v %d", out, code)
	}
	out, code = runIn(t, vault, "reindex", "--prune-missing")
	if code != 0 || out["deleted"] != float64(1) {
		t.Fatalf("confirmed prune: %+v %d", out, code)
	}
	out, code = runIn(t, vault, "notes")
	if code != 0 || len(out["notes"].([]any)) != 1 || out["read_state"] != nil {
		t.Fatalf("cleanup not complete: %+v %d", out, code)
	}
}
