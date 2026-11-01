package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
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
