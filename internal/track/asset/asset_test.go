package asset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
}

func TestStoreWritesUnderAssets(t *testing.T) {
	cfg := testConfig(t)

	note, err := Store(cfg, "Cover Image.png", []byte("png-bytes"))
	if err != nil {
		t.Fatalf("store asset: %v", err)
	}
	// Spaces are replaced so the reference is valid inside ![](...).
	if note.Name != "Cover-Image.png" {
		t.Fatalf("unexpected sanitized name: %q", note.Name)
	}
	if note.Ref != "assets/Cover-Image.png" {
		t.Fatalf("unexpected ref: %q", note.Ref)
	}
	if note.Path != filepath.Join(cfg.AssetsDir(), "Cover-Image.png") {
		t.Fatalf("unexpected path: %q", note.Path)
	}
	if got, _ := os.ReadFile(note.Path); string(got) != "png-bytes" {
		t.Fatalf("asset content not written: %q", got)
	}
}

func TestStoreAvoidsOverwrite(t *testing.T) {
	cfg := testConfig(t)
	first, err := Store(cfg, "a.png", []byte("1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Store(cfg, "a.png", []byte("2"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "a.png" || second.Name != "a-1.png" {
		t.Fatalf("collision should suffix: %q then %q", first.Name, second.Name)
	}
	if got, _ := os.ReadFile(first.Path); string(got) != "1" {
		t.Fatalf("first asset must not be overwritten: %q", got)
	}
}

func TestStoreSanitizesAwkwardNames(t *testing.T) {
	cfg := testConfig(t)
	// A name with a path component and link-breaking characters reduces to a single safe filename.
	stored, err := Store(cfg, "../etc/we(i)rd name?.png", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "we-i-rd-name-.png" {
		t.Fatalf("unexpected sanitized name: %q", stored.Name)
	}
	if filepath.Dir(stored.Path) != cfg.AssetsDir() {
		t.Fatalf("asset escaped the assets dir: %q", stored.Path)
	}
}

func TestImportCopiesFile(t *testing.T) {
	cfg := testConfig(t)
	src := filepath.Join(t.TempDir(), "diagram.svg")
	if err := os.WriteFile(src, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err := Import(cfg, src)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stored.Ref != "assets/diagram.svg" {
		t.Fatalf("unexpected ref: %q", stored.Ref)
	}
	if got, _ := os.ReadFile(stored.Path); string(got) != "<svg/>" {
		t.Fatalf("imported content mismatch: %q", got)
	}
}
