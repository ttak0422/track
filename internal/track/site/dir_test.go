package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDirRendersMarkdownDir(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Welcome\n\nstart with [[cli]] and the missing [[ghost]]\n")
	write("cli.md", "# CLI\n\nback to [[Welcome]]\n")
	// An asset referenced from a page.
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("guide.md", "# Guide\n\n![pic](assets/pic.png)\n")
	if err := os.WriteFile(filepath.Join(src, "assets", "pic.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	res, err := BuildDir(src, "index", out)
	if err != nil {
		t.Fatalf("BuildDir: %v", err)
	}
	// Root file is published as index.html and is listed first.
	if len(res.Pages) == 0 || res.Pages[0] != "index.html" {
		t.Fatalf("root should publish as index.html first, got %v", res.Pages)
	}

	index := readFile(t, filepath.Join(out, "index.html"))
	// [[cli]] resolves to its page by base name; the H1 title link [[Welcome]] also works (see below).
	if !strings.Contains(index, `href="cli.html"`) {
		t.Fatalf("index should link to cli page:\n%s", index)
	}
	// An unknown target stays inert text.
	if strings.Contains(index, "ghost.html") {
		t.Fatalf("unknown wiki target must not be linked:\n%s", index)
	}
	if !strings.Contains(index, "ghost") {
		t.Fatalf("unknown wiki target should remain as text:\n%s", index)
	}

	// Title-based resolution: cli.md links back via [[Welcome]] (index.md's H1) -> index.html.
	cli := readFile(t, filepath.Join(out, "cli.html"))
	if !strings.Contains(cli, `href="index.html"`) {
		t.Fatalf("cli page should resolve [[Welcome]] to index.html:\n%s", cli)
	}

	if got := readFile(t, filepath.Join(out, "assets", "pic.png")); got != "PNG" {
		t.Fatalf("asset not copied: %q", got)
	}
}

func TestBuildDirRejectsMissingRoot(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildDir(src, "index", t.TempDir()); err == nil {
		t.Fatalf("expected error when root file is absent")
	}
}
