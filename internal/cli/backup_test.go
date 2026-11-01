package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/backup"
)

func TestBackupCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "track")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/track")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	active := filepath.Join(dir, "active")
	other := filepath.Join(dir, "other")
	for _, vault := range []string{active, other} {
		for _, name := range []string{"note", "journal", "template", "assets", "data", ".track/notes"} {
			if err := os.MkdirAll(filepath.Join(vault, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		os.WriteFile(filepath.Join(vault, "note/100.md"), []byte(filepath.Base(vault)), 0o600)
		os.WriteFile(filepath.Join(vault, ".track/notes/100.yaml"), []byte("version: 1\ntitle: Fixture\n"), 0o600)
	}
	configPath := filepath.Join(dir, "machine.yml")
	// A real secret is never used. The old agents field must not leak into archives.
	os.WriteFile(configPath, []byte("default_vault: work\nvaults:\n  work: "+active+"\n  active: "+other+"\nagents:\n  token: synthetic-not-for-archive\n"), 0o600)
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", active)
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(dir, "cache"))
	invoke := func(wantOK bool, args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(binary, args...)
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("%v: err=%v out=%s", args, err, out)
		}
		var result map[string]any
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatalf("invalid output: %s", out)
		}
		return result
	}
	archive := filepath.Join(dir, "backup.tar.gz")
	result := invoke(true, "backup", "--out", archive)
	if result["complete"] != true || result["vaults"] != float64(1) {
		t.Fatal(result)
	}
	manifest, contents := readBackupTest(t, archive)
	if len(manifest.Vaults) != 1 || manifest.Vaults[0].Name != "work" || contents["vault/note/100.md"] != "active" {
		t.Fatalf("bad default selection: %+v", manifest)
	}
	for name, data := range contents {
		if strings.Contains(data, "synthetic-not-for-archive") {
			t.Fatalf("machine credential leaked in %s", name)
		}
	}
	before, _ := os.ReadFile(archive)
	invoke(false, "backup", "--out", archive)
	after, _ := os.ReadFile(archive)
	if string(before) != string(after) {
		t.Fatal("existing backup overwritten without flag")
	}
	invoke(true, "backup", "--out", archive, "--overwrite")
	all := filepath.Join(dir, "all.tar.gz")
	invoke(true, "backup", "--all-vaults", "--out", all)
	manifest, contents = readBackupTest(t, all)
	if len(manifest.Vaults) != 2 {
		t.Fatalf("all vault count: %+v", manifest)
	}
	if contents["vaults/0001/note/100.md"] != "other" || contents["vaults/0002/note/100.md"] != "active" {
		t.Fatalf("colliding note paths: %+v", contents)
	}
	selected := filepath.Join(dir, "selected.tar.gz")
	invoke(true, "--vault", "active", "backup", "--out", selected)
	_, contents = readBackupTest(t, selected)
	if contents["vault/note/100.md"] != "other" {
		t.Fatal("global vault selection failed")
	}
	invoke(false, "backup", "--vault", "missing", "--out", filepath.Join(dir, "missing.tar.gz"))
	invoke(false, "backup", "--vault", "work", "--all-vaults", "--out", all)
	invoke(false, "backup", "--out", filepath.Join(active, "assets/inside.tar.gz"))
	alias := filepath.Join(dir, "alias")
	os.Symlink(active, alias)
	invoke(false, "backup", "--out", filepath.Join(alias, "inside.tar.gz"))
	link := filepath.Join(dir, "link.tar.gz")
	os.Symlink(archive, link)
	invoke(false, "backup", "--out", link, "--overwrite")
	invoke(false, "backup", "--timeout", "0", "--out", archive, "--overwrite")
	// Failure after an existing complete archive must not publish a partial one.
	before, _ = os.ReadFile(archive)
	os.Chmod(filepath.Join(active, "note/100.md"), 0)
	invoke(false, "backup", "--out", archive, "--overwrite")
	os.Chmod(filepath.Join(active, "note/100.md"), 0o600)
	after, _ = os.ReadFile(archive)
	if string(before) != string(after) {
		t.Fatal("read error replaced previous archive")
	}
	for _, change := range []string{"directory", "symlink alias"} {
		t.Run("destination "+change+" changed during copy", func(t *testing.T) {
			data := make([]byte, 8<<20)
			if _, err := rand.Read(data); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(active, "assets/large.bin"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			outputDir := filepath.Join(dir, "moving-output-"+strings.ReplaceAll(change, " ", "-"))
			storageDir := outputDir
			if change == "symlink alias" {
				storageDir = outputDir + "-storage"
			}
			os.Mkdir(storageDir, 0o700)
			if change == "symlink alias" {
				if err := os.Symlink(storageDir, outputDir); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(outputDir, "previous.tar.gz")
			os.WriteFile(target, []byte("previous-complete"), 0o600)
			cmd := exec.Command(binary, "backup", "--overwrite", "--out", target)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			moved := storageDir
			if change == "directory" {
				moved = filepath.Join(dir, "moved-output")
			}
			for {
				select {
				case err := <-done:
					t.Fatalf("backup finished before swap was exercised: %v %s", err, output.String())
				case <-timer.C:
					cmd.Process.Kill()
					t.Fatal("copy did not make progress")
				case <-ticker.C:
					stages, _ := filepath.Glob(filepath.Join(outputDir, ".track-backup-*.partial"))
					if len(stages) == 0 {
						continue
					}
					info, err := os.Stat(stages[0])
					if err != nil || info.Size() < 32<<10 {
						continue
					}
					if change == "directory" {
						if err := os.Rename(outputDir, moved); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Remove(outputDir); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Symlink(filepath.Join(active, "assets"), outputDir); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-done:
						if err == nil || !strings.Contains(output.String(), "destination directory changed") {
							t.Fatalf("changed destination accepted: %v %s", err, output.String())
						}
						raw, err := os.ReadFile(filepath.Join(moved, "previous.tar.gz"))
						if err != nil || string(raw) != "previous-complete" {
							t.Fatal("old destination changed", err)
						}
						leftovers, _ := filepath.Glob(filepath.Join(moved, ".track-backup-*.partial"))
						if len(leftovers) != 0 {
							t.Fatal("stage cleanup followed swapped ancestor", leftovers)
						}
						newStages, _ := filepath.Glob(filepath.Join(active, "assets/.track-backup-*"))
						if len(newStages) != 0 {
							t.Fatal("archive written into source", newStages)
						}
						return
					case <-timer.C:
						cmd.Process.Kill()
						t.Fatal("copy did not finish")
					}
				}
			}
		})
	}
	// A FIFO machine config simulates a read that never completes. The parent
	// deadline includes configuration resolution, before any source hydration.
	fifo := filepath.Join(dir, "config-fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", fifo)
	start := time.Now()
	result = invoke(false, "backup", "--timeout", "100ms", "--overwrite", "--out", archive)
	if time.Since(start) > 3*time.Second || !strings.Contains(result["error"].(string), "timed out") {
		t.Fatal(result)
	}
	after, _ = os.ReadFile(archive)
	if string(before) != string(after) {
		t.Fatal("timeout replaced previous archive")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".track-backup-*.partial"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache")); !os.IsNotExist(err) {
		t.Fatalf("backup created an index cache: %v", err)
	}
	// Syntax errors in old secret-bearing fields must not print their values.
	t.Setenv("TRACK_CONFIG", configPath)
	os.WriteFile(configPath, []byte("vault_dir: [synthetic-secret-syntax-error]\n"), 0o600)
	result = invoke(false, "backup", "--out", archive, "--overwrite")
	if strings.Contains(result["error"].(string), "synthetic-secret") {
		t.Fatal("config value leaked")
	}
}

func readBackupTest(t *testing.T, filename string) (backup.Manifest, map[string]string) {
	t.Helper()
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var manifest backup.Manifest
	contents := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "manifest.json" {
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
		} else {
			contents[h.Name] = string(data)
		}
	}
	return manifest, contents
}

func TestBackupAtomicNoClobber(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, pair := range []struct{ name, body string }{{"stage", "new complete"}, {"destination.tar.gz", "concurrently created"}} {
		if err := os.WriteFile(filepath.Join(dir, pair.name), []byte(pair.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := commitBackupExclusive(root, "stage", "destination.tar.gz"); err == nil {
		t.Fatal("no-replace commit clobbered an existing file")
	}
	data, err := os.ReadFile(filepath.Join(dir, "destination.tar.gz"))
	if err != nil || string(data) != "concurrently created" {
		t.Fatalf("destination changed: %s %v", data, err)
	}
	if err := commitBackupExclusive(root, "stage", "fresh.tar.gz"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "fresh.tar.gz"))
	if err != nil || string(data) != "new complete" {
		t.Fatalf("commit failed: %s %v", data, err)
	}
}

func TestBackupDirectorySyncFailureReportsCommittedOutcome(t *testing.T) {
	output, code := capture(t, func() int { return emitBackupCommitError("fixture.tar.gz", fmt.Errorf("synthetic sync failure")) })
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if code != 1 || result["commit_state"] != "committed" || result["durability"] != "uncertain" || result["complete"] == true {
		t.Fatalf("misleading post-commit result: %s", output)
	}
}
