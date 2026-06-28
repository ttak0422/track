package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDirBundle(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Welcome\n\nstart with [[cli]] and the missing [[ghost]]\n")
	write("cli.md", "# CLI\n\nback to [[Welcome]]\n")
	write("guide.md", "# Guide\n\n![pic](assets/pic.png)\n")
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "assets", "pic.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	res, err := BuildDir(src, "index", fakeFrontend(t), out)
	if err != nil {
		t.Fatalf("BuildDir: %v", err)
	}
	if len(res.Notes) != 3 {
		t.Fatalf("expected 3 notes, got %v", res.Notes)
	}

	// Frontend + asset copied.
	if !fileExists(filepath.Join(out, "index.html")) {
		t.Fatalf("frontend index.html not copied")
	}
	if data, _ := os.ReadFile(filepath.Join(out, "assets", "pic.png")); string(data) != "PNG" {
		t.Fatalf("asset not copied: %q", data)
	}

	// site.json: index.md is the root.
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	if site.Title != "Welcome" {
		t.Fatalf("root title should be Welcome, got %q", site.Title)
	}

	// resolve.json: [[cli]] resolves by base name, [[Welcome]] by H1 title; [[ghost]] is unknown.
	resolve := readJSON[map[string]jsonRef](t, filepath.Join(out, "data", "resolve.json"))
	if _, ok := resolve["cli"]; !ok {
		t.Fatalf("cli should be resolvable by base name")
	}
	if _, ok := resolve["Welcome"]; !ok {
		t.Fatalf("Welcome should be resolvable by H1 title")
	}
	if _, ok := resolve["ghost"]; ok {
		t.Fatalf("unknown target should not be resolvable")
	}

	// graph: index links to cli; the missing [[ghost]] produces no edge.
	graph := readJSON[struct {
		Graph jsonGraph `json:"graph"`
	}](t, filepath.Join(out, "data", "graph.json"))
	if !hasSlugEdge(graph.Graph.Edges, site.Root, resolve["cli"].NoteID) {
		t.Fatalf("expected edge index->cli, got %+v", graph.Graph.Edges)
	}

	// Root note body keeps the wiki links for the frontend to resolve.
	rootNote := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	if !strings.Contains(rootNote.Note.Body, "[[cli]]") {
		t.Fatalf("root body should keep wiki link: %q", rootNote.Note.Body)
	}
}

func TestBuildDirRejectsMissingRoot(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildDir(src, "index", fakeFrontend(t), t.TempDir()); err == nil {
		t.Fatalf("expected error when root file is absent")
	}
}
