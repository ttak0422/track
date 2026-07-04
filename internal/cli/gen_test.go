package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRmMovesNoteToTrashAndReindexes(t *testing.T) {
	vault := t.TempDir()

	if _, code := runIn(t, vault, "new", "--title", "捨てるノート", "--id", "1000"); code != 0 {
		t.Fatal("new failed")
	}

	removed, code := runIn(t, vault, "rm", "--title", "捨てるノート")
	if code != 0 {
		t.Fatalf("rm failed: %v", removed)
	}
	if removed["id"].(float64) != 1000 {
		t.Fatalf("unexpected id: %v", removed["id"])
	}
	trash := removed["trash"].(string)
	if _, err := os.Stat(trash); err != nil {
		t.Fatalf("trashed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "note", "1000.md")); !os.IsNotExist(err) {
		t.Fatal("note file should be gone from the vault")
	}
	entries, err := os.ReadDir(filepath.Join(vault, ".track", "trash"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("trash should hold the note and its sidecar, got %v (err %v)", entries, err)
	}

	resolved, code := runIn(t, vault, "resolve", "--term", "捨てるノート")
	if code != 0 {
		t.Fatalf("resolve failed: %v", resolved)
	}
	if resolved["found"] != false {
		t.Fatalf("removed note should not resolve: %v", resolved)
	}
}

func TestGenUndoRedoFlowThroughCLI(t *testing.T) {
	vault := t.TempDir()

	if _, code := runInWithStdin(t, vault, "v1\n", "new", "--title", "世代ノート", "--id", "1000"); code != 0 {
		t.Fatal("new failed")
	}
	inc, code := runIn(t, vault, "gen", "increment")
	if code != 0 || inc["gen"].(float64) != 1 || inc["changed"] != true {
		t.Fatalf("first increment: %v", inc)
	}

	if _, code := runInWithStdin(t, vault, "v2\n", "update", "--id", "1000"); code != 0 {
		t.Fatal("update failed")
	}
	if inc, code = runIn(t, vault, "gen", "increment"); code != 0 || inc["gen"].(float64) != 2 {
		t.Fatalf("second increment: %v", inc)
	}

	undo, code := runIn(t, vault, "gen", "undo")
	if code != 0 || undo["gen"].(float64) != 1 {
		t.Fatalf("undo: %v", undo)
	}
	body, err := os.ReadFile(filepath.Join(vault, "note", "1000.md"))
	if err != nil || !strings.Contains(string(body), "v1") {
		t.Fatalf("after undo body = %q (err %v), want v1", body, err)
	}

	// The rebuilt index still serves queries after the wholesale restore.
	resolved, code := runIn(t, vault, "resolve", "--term", "世代ノート")
	if code != 0 || resolved["found"] != true {
		t.Fatalf("resolve after undo: %v", resolved)
	}

	redo, code := runIn(t, vault, "gen", "redo")
	if code != 0 || redo["gen"].(float64) != 2 {
		t.Fatalf("redo: %v", redo)
	}
	body, err = os.ReadFile(filepath.Join(vault, "note", "1000.md"))
	if err != nil || !strings.Contains(string(body), "v2") {
		t.Fatalf("after redo body = %q (err %v), want v2", body, err)
	}

	list, code := runIn(t, vault, "gen", "list")
	if code != 0 {
		t.Fatalf("list: %v", list)
	}
	if list["cursor"].(float64) != 2 || len(list["generations"].([]any)) != 2 {
		t.Fatalf("list = %v, want cursor 2 over two generations", list)
	}
}

func TestGenPeekPrintsSnapshotContent(t *testing.T) {
	vault := t.TempDir()

	if _, code := runInWithStdin(t, vault, "old body\n", "new", "--title", "peekノート", "--id", "1000"); code != 0 {
		t.Fatal("new failed")
	}
	if _, code := runIn(t, vault, "gen", "increment"); code != 0 {
		t.Fatal("increment failed")
	}
	if _, code := runInWithStdin(t, vault, "new body\n", "update", "--id", "1000"); code != 0 {
		t.Fatal("update failed")
	}

	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(vault, ".test-cache"))
	out, code := capture(t, func() int {
		return Run([]string{"gen", "peek", "--gen", "1", "--title", "peekノート"})
	})
	if code != 0 {
		t.Fatalf("peek failed: %q", out)
	}
	if !strings.Contains(out, "old body") {
		t.Fatalf("peek = %q, want the generation-1 body", out)
	}

	// rm the note, then peek it back out of the snapshot by id: selective restore for a dream review.
	if _, code := runIn(t, vault, "rm", "--id", "1000"); code != 0 {
		t.Fatal("rm failed")
	}
	out, code = capture(t, func() int {
		return Run([]string{"gen", "peek", "--gen", "1", "--id", "1000"})
	})
	if code != 0 || !strings.Contains(out, "old body") {
		t.Fatalf("peek of removed note = %q (code %d), want snapshot body", out, code)
	}
}
