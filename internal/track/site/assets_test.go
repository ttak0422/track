package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyAssetsRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "vault", "assets-src")
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "vault", "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(srcDir, "ok.txt")
	if err := os.WriteFile(inside, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	rels := []string{"ok.txt", "../secret.txt", secret, ""}
	copied, missing, err := copyAssets(srcDir, outDir, rels, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 || copied[0] != "ok.txt" {
		t.Fatalf("copied = %v, want only ok.txt", copied)
	}
	if len(missing) != 3 {
		t.Fatalf("missing = %v, want the three rejected rels", missing)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("published %d assets, want 1", len(entries))
	}
}
