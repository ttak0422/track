package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "vault")
	files := map[string][]byte{
		"note/100.md":                         []byte("# 本文\n[[Title]] ![](assets/image.png)\n"),
		"journal/20261101.md":                 []byte("journal\n"),
		"template/200.template.md":            []byte("<!-- track-template\nname: default\n-->\n{{ title }}\n"),
		".track/notes/100.yaml":               []byte("version: 1\ntitle: Title\ntags: [backup]\n"),
		".track/notes/20261101.yaml":          []byte("version: 1\ntitle: Journal\n"),
		".track/config.yml":                   []byte("date_format: '2006-01-02'\n"),
		"assets/image.png":                    {0, 1, 2, 255},
		"data/samples.jsonl":                  []byte("{\"value\":42}\n"),
		"calendar/events/one.json":            []byte("{\"revision\":1,\"deleted\":true}\n"),
		".track/gen/1/note/100.md":            []byte("older\n"),
		".track/gen/state.json":               []byte("{\"cursor\":1}\n"),
		".track/trash/old.md":                 []byte("deleted\n"),
		".track/sources/100/hash/record.json": []byte("{\"body\":\"evidence\"}\n"),
		".track/requests/request.json":        []byte("{\"status\":\"done\"}\n"),
		"apps/demo/index.html":                []byte("<!doctype html><title>app</title>"),
	}
	for name, data := range files {
		dest := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(source, "assets/empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	return source, files
}

func TestRoundTrip(t *testing.T) {
	source, files := fixture(t)
	stage := filepath.Join(t.TempDir(), "fixture.tar.gz")
	// Repository internals are excluded; their config may contain credentials.
	os.MkdirAll(filepath.Join(source, ".git"), 0o700)
	os.WriteFile(filepath.Join(source, ".git/config"), []byte("fixture-token"), 0o600)
	os.WriteFile(filepath.Join(source, ".DS_Store"), []byte("finder"), 0o600)
	manifest, err := Write([]Vault{{Name: "work", Prefix: "vault", Source: source}}, stage)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tar", "-tzf", stage).CombinedOutput(); err != nil {
		t.Fatalf("tar list: %v %s", err, out)
	}
	nativeExtract := t.TempDir()
	if out, err := exec.Command("tar", "-xzf", stage, "-C", nativeExtract).CombinedOutput(); err != nil {
		t.Fatalf("tar extract: %v %s", err, out)
	}
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(nativeExtract, "vault", name))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("native extraction differs: %s %v", name, err)
		}
	}
	if !manifest.Complete || manifest.Version != 1 || len(manifest.Vaults) != 1 {
		t.Fatalf("invalid manifest: %+v", manifest)
	}
	info, _ := os.Stat(stage)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	f, err := os.Open(stage)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	extract := t.TempDir()
	restored := map[string][]byte{}
	var decoded Manifest
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsLocal(hdr.Name) || strings.Contains(hdr.Name, "\\") {
			t.Fatalf("unsafe path %q", hdr.Name)
		}
		if hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeReg {
			t.Fatalf("unsafe entry: %+v", hdr)
		}
		dest := filepath.Join(extract, hdr.Name)
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "fixture-token") {
			t.Fatal("git config leaked")
		}
		if hdr.Name == "manifest.json" {
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			continue
		}
		os.MkdirAll(filepath.Dir(dest), 0o700)
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			t.Fatal(err)
		}
		restored[strings.TrimPrefix(hdr.Name, "vault/")] = data
	}
	if len(restored) != len(files) {
		t.Fatalf("restored %d, want %d", len(restored), len(files))
	}
	for name, want := range files {
		if !bytes.Equal(restored[name], want) {
			t.Errorf("bytes changed: %s", name)
		}
	}
	for _, entry := range decoded.Entries {
		if entry.Kind != "file" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(extract, entry.Path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if entry.SHA256 != hex.EncodeToString(sum[:]) || entry.Size != int64(len(data)) {
			t.Errorf("checksum mismatch %s", entry.Path)
		}
	}
	if _, err := os.Stat(filepath.Join(extract, "vault/assets/empty")); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesUnsafeOrIncompleteSources(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"Finder-name directory": func(t *testing.T, p string) {
			os.MkdirAll(filepath.Join(p, "assets/.DS_Store"), 0o700)
			os.WriteFile(filepath.Join(p, "assets/.DS_Store/keep.png"), []byte("essential bytes"), 0o600)
		},
		"Finder-name symlink":        func(t *testing.T, p string) { os.Symlink("image.png", filepath.Join(p, "assets/.DS_Store")) },
		"missing metadata directory": func(t *testing.T, p string) { os.RemoveAll(filepath.Join(p, ".track/notes")) },
		"legacy iCloud placeholder": func(t *testing.T, p string) {
			os.WriteFile(filepath.Join(p, "assets/.photo.png.icloud"), []byte("placeholder"), 0o600)
		},
		"unknown root":     func(t *testing.T, p string) { os.WriteFile(filepath.Join(p, ".env"), []byte("fixture-token"), 0o600) },
		"external symlink": func(t *testing.T, p string) { os.Symlink(t.TempDir(), filepath.Join(p, "assets/escape")) },
		"internal symlink": func(t *testing.T, p string) { os.Symlink("100.md", filepath.Join(p, "note/alias.md")) },
		"directory symlink": func(t *testing.T, p string) {
			os.RemoveAll(filepath.Join(p, ".track/notes"))
			os.Symlink(t.TempDir(), filepath.Join(p, ".track/notes"))
		},
		"fifo": func(t *testing.T, p string) {
			if err := syscall.Mkfifo(filepath.Join(p, "assets/pipe"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable file": func(t *testing.T, p string) {
			if os.Geteuid() == 0 {
				t.Skip("root bypasses permissions")
			}
			os.Chmod(filepath.Join(p, "note/100.md"), 0)
		},
		"unreadable directory": func(t *testing.T, p string) {
			if os.Geteuid() == 0 {
				t.Skip("root bypasses permissions")
			}
			d := filepath.Join(p, "assets")
			os.Chmod(d, 0)
			t.Cleanup(func() { os.Chmod(d, 0o700) })
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			source, _ := fixture(t)
			prepare(t, source)
			stage := filepath.Join(t.TempDir(), "bad.tar.gz")
			if _, err := Write([]Vault{{Prefix: "vault", Source: source}}, stage); err == nil {
				t.Fatal("unexpected success")
			}
			if _, err := os.Stat(stage); !os.IsNotExist(err) {
				t.Fatalf("partial left behind: %v", err)
			}
		})
	}
}

func TestDestinationAndPrefixValidation(t *testing.T) {
	source, _ := fixture(t)
	for _, prefix := range []string{"../escape", "/absolute", ".", "vault\\escape"} {
		if _, err := Write([]Vault{{Source: source, Prefix: prefix}}, filepath.Join(t.TempDir(), "bad.tar.gz")); err == nil {
			t.Errorf("accepted prefix %s", prefix)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{filepath.Join(source, "assets/inside.tar.gz"), filepath.Join(alias, "inside.tar.gz")} {
		if _, err := Write([]Vault{{Source: source, Prefix: "vault"}}, dest); err == nil {
			t.Error("accepted source destination")
		}
	}
	stage := filepath.Join(t.TempDir(), "old.tar.gz")
	os.WriteFile(stage, []byte("previous-complete"), 0o600)
	if _, err := Write([]Vault{{Source: source, Prefix: "vault"}}, stage); err == nil {
		t.Error("overwrote stage")
	}
	raw, _ := os.ReadFile(stage)
	if string(raw) != "previous-complete" {
		t.Fatal("existing file changed")
	}
}

func TestSameRejectsChanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "file")
	os.WriteFile(p, []byte("one"), 0o600)
	before, _ := os.Stat(p)
	// Same-length edits with a restored mtime must still be caught by ctime.
	time.Sleep(2 * time.Millisecond)
	os.WriteFile(p, []byte("two"), 0o600)
	os.Chtimes(p, time.Now(), before.ModTime())
	after, _ := os.Stat(p)
	if same(before, after) {
		t.Fatal("missed rewrite with restored mtime")
	}
	os.Remove(p)
	os.WriteFile(p, []byte("one"), 0o600)
	replacement, _ := os.Stat(p)
	if same(before, replacement) {
		t.Fatal("missed replacement")
	}
}

func TestCopyRejectsConcurrentChange(t *testing.T) {
	for _, kind := range []string{"edit", "delete", "replace", "grow", "shrink"} {
		t.Run(kind, func(t *testing.T) {
			source, _ := fixture(t)
			root, err := os.OpenRoot(source)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			before, _ := root.Lstat("note/100.md")
			p := filepath.Join(source, "note/100.md")
			switch kind {
			case "edit":
				os.WriteFile(p, []byte(strings.Repeat("x", int(before.Size()))), 0o600)
			case "delete":
				os.Remove(p)
			case "replace":
				os.Rename(p, p+".old")
				os.WriteFile(p, []byte("replacement"), 0o600)
			case "grow":
				os.WriteFile(p, []byte(strings.Repeat("x", int(before.Size())+1)), 0o600)
			case "shrink":
				os.WriteFile(p, []byte("x"), 0o600)
			}
			tw := tar.NewWriter(io.Discard)
			if _, err := copyFile(root, "note/100.md", before, tw, &tar.Header{Name: "vault/note/100.md", Size: before.Size(), Mode: 0o600}); err == nil {
				t.Fatal("missed mutation")
			}
		})
	}
}

type modifyingWriter struct {
	beforeWrite func()
	io.Writer
}

func (w *modifyingWriter) Write(p []byte) (int, error) {
	if w.beforeWrite != nil {
		action := w.beforeWrite
		w.beforeWrite = nil
		action()
	}
	return w.Writer.Write(p)
}

func TestCopyDetectsChangeWhileReading(t *testing.T) {
	source, _ := fixture(t)
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, _ := root.Lstat("note/100.md")
	changed := false
	writer := &modifyingWriter{Writer: io.Discard, beforeWrite: func() {
		changed = true
		if err := os.WriteFile(filepath.Join(source, "note/100.md"), []byte(strings.Repeat("x", int(before.Size()))), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	tw := tar.NewWriter(writer)
	if _, err := copyFile(root, "note/100.md", before, tw, &tar.Header{Name: "vault/note/100.md", Size: before.Size(), Mode: 0o600}); err == nil {
		t.Fatal("accepted file edited during copy")
	}
	if !changed {
		t.Fatal("mutation was not exercised")
	}
}

func TestProtectedMachineConfigRefusedByIdentity(t *testing.T) {
	source, _ := fixture(t)
	config := filepath.Join(t.TempDir(), "machine.yml")
	if err := os.WriteFile(config, []byte("synthetic-credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(source, ".track/machine.yml")
	if err := os.Link(config, alias); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "protected.tar.gz")
	if _, err := Write([]Vault{{Prefix: "vault", Source: source}}, stage, config); err == nil {
		t.Fatal("protected configuration was archived")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("protected partial was left behind")
	}
}

func TestRejectsEditAfterEarlierFileWasCopied(t *testing.T) {
	source, _ := fixture(t)
	// Incompressible synthetic data keeps the later assets copy in progress after
	// .track/notes has been written, exercising the final whole-vault comparison.
	data := make([]byte, 8<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets/large.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "changing.tar.gz")
	done := make(chan error, 1)
	go func() { _, err := Write([]Vault{{Prefix: "vault", Source: source}}, stage); done <- err }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("copy finished before mutation could be exercised: %v", err)
		case <-deadline.C:
			t.Fatal("backup did not make progress")
		case <-ticker.C:
			info, err := os.Stat(stage)
			if err != nil || info.Size() < 32<<10 {
				continue
			}
			if err := os.WriteFile(filepath.Join(source, ".track/notes/100.yaml"), []byte("version: 1\ntitle: Concurrent edit\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "vault changed during backup") {
					t.Fatalf("late edit was not detected: %v", err)
				}
				if _, err := os.Stat(stage); !os.IsNotExist(err) {
					t.Fatal("changed-vault partial was not removed")
				}
				return
			case <-deadline.C:
				t.Fatal("backup did not finish")
			}
		}
	}
}

func TestStageStaysInCapturedDestinationDirectory(t *testing.T) {
	source, _ := fixture(t)
	parent := t.TempDir()
	output := filepath.Join(parent, "output")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	moved := filepath.Join(parent, "moved-output")
	if err := os.Rename(output, moved); err != nil {
		t.Fatal(err)
	}
	redirect := t.TempDir()
	if err := os.Symlink(redirect, output); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteAt([]Vault{{Prefix: "vault", Source: source}}, root, "stage.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, "stage.tar.gz")); err != nil {
		t.Fatal("stage did not stay anchored", err)
	}
	if _, err := os.Stat(filepath.Join(redirect, "stage.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("stage followed changed ancestor")
	}
}

func TestIndependentConfigCopiesArePreservedAsVaultContent(t *testing.T) {
	source, _ := fixture(t)
	config := filepath.Join(t.TempDir(), "machine.yml")
	raw := []byte("agents:\n  token: synthetic-copy-test\n")
	if err := os.WriteFile(config, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".track/copied-machine.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "copy.tar.gz")
	if _, err := Write([]Vault{{Prefix: "vault", Source: source}}, stage, config); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(stage)
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
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			t.Fatal("independent vault file silently redacted")
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name != "vault/.track/copied-machine.yml" {
			continue
		}
		restored, err := io.ReadAll(tr)
		if err != nil || !bytes.Equal(restored, raw) {
			t.Fatal("independent vault file changed", err)
		}
		return
	}
}
