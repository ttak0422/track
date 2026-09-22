package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/site"
)

func TestLiterateDotfilesExample(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "examples", "literate-dotfiles", "dotfiles.md"))
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	if out, code := runIn(t, vault, "init"); code != 0 {
		t.Fatalf("init: %v", out)
	}
	if out, code := runInWithStdin(t, vault, string(body), "new", "--title", "Literate dotfiles", "--id", "100"); code != 0 {
		t.Fatalf("new: %v", out)
	}
	generated := filepath.Join(vault, "note", "generated")
	plan, code := runIn(t, vault, "babel", "tangle", "--id", "100", "--dry-run")
	if code != 0 || plan["dry_run"] != true || len(plan["targets"].([]any)) != 2 {
		t.Fatalf("dry-run: %v", plan)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("dry-run created output directory: %v", err)
	}
	if out, code := runIn(t, vault, "babel", "tangle", "--id", "100"); code != 0 {
		t.Fatalf("tangle: %v", out)
	}
	for path, want := range map[string]string{
		"bin/hello":  "#!/bin/sh\nset -eu\n\nprintf '%s\\n' 'Hello from literate dotfiles.'\n",
		".gitconfig": "[init]\n    defaultBranch = main\n\n[core]\n    editor = vi\n",
	} {
		got, err := os.ReadFile(filepath.Join(generated, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q (%v), want %q", path, got, err, want)
		}
	}
	script := filepath.Join(generated, "bin", "hello")
	if out, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("shell syntax: %s (%v)", out, err)
	}
	if out, err := exec.Command("sh", script).CombinedOutput(); err != nil || string(out) != "Hello from literate dotfiles.\n" {
		t.Fatalf("generated shell command: %q (%v)", out, err)
	}
	markdown, code := capture(t, func() int { return Run([]string{"export", "--id", "100"}) })
	if code != 0 || !strings.Contains(markdown, "```sh\n#!/bin/sh") || !strings.Contains(markdown, "<<greeting>>") || strings.Contains(markdown, ":name hello") {
		t.Fatalf("portable Markdown should retain unexpanded source without fence headers: %q", markdown)
	}
	staticDir := filepath.Join(t.TempDir(), "site")
	if out, code := runIn(t, vault, "export-site", "--root", "100", "--frontend", fakeFrontend(t), "--out", staticDir); code != 0 {
		t.Fatalf("export-site: %v", out)
	}
	var published struct {
		Note struct {
			Body string `json:"body"`
		} `json:"note"`
	}
	if err := json.Unmarshal(readBundle(t, staticDir, "", site.PublishID(100), "note/"+site.PublishID(100)), &published); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"# Literate dotfiles", "```sh :name hello", "<<greeting>>", "defaultBranch = main", "editor = vi"} {
		if !strings.Contains(published.Note.Body, source) {
			t.Fatalf("published source missing %q: %q", source, published.Note.Body)
		}
	}
	unchanged, err := os.ReadFile(filepath.Join(vault, "note", "100.md"))
	if err != nil || string(unchanged) != string(body) {
		t.Fatalf("tangle/export changed the original note: %q (%v)", unchanged, err)
	}
}
