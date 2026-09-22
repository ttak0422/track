package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBabelTangleValidatesWholePlanBeforeWriting(t *testing.T) {
	for _, lastTarget := range []string{"../../outside.sh", "../alias/config.yml", "./scripts/out.sh", "scripts/alias.sh"} {
		t.Run(lastTarget, func(t *testing.T) {
			vault := t.TempDir()
			if out, code := runIn(t, vault, "new", "--title", "Tangle", "--id", "512"); code != 0 {
				t.Fatalf("create note: %v", out)
			}
			output := filepath.Join(vault, "note", "scripts", "out.sh")
			if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(output, filepath.Join(filepath.Dir(output), "alias.sh")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(vault, ".track"), filepath.Join(vault, "alias")); err != nil {
				t.Fatal(err)
			}
			body := "```sh :tangle scripts/out.sh\nreplace\n```\n" +
				"```sh :tangle new-dir/new.sh\ncreate\n```\n" +
				"```sh :tangle " + lastTarget + "\ninvalid\n```\n"
			if err := os.WriteFile(filepath.Join(vault, "note", "512.md"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, dryRun := range []bool{false, true} {
				args := []string{"babel", "tangle", "--id", "512"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				if out, code := runIn(t, vault, args...); code != 1 {
					t.Fatalf("invalid plan accepted: %v", out)
				}
				if got, err := os.ReadFile(output); err != nil || string(got) != "keep" {
					t.Fatalf("earlier output changed: %q, %v", got, err)
				}
				if _, err := os.Stat(filepath.Join(vault, "note", "new-dir")); !os.IsNotExist(err) {
					t.Fatalf("invalid plan created an output directory: %v", err)
				}
			}
		})
	}
}

func TestResolveDirChecksSymlinksAndDefaultDirectory(t *testing.T) {
	vault := t.TempDir()
	noteDir := filepath.Join(vault, "note")
	if err := os.Mkdir(noteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for name, destination := range map[string]string{"escape": outside, "inside": noteDir} {
		if err := os.Symlink(destination, filepath.Join(vault, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, arg := range []string{"", ".", "..", "../inside"} {
		if _, err := resolveDir(noteDir, vault, arg); err != nil {
			t.Fatalf("safe directory %q: %v", arg, err)
		}
	}
	if _, err := resolveDir(noteDir, vault, "../escape"); err == nil {
		t.Fatal("symlink outside vault must be refused")
	}
	if _, err := resolveDir(filepath.Join(vault, "escape"), vault, ""); err == nil {
		t.Fatal("default note directory outside vault must be refused")
	}
	t.Chdir(vault)
	if _, err := resolveDir("note", vault, ""); err != nil {
		t.Fatalf("relative note path should work: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "vault")
	if err := os.Symlink(vault, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDir(filepath.Join(alias, "note"), alias, ".."); err != nil {
		t.Fatalf("symlinked vault should work: %v", err)
	}
}
