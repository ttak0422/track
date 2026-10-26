package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestColdCacheCommandsPreserveExistingTitles(t *testing.T) {
	for _, command := range []string{"resolve", "keywords", "open", "new"} {
		t.Run(command, func(t *testing.T) {
			vault := t.TempDir()
			if err := os.MkdirAll(filepath.Join(vault, "note"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(vault, ".track", "notes"), 0o755); err != nil {
				t.Fatal(err)
			}
			body := []byte("Historical body\n")
			meta := []byte("version: 1\ntitle: Existing\ndays:\n  - 2020-01-01\n")
			bodyPath := filepath.Join(vault, "note", "100.md")
			metaPath := filepath.Join(vault, ".track", "notes", "100.yaml")
			if err := os.WriteFile(bodyPath, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(metaPath, meta, 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{command}
			if command == "resolve" {
				args = append(args, "--term", "Existing")
			}
			if command == "open" || command == "new" {
				args = append(args, "--title", "Existing")
			}
			out, code := runInWithStdin(t, vault, "", args...)
			switch command {
			case "resolve":
				if code != 0 || out["found"] != true || out["note_id"] != float64(100) {
					t.Fatalf("resolve: %d %+v", code, out)
				}
			case "keywords":
				if code != 0 || len(out["keywords"].([]any)) != 1 {
					t.Fatalf("keywords: %d %+v", code, out)
				}
			case "open":
				if code != 0 || out["created"] != false || out["id"] != float64(100) {
					t.Fatalf("open duplicated existing note: %d %+v", code, out)
				}
			case "new":
				if code == 0 || !strings.Contains(out["error"].(string), "already exists") {
					t.Fatalf("new accepted duplicate: %d %+v", code, out)
				}
			}
			files, err := os.ReadDir(filepath.Join(vault, "note"))
			if err != nil || len(files) != 1 {
				t.Fatalf("note files changed: %v, %v", files, err)
			}
			for path, want := range map[string][]byte{bodyPath: body, metaPath: meta} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("bootstrap changed %s: %q, %v", path, got, err)
				}
			}
		})
	}
}

func TestColdCacheFailureStopsNewBeforeWriting(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".track"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "note"), []byte("unreadable note tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := runInWithStdin(t, vault, "", "new", "--title", "Unsafe")
	if code == 0 || !strings.Contains(out["error"].(string), "initialize index") {
		t.Fatalf("bootstrap failure was ignored: %d %+v", code, out)
	}
	if _, err := os.Stat(filepath.Join(vault, ".track", "notes")); !os.IsNotExist(err) {
		t.Fatalf("failed bootstrap wrote metadata: %v", err)
	}
}
