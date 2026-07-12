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
	res, err := BuildDir(src, "", fakeFrontend(t), out)
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
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
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

func TestBuildDirRejectsMissingEntry(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildDir(src, "", fakeFrontend(t), t.TempDir()); err == nil {
		t.Fatalf("expected error when the entry page is absent")
	}
}

func TestBuildDirIconFromInlineField(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Home\n\nicon:: 📓\n\nsee [[cli]], [[plain]] and [[blank]]\n")
	write("cli.md", "# CLI\n\nicon:: 🛠\nicon:: 🔥\n") // more than one field: the first wins
	write("plain.md", "# Plain\n\nno fields here\n")
	write("blank.md", "# Blank\n\nicon:: \n") // empty value: no icon

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	icons := map[string]string{}
	list := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	for _, n := range list.Notes {
		icons[n.Title] = n.Icon
	}
	want := map[string]string{"Home": "📓", "CLI": "🛠", "Plain": "", "Blank": ""}
	for title, icon := range want {
		if icons[title] != icon {
			t.Errorf("%s icon = %q, want %q", title, icons[title], icon)
		}
	}

	// The icon:: field is an ordinary property and stays in the page's published props — but only
	// there: the published body drops the field line, so the page does not also print it as prose.
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	if len(root.Note.Props) != 1 || root.Note.Props[0].Key != "icon" || root.Note.Props[0].Value != "📓" {
		t.Fatalf("props = %+v", root.Note.Props)
	}
	if strings.Contains(root.Note.Body, "icon::") {
		t.Errorf("published body still carries the field line:\n%s", root.Note.Body)
	}
}

func TestBuildDirPublishesInlineFieldProps(t *testing.T) {
	src := t.TempDir()
	body := "# Fields\n\nstatus:: draft\n- rating:: 8\n"
	if err := os.WriteFile(filepath.Join(src, "index.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
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

// writeDir writes a directory-mode source tree: name -> content.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	src := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

// rootTitle builds the site and returns the title of its entry page.
func rootTitle(t *testing.T, src string) string {
	t.Helper()
	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	return site.Title
}

func TestBuildDirSiteConfigHome(t *testing.T) {
	files := map[string]string{
		"index.md": "# Index\n",
		"start.md": "# Start here\n",
	}

	// No site.yml: the "index" convention.
	if got := rootTitle(t, writeDir(t, files)); got != "Index" {
		t.Errorf("without site.yml, entry = %q, want Index", got)
	}

	// home by file base name, and by page title. It is the only way to name the entry page: there is no
	// flag to override it with.
	for _, home := range []string{"start", "Start here"} {
		files["site.yml"] = "home: " + home + "\n"
		if got := rootTitle(t, writeDir(t, files)); got != "Start here" {
			t.Errorf("home %q: entry = %q, want Start here", home, got)
		}
	}

	// A home naming no page fails loudly rather than publishing a different front door.
	src := writeDir(t, map[string]string{"index.md": "# Index\n", "site.yml": "home: ghost\n"})
	if _, err := BuildDir(src, "", fakeFrontend(t), t.TempDir()); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected a loud error naming the missing entry page, got %v", err)
	}
}

// A page whose H1 spells another page's file base name must not steal the front door: the entry page is
// resolved by file base name first, by title only when no file is named that.
func TestBuildDirEntryPrefersFileNameOverTitle(t *testing.T) {
	files := map[string]string{
		"index.md":    "# Track help\n", // the real landing page
		"synonyms.md": "# index\n",      // its title happens to spell the other page's file name
	}
	for _, c := range []struct{ name, config string }{
		{"convention", ""},
		{"site home", "home: index\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.config != "" {
				files["site.yml"] = c.config
			} else {
				delete(files, "site.yml")
			}
			if got := rootTitle(t, writeDir(t, files)); got != "Track help" {
				t.Errorf("entry = %q, want Track help (index.md)", got)
			}
		})
	}
}

// The config is found under either spelling; two of them is a loud error, not a coin flip.
func TestBuildDirSiteConfigFileName(t *testing.T) {
	pages := map[string]string{"index.md": "# Index\n", "start.md": "# Start here\n"}

	for _, name := range []string{"site.yml", "site.yaml"} {
		files := map[string]string{name: "home: start\n"}
		for k, v := range pages {
			files[k] = v
		}
		if got := rootTitle(t, writeDir(t, files)); got != "Start here" {
			t.Errorf("%s: entry = %q, want Start here", name, got)
		}
	}

	files := map[string]string{"site.yml": "home: start\n", "site.yaml": "home: index\n"}
	for k, v := range pages {
		files[k] = v
	}
	_, err := BuildDir(writeDir(t, files), "", fakeFrontend(t), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "site.yaml") || !strings.Contains(err.Error(), "site.yml") {
		t.Fatalf("two site configs should be a loud error naming both, got %v", err)
	}
}

func TestBuildDirSiteConfigRejectsBadFile(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []string
	}{
		{"unknown key", "home: index\nbase_url: https://example.com\n", []string{"site.yml", "base_url"}},
		{"unknown nested key", "icons:\n  colors:\n    idea: blue\n", []string{"site.yml", "colors"}},
		{"malformed yaml", "home: [index\n", []string{"site.yml"}},
		// A second document is never read by a single Decode: its keys would be dropped unchecked.
		{"second document", "home: index\n---\nhome: ghost\nbase_url: https://oops\n", []string{"site.yml", "document"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := writeDir(t, map[string]string{"index.md": "# Index\n", "site.yml": c.yaml})
			_, err := BuildDir(src, "", fakeFrontend(t), t.TempDir())
			if err == nil {
				t.Fatalf("expected an error, got none")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should name %q", err, want)
				}
			}
		})
	}
}

func TestBuildDirSiteConfigIcons(t *testing.T) {
	src := writeDir(t, map[string]string{
		"index.md": "# Index\n\ntags:: idea, book\n",    // first mapped tag wins
		"kind.md":  "# Kind\n\nno fields here\n",        // falls through to the kinds map
		"over.md":  "# Over\n\ntags:: idea\nicon:: 🔥\n", // the page's own icon beats both maps
		"other.md": "# Other\n\ntags:: unmapped\n",      // no mapping anywhere: no icon
		"site.yml": "icons:\n  tags:\n    idea: 💡\n    book: 📚\n  kinds:\n    note: 📄\n",
	})

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	list := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	icons := map[string]string{}
	tags := map[string][]string{}
	for _, n := range list.Notes {
		icons[n.Title] = n.Icon
		tags[n.Title] = n.Tags
	}

	// A page with no mapped tag still gets the kind mapping; "note" is the only kind a directory page has.
	want := map[string]string{"Index": "💡", "Kind": "📄", "Over": "🔥", "Other": "📄"}
	for title, icon := range want {
		if icons[title] != icon {
			t.Errorf("%s icon = %q, want %q", title, icons[title], icon)
		}
	}

	// "tags::" is lifted into the page's tags and published with it.
	if got := tags["Index"]; len(got) != 2 || got[0] != "idea" || got[1] != "book" {
		t.Errorf("Index tags = %v, want [idea book]", got)
	}
	if got := tags["Kind"]; len(got) != 0 {
		t.Errorf("Kind tags = %v, want none", got)
	}
}
