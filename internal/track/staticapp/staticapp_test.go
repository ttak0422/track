package staticapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenFileStaysInsideAppRoot(t *testing.T) {
	vault := t.TempDir()
	appDir := filepath.Join(vault, DirName, "demo")
	if err := os.MkdirAll(filepath.Join(appDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "index.html"), []byte("<h1>demo</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "assets", "style.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(vault, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(appDir, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	root, err := Open(vault, "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := CheckIndex(root); err != nil {
		t.Fatalf("CheckIndex: %v", err)
	}
	f, _, err := OpenFile(root, "assets/style.css")
	if err != nil {
		t.Fatalf("OpenFile asset: %v", err)
	}
	f.Close()
	if _, _, err := OpenFile(root, "../secret.txt"); err == nil {
		t.Fatal("traversal path was accepted")
	}
	if _, _, err := OpenFile(root, "leak.txt"); err == nil {
		t.Fatal("symlink escaping the app root was accepted")
	}
}

func TestCopyPreservesBytesAndRejectsEscapingSymlinks(t *testing.T) {
	vault := t.TempDir()
	appDir := filepath.Join(vault, DirName, "demo")
	if err := os.MkdirAll(filepath.Join(appDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := []byte("<!doctype html>\x00\xff\n")
	js := []byte("const x = 'static';\n")
	if err := os.WriteFile(filepath.Join(appDir, "index.html"), index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "scripts", "app.js"), js, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := Open(vault, "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	out := filepath.Join(t.TempDir(), "demo")
	if err := Copy(root, out); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"index.html": index, "scripts/app.js": js} {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s bytes changed: got %q want %q", name, got, want)
		}
	}

	outside := filepath.Join(vault, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(appDir, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	if err := Copy(root, filepath.Join(t.TempDir(), "rejected")); err == nil {
		t.Fatal("copy accepted a symlink that escapes the app")
	}
}

func TestNameValidation(t *testing.T) {
	for _, name := range []string{"demo", "a-1", "x"} {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "../demo", "Demo", "a/b", "-demo", "demo.html"} {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true", name)
		}
	}
}

func TestOpenRejectsSymlinkedAppRoots(t *testing.T) {
	vault := t.TempDir()
	apps := filepath.Join(vault, DirName)
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("<h1>target</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(apps, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(vault, "alias"); err == nil {
		t.Fatal("symlinked app directory was accepted")
	}

	linkedVault := t.TempDir()
	if err := os.Symlink(apps, filepath.Join(linkedVault, DirName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(linkedVault, "alias"); err == nil {
		t.Fatal("symlinked apps directory was accepted")
	}
}
