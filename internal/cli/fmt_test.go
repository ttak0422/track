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

func TestFmtAllOrdersSidecarKeys(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "note"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "note", "1.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaDir := filepath.Join(vault, ".track", "notes")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(metaDir, "1.yaml")
	messy := "created: 2026-01-02\ntitle: Hi\nversion: 1\ntags:\n  - a\n"
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
	want := "version: 1\ntitle: Hi\ntags:\n    - a\ncreated: 2026-01-02\n"
	if string(got) != want {
		t.Fatalf("sidecar = %q, want %q", got, want)
	}
}

func TestFmtCheckReportsSidecarWithoutWriting(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "note"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "note", "1.md"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaDir := filepath.Join(vault, ".track", "notes")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(metaDir, "1.yaml")
	messy := "title: Hi\nversion: 1\n"
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
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != messy {
		t.Fatalf("sidecar was modified by --check: %q", got)
	}
}

func TestFmtAllLeavesCanonicalSidecar(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "note"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "note", "1.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	metaDir := filepath.Join(vault, ".track", "notes")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical := "version: 1\ntitle: Hi\ntags:\n    - a\ncreated: 2026-01-02\nprops:\n    status: done\nflags:\n    - CONFIDENTIAL\n"
	if err := os.WriteFile(filepath.Join(metaDir, "1.yaml"), []byte(canonical), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "fmt", "--check", "--all")
	if code != 0 {
		t.Fatalf("fmt --check on canonical vault exit = %d, want 0 (out %v)", code, out)
	}
	if changed, _ := out["changed"].([]any); len(changed) != 0 {
		t.Fatalf("changed = %v, want none", out["changed"])
	}
}

func TestFormatSidecarRefusesNonMapping(t *testing.T) {
	cases := []string{
		"---\nversion: 1\n---\nversion: 2\n", // two documents
		"[1, 2, 3]\n",                        // not a mapping
		"title: Hi\ntitle: Ho\nversion: 1\n", // duplicate key
		"",                                   // empty
	}
	for _, in := range cases {
		if got := formatSidecar([]byte(in)); got != in {
			t.Errorf("formatSidecar(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestFormatSidecarIdempotent(t *testing.T) {
	in := []byte("created: 2026-01-02\ntitle: Hi\nversion: 1\n")
	once := formatSidecar(in)
	if once == string(in) {
		t.Fatalf("formatSidecar did not reorder %q", in)
	}
	if twice := formatSidecar([]byte(once)); twice != once {
		t.Fatalf("not idempotent: once=%q twice=%q", once, twice)
	}
}
