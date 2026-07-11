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
	res, err := BuildDir(src, "index", "", fakeFrontend(t), out)
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

func TestBuildDirResolvesSpecAssetToEChartsOption(t *testing.T) {
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
	if _, err := BuildDir(src, "index", "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	// The spec asset is published as a resolved ECharts option (not the raw spec JSON).
	assetName := publishAssetName("c.viewspec.json")
	if !strings.HasSuffix(assetName, ".echarts.json") {
		t.Fatalf("spec asset should publish as .echarts.json, got %q", assetName)
	}
	data, err := os.ReadFile(filepath.Join(out, "assets", assetName))
	if err != nil {
		t.Fatalf("resolved option not written: %v", err)
	}
	if !strings.Contains(string(data), `"type":"bar"`) || !strings.Contains(string(data), `"text":"Demo"`) {
		t.Fatalf("asset is not the resolved ECharts option: %.80s", data)
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
	if _, err := BuildDir(src, "index", "", fakeFrontend(t), t.TempDir()); err == nil {
		t.Fatalf("expected error when root file is absent")
	}
}

func TestBuildDirTagsAndQueryBlocks(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Home\n\n```track-query\nTABLE title, tags FROM #docs SORT title\n```\n\ntags:: docs\n")
	write("a.md", "# Alpha\n\ntags:: docs/guide\n")
	write("b.md", "# Beta\n\nno tags here\n")

	out := t.TempDir()
	if _, err := BuildDir(src, "index", "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	// The tags:: inline field becomes the page's tags in notes.json.
	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	tagsByTitle := map[string][]string{}
	for _, n := range notes.Notes {
		tagsByTitle[n.Title] = n.Tags
	}
	if len(tagsByTitle["Alpha"]) != 1 || tagsByTitle["Alpha"][0] != "docs/guide" {
		t.Fatalf("Alpha tags = %v", tagsByTitle["Alpha"])
	}

	// The track-query fence is expanded to a Markdown result table at build time; #docs matches the
	// nested docs/guide tag too.
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	if strings.Contains(root.Note.Body, "```track-query") {
		t.Fatalf("query fence should be expanded: %q", root.Note.Body)
	}
	if !strings.Contains(root.Note.Body, "| [[Alpha]] | docs/guide |") ||
		!strings.Contains(root.Note.Body, "| [[Home]] | docs |") {
		t.Fatalf("expanded table missing rows: %q", root.Note.Body)
	}
	if strings.Contains(root.Note.Body, "[[Beta]]") {
		t.Fatalf("untagged note must not match #docs: %q", root.Note.Body)
	}

	// Every used tag and its ancestors get a real page file.
	for _, rel := range []string{"tags/docs/index.html", "tags/docs/guide/index.html"} {
		if !fileExists(filepath.Join(out, rel)) {
			t.Fatalf("missing tag page %s", rel)
		}
	}
}

func TestBuildDirPublishesInlineFieldProps(t *testing.T) {
	src := t.TempDir()
	body := "# Fields\n\nstatus:: draft\n- rating:: 8\n"
	if err := os.WriteFile(filepath.Join(src, "index.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if _, err := BuildDir(src, "index", "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	props := root.Note.Props
	if len(props) != 2 || props[0].Key != "status" || props[0].Value != "draft" ||
		props[1].Key != "rating" || props[1].Type != "number" {
		t.Fatalf("props = %+v", props)
	}
}
