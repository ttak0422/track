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

	// Frontend + asset copied. The asset is published under its opaque slug name, not "pic.png".
	if !fileExists(filepath.Join(out, "index.html")) {
		t.Fatalf("frontend index.html not copied")
	}
	if fileExists(filepath.Join(out, "assets", "pic.png")) {
		t.Fatalf("asset should not be published under its source name")
	}
	assetName := publishAssetName("pic.png")
	if data, _ := os.ReadFile(filepath.Join(out, "assets", assetName)); string(data) != "PNG" {
		t.Fatalf("asset not copied to %q: %q", assetName, data)
	}
	if !strings.HasSuffix(assetName, ".png") {
		t.Fatalf("asset slug should keep its extension, got %q", assetName)
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

	// The guide's body references the asset by its slug name, never the source "pic.png".
	guide := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", resolve["guide"].NoteID+".json"))
	if !strings.Contains(guide.Note.Body, "assets/"+assetName) || strings.Contains(guide.Note.Body, "assets/pic.png") {
		t.Fatalf("guide body should reference the slugged asset: %q", guide.Note.Body)
	}
}

func TestBuildDirRendersSpecAssetToSVG(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Home\n\n![chart](assets/c.viewspec.json)\n")
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `{"version":2,"mark":"bar","title":"Demo","data":{"kind":"metric","records":[
		{"name":"A","time":"t1","value":3},{"name":"B","time":"t1","value":7}]},"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}`
	write(filepath.Join("assets", "c.viewspec.json"), spec)

	out := t.TempDir()
	if _, err := BuildDir(src, "index", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	// The spec asset is published as an SVG image (not the raw JSON).
	assetName := publishAssetName("c.viewspec.json")
	if !strings.HasSuffix(assetName, ".svg") {
		t.Fatalf("spec asset should publish as .svg, got %q", assetName)
	}
	data, err := os.ReadFile(filepath.Join(out, "assets", assetName))
	if err != nil {
		t.Fatalf("rendered SVG not written: %v", err)
	}
	if !strings.HasPrefix(string(data), "<?xml") || !strings.Contains(string(data), ">Demo<") {
		t.Fatalf("asset is not the rendered SVG: %.60s", data)
	}

	// The body references the rendered .svg, never the source .viewspec.json.
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	home := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	if !strings.Contains(home.Note.Body, "assets/"+assetName) || strings.Contains(home.Note.Body, "viewspec.json") {
		t.Fatalf("body should reference the rendered svg: %q", home.Note.Body)
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
