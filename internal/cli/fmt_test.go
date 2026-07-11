package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFmtAllRewritesInPlace(t *testing.T) {
	vault := t.TempDir()
	noteDir := filepath.Join(vault, "note")
	if err := os.MkdirAll(noteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(noteDir, "1.md")
	messy := "# Title\nbody   \n* a\n* b\n\n\n\ntail"
	if err := os.WriteFile(path, []byte(messy), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "fmt", "--all")
	if code != 0 {
		t.Fatalf("fmt --all exit = %d, out %v", code, out)
	}
	changed, _ := out["changed"].([]any)
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want one file", out["changed"])
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\nbody\n- a\n- b\n\ntail\n"
	if string(got) != want {
		t.Fatalf("formatted = %q, want %q", got, want)
	}
}

func TestFmtCheckReportsWithoutWriting(t *testing.T) {
	vault := t.TempDir()
	noteDir := filepath.Join(vault, "note")
	if err := os.MkdirAll(noteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(noteDir, "1.md")
	messy := "a   \nb\n"
	if err := os.WriteFile(path, []byte(messy), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "fmt", "--check", "--all")
	if code != 1 {
		t.Fatalf("fmt --check exit = %d, want 1 (out %v)", code, out)
	}
	if changed, _ := out["changed"].([]any); len(changed) != 1 {
		t.Fatalf("changed = %v, want one file", out["changed"])
	}
	// --check must not modify the file.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != messy {
		t.Fatalf("file was modified by --check: %q", got)
	}
}

func TestFmtCheckCleanExitsZero(t *testing.T) {
	vault := t.TempDir()
	noteDir := filepath.Join(vault, "note")
	if err := os.MkdirAll(noteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noteDir, "1.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, vault, "fmt", "--check", "--all")
	if code != 0 {
		t.Fatalf("fmt --check on clean vault exit = %d, want 0 (out %v)", code, out)
	}
}

func TestFmtRequiresPathsOrAll(t *testing.T) {
	vault := t.TempDir()
	out, code := runIn(t, vault, "fmt")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if _, ok := out["error"]; !ok {
		t.Fatalf("expected an error, got %v", out)
	}
}
