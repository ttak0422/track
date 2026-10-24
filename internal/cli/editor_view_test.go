package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/editorview"
	"github.com/ttak0422/track/internal/track/note"
)

func editorCLIFile(t *testing.T, vault string, id int64, title string) string {
	t.Helper()
	cfg := &config.Config{VaultDir: vault, Extensions: []string{".md"}}
	os.MkdirAll(filepath.Dir(cfg.NotePath(id)), 0700)
	if err := os.WriteFile(cfg.NotePath(id), []byte("body\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
	return cfg.NotePath(id)
}
func TestEditorPathCanonicalContractAndSafeCheck(t *testing.T) {
	vault := t.TempDir()
	path := editorCLIFile(t, vault, 100, "日本語 100% #Title")
	t.Setenv(editorview.Env, filepath.Join(t.TempDir(), "view"))
	row, code := runIn(t, vault, "editor-path", "--path", path)
	if code != 0 {
		t.Fatal(row)
	}
	if row["path"] != path || row["id"] != float64(100) || row["editor_path"] == path {
		t.Fatalf("bad result %+v", row)
	}
	alias := row["editor_path"].(string)
	checked, code := runIn(t, vault, "editor-path", "--path", alias, "--check")
	if code != 0 || checked["path"] != path {
		t.Fatalf("check %+v code %d", checked, code)
	}
	meta, code := runIn(t, vault, "meta", "--path", alias)
	if code != 0 || meta["title"] != "日本語 100% #Title" {
		t.Fatalf("ordinary --path %+v code %d", meta, code)
	}
	os.Remove(alias)
	os.WriteFile(alias, []byte("recovery"), 0600)
	row, code = runIn(t, vault, "editor-path", "--path", alias, "--check")
	if code == 0 || !strings.Contains(row["error"].(string), "recovery conflict") {
		t.Fatalf("accepted replaced alias %+v", row)
	}
	raw, _ := os.ReadFile(alias)
	master, _ := os.ReadFile(path)
	if string(raw) != "recovery" || string(master) != "body\n" {
		t.Fatal("recovery changed data")
	}
}
func TestEditorAliasSelectsItsOwnVault(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	editorCLIFile(t, a, 100, "Wrong vault")
	pathB := editorCLIFile(t, b, 100, "Right vault")
	t.Setenv(editorview.Env, filepath.Join(t.TempDir(), "view"))
	row, code := runIn(t, b, "editor-path", "--path", pathB)
	if code != 0 {
		t.Fatal(row)
	}
	alias := row["editor_path"].(string)
	row, code = runIn(t, a, "meta", "--path", alias)
	if code != 0 || row["title"] != "Right vault" {
		t.Fatalf("alias used default vault %+v code %d", row, code)
	}
	row, code = runIn(t, a, "editor-path", "--path", pathB, "--check")
	if code != 0 || row["vault_dir"] != b {
		t.Fatalf("canonical target used default vault %+v", row)
	}
}
func TestEditorPathExplicitVaultRejectsOtherAliasAndSourceFormat(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	pathA := editorCLIFile(t, a, 100, "Source")
	pathB := editorCLIFile(t, b, 100, "Target")
	os.WriteFile(filepath.Join(a, ".track", "config.yml"), []byte("link_format: id-v1\n"), 0600)
	machine := filepath.Join(t.TempDir(), "config.yml")
	raw := []byte("default_vault: a\nvaults:\n  a: " + a + "\n  b: " + b + "\n")
	os.WriteFile(machine, raw, 0600)
	t.Setenv("TRACK_CONFIG", machine)
	t.Setenv("TRACK_VAULT", a)
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	t.Setenv(editorview.Env, filepath.Join(t.TempDir(), "view"))
	call := func(args ...string) (map[string]any, int) {
		out, code := capture(t, func() int { return Run(args) })
		var row map[string]any
		if err := json.Unmarshal([]byte(out), &row); err != nil {
			t.Fatal(err)
		}
		return row, code
	}
	row, code := call("editor-path", "--path", pathB, "--source-path", pathA)
	if code != 0 || row["wikilink"] != "[[b:id:100|Target]]" {
		t.Fatalf("source link %+v code %d", row, code)
	}
	alias := row["editor_path"].(string)
	row, code = call("meta", "--vault", "a", "--path", alias)
	if code == 0 || !strings.Contains(row["error"].(string), "different vault") {
		t.Fatalf("explicit mismatch %+v", row)
	}
}
func TestEditorViewEnumeratesAndKeepsCanonicalPaths(t *testing.T) {
	vault := t.TempDir()
	path := editorCLIFile(t, vault, 100, "Title")
	t.Setenv(editorview.Env, filepath.Join(t.TempDir(), "view"))
	row, code := runIn(t, vault, "editor-view")
	if code != 0 {
		t.Fatal(row)
	}
	entries := row["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries %+v", row)
	}
	entry := entries[0].(map[string]any)
	if entry["path"] != path || entry["editor_path"] == path {
		t.Fatalf("canonical path lost %+v", entry)
	}
}
