package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tangleFiles(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	out, code := capture(t, func() int { return Run(append([]string{"babel", "tangle"}, args...)) })
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("JSON: %q (%v)", out, err)
	}
	return result, code
}

func TestBabelFileTangleWithoutConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	// A config lookup would fail on this existing directory; neither machine nor vault is opened.
	t.Setenv("TRACK_CONFIG", dir)
	t.Setenv("TRACK_VAULT", filepath.Join(dir, "no-vault"))
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(dir, "no-cache"))
	t.Setenv("TMPDIR", dir)
	source := filepath.Join(dir, "source.md")
	body := "```sh :name fragment :eval no\nliteral $HOME\n```\n" +
		"```sh :tangle generated/out :eval no :noweb tangle\n<<fragment>>\n```\n" +
		"```sh :tangle generated/./out :eval no\nlast $HOME\n```\n"
	if err := os.WriteFile(source, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"--file", "--path"} {
		for _, dry := range []bool{true, false} {
			args := []string{selector, source}
			if dry {
				args = append(args, "--dry-run")
			}
			out, code := tangleFiles(t, args...)
			if code != 0 || out["temporary"] != true || out["dry_run"] != dry {
				t.Fatalf("tangle: %v", out)
			}
			root := out["output_dir"].(string)
			t.Cleanup(func() { os.RemoveAll(root) })
			targets := out["targets"].([]any)
			if len(targets) != 1 {
				t.Fatalf("targets: %v", targets)
			}
			target := targets[0].(map[string]any)
			if target["blocks"] != float64(2) || len(target["overridden"].([]any)) != 1 || target["source"].(map[string]any)["line"] != float64(7) {
				t.Fatalf("provenance: %v", target)
			}
			if dry {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatalf("dry-run leaked directory: %v", err)
				}
			} else {
				content, err := os.ReadFile(filepath.Join(root, "generated/out"))
				if err != nil || string(content) != "last $HOME\n" {
					t.Fatalf("content: %q, %v", content, err)
				}
			}
		}
	}
	for _, path := range []string{"no-vault", "no-cache"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
			t.Fatalf("created %s: %v", path, err)
		}
	}
	if got, _ := os.ReadFile(source); string(got) != body {
		t.Fatal("input changed")
	}
}

func TestBabelFileTangleMultiSourcePreflight(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "out")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "same")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("same", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	if err := os.WriteFile(a, []byte("```text :tangle same\nfirst\n```\n```text :tangle created/new\nnew\n```"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"same", "./same", "sub/../same", "alias"} {
		if err := os.WriteFile(b, []byte("```text :tangle "+target+"\nlast\n```"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, sources := range [][]string{{a, b}, {b, a}} {
			for _, dry := range []bool{true, false} {
				args := []string{"--file", sources[0], "--file", sources[1], "--out-dir", root}
				if dry {
					args = append(args, "--dry-run")
				}
				out, code := tangleFiles(t, args...)
				if code != 1 || !strings.Contains(out["error"].(string), "duplicate tangle target") || !strings.Contains(out["error"].(string), "a.md:1") || !strings.Contains(out["error"].(string), "b.md:1") {
					t.Fatalf("conflict: %v", out)
				}
				if got, _ := os.ReadFile(existing); string(got) != "keep" {
					t.Fatal("earlier output overwritten")
				}
				if _, err := os.Stat(filepath.Join(root, "created")); !os.IsNotExist(err) {
					t.Fatalf("created output on conflict: %v", err)
				}
			}
		}
	}
	// A source listed through a symlink is the same document, not a cross-document collision.
	alias := filepath.Join(dir, "a-alias.md")
	if err := os.Symlink(a, alias); err != nil {
		t.Fatal(err)
	}
	if out, code := tangleFiles(t, "--file", a, "--file", alias, "--out-dir", root); code != 0 {
		t.Fatalf("same source alias: %v", out)
	}
	// Distinct documents with distinct targets compose one plan.
	if err := os.WriteFile(b, []byte("```text :tangle other\nsecond\n```"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := tangleFiles(t, "--file", a, "--file", b, "--out-dir", root); code != 0 || len(out["targets"].([]any)) != 3 {
		t.Fatalf("multi-file: %v", out)
	}
}

func TestBabelFileTangleProtectsInputsAndCleansFailures(t *testing.T) {
	for _, target := range []string{"source.md", "input-alias", "input-hardlink", "../escape", "escape-link/out", "parent", ".track/config.yml"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			source := filepath.Join(dir, "source.md")
			body := "```text :tangle created/new\nnew\n```\n```text :tangle " + target + "\nreplace\n```"
			if target == "parent" {
				body += "\n```text :tangle parent/child\nx\n```"
			}
			if err := os.WriteFile(source, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(source, filepath.Join(dir, "input-alias")); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(source, filepath.Join(dir, "input-hardlink")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "escape-link")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(dir, ".track"), 0o755); err != nil {
				t.Fatal(err)
			}
			out, code := tangleFiles(t, "--file", source, "--out-dir", dir)
			if code != 1 {
				t.Fatalf("unsafe plan accepted: %v", out)
			}
			if got, _ := os.ReadFile(source); string(got) != body {
				t.Fatal("input changed")
			}
			if _, err := os.Stat(filepath.Join(dir, "created")); !os.IsNotExist(err) {
				t.Fatalf("created partial output: %v", err)
			}
		})
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	source := filepath.Join(dir, "invalid.md")
	if err := os.WriteFile(source, []byte("```sh :tangle yes\nx\n```"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := tangleFiles(t, "--file", source); code != 1 {
		t.Fatalf("invalid: %v", out)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "track-tangle-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary output leaked: %v %v", leftovers, err)
	}
}
