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
	write("cli.md", "# CLI\n\nback to [[Welcome]]\n\n- [ ] open item [#A]\n- [x] shipped item\n")
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

	// Task lines are published with the default state set, so the static board can render read-only.
	cliNote := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", resolve["cli"].NoteID+".json"))
	if cliNote.Note.Tasks == nil || len(cliNote.Note.Tasks.Items) != 2 {
		t.Fatalf("cli note should publish its tasks: %+v", cliNote.Note.Tasks)
	}
	if cliNote.Note.Tasks.Items[0].Priority != "A" || !cliNote.Note.Tasks.Items[1].Done {
		t.Fatalf("unexpected published tasks: %+v", cliNote.Note.Tasks.Items)
	}
	if rootNote.Note.Tasks != nil {
		t.Fatalf("taskless note should omit tasks, got %+v", rootNote.Note.Tasks)
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

func TestBuildDirTagsAndQueryBlocks(t *testing.T) {
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Home\n\n```track-query\nTABLE title, tags FROM #docs SORT title\n```\n")
	write("a.md", "# Alpha\n")
	write("b.md", "# Beta\n\nno tags here\n")
	write("site.yml", "pages:\n  index:\n    tags: [docs]\n  a:\n    tags: [docs/guide]\n")

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	// The site.yml pages entry becomes the page's tags in notes.json.
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

// A whole-line inline field is data that belongs in the prose (ADR 0032): it is indexed as a property
// *and* rendered as the line it is. Note-level metadata is not written in a body at all.
func TestBuildDirPublishesInlineFieldProps(t *testing.T) {
	src := writeDir(t, map[string]string{"index.md": "# Fields\n\nweight:: 68.2\n- rating:: 8\n"})

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	props := root.Note.Props
	if len(props) != 2 || props[0].Key != "weight" || props[0].Type != "number" ||
		props[1].Key != "rating" || props[1].Type != "number" {
		t.Fatalf("props = %+v", props)
	}
	if !strings.Contains(root.Note.Body, "weight:: 68.2") {
		t.Errorf("the field line is prose and must still render:\n%s", root.Note.Body)
	}
}

// A pages entry's props are the sidecar's props map: published before the body's inline fields, in
// the order a vault note's properties assemble (note.CollectProps), and typed like sidecar values —
// a quoted date classifies as a date.
func TestBuildDirSiteConfigProps(t *testing.T) {
	src := writeDir(t, map[string]string{
		"index.md": "# Fields\n\nweight:: 68.2\n",
		"site.yml": "pages:\n  index:\n    props: {section: guide, reviewed: \"2026-07-02\"}\n",
	})

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", site.Root+".json"))
	props := root.Note.Props
	if len(props) != 3 || props[0].Key != "reviewed" || props[0].Type != "date" ||
		props[1].Key != "section" || props[1].Value != "guide" || props[2].Key != "weight" {
		t.Fatalf("props = %+v", props)
	}
}

// writeDir writes a directory-mode source tree: name (possibly under a subdirectory) -> content.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	src := t.TempDir()
	for name, body := range files {
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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

// "home: index.md" is the same front door as "home: index" — but only a ".md" extension is forgiven.
// filepath.Ext strips any dot suffix, so a page whose title carries one ("v1.2") must resolve to that
// page and never to the file base name the strip would leave behind ("v1").
func TestBuildDirEntryExtension(t *testing.T) {
	pages := map[string]string{
		"index.md":     "# Index\n",
		"v1.md":        "# Version One\n",
		"changelog.md": "# v1.2\n", // its title is what a dot-suffix strip would mangle
	}
	for _, c := range []struct{ home, want string }{
		{"changelog.md", "v1.2"}, // a file base name, spelled with its extension
		{"v1.2", "v1.2"},         // a page title with a dot: not "v1" with an extension
		{"Version One", "Version One"},
	} {
		files := map[string]string{"site.yml": "home: " + c.home + "\n"}
		for k, v := range pages {
			files[k] = v
		}
		if got := rootTitle(t, writeDir(t, files)); got != c.want {
			t.Errorf("home %q: entry = %q, want %q", c.home, got, c.want)
		}
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

// A page's icon comes from the site config's pages entry, keyed by file base name — the per-page override
// slot of config.NoteIcon, the one resolver. A page whose entry carries only tags takes the icons.tags
// mapping, and a page with neither falls through to the kinds map: override → tags → kinds, the same
// precedence a vault note resolves in.
func TestBuildDirSiteConfigIcons(t *testing.T) {
	src := writeDir(t, map[string]string{
		"index.md":  "# Index\n",
		"over.md":   "# Over\n",
		"tagged.md": "# Tagged\n", // no icon of its own: the icons.tags mapping supplies it
		"kind.md":   "# Kind\n",   // no pages entry at all: falls through to the kinds map
		"site.yml": "pages:\n  index:\n    icon: 💡\n  over:\n    icon: 🔥\n  tagged:\n    tags: [guide]\n" +
			"icons:\n  tags:\n    guide: 📚\n  kinds:\n    note: 📄\n",
	})

	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}

	list := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	icons := map[string]string{}
	for _, n := range list.Notes {
		icons[n.Title] = n.Icon
	}

	// "note" is the only kind a directory page has, so the kinds map is what every unlisted page gets.
	want := map[string]string{"Index": "💡", "Over": "🔥", "Tagged": "📚", "Kind": "📄"}
	for title, icon := range want {
		if icons[title] != icon {
			t.Errorf("%s icon = %q, want %q", title, icons[title], icon)
		}
	}
}

// A pages entry naming no page is a typo — a page renamed, or its name misspelled — and the page it
// meant to describe would otherwise publish with the wrong icon and no tags, silently. It is a build
// error naming both the entry and the file it looked for.
func TestBuildDirSiteConfigRejectsOrphanPage(t *testing.T) {
	src := writeDir(t, map[string]string{
		"index.md": "# Index\n",
		"site.yml": "pages:\n  index:\n    icon: 🧭\n  ghost:\n    icon: 👻\n",
	})
	_, err := BuildDir(src, "", fakeFrontend(t), t.TempDir())
	if err == nil {
		t.Fatalf("a pages entry with no page must fail the build")
	}
	for _, want := range []string{"pages", "ghost", "ghost.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

// An up that resolves to no page in the set is the orphan pages entry's sibling: the page it places
// would publish with no breadcrumb, silently. So is a page named as its own parent — a trail that
// could never appear.
func TestBuildDirSiteConfigRejectsBadUp(t *testing.T) {
	for _, tc := range []struct{ yml, want string }{
		{"pages:\n  index: {up: ghost}\n", "ghost"},
		{"pages:\n  index: {up: index}\n", "the page itself"},
		{"pages:\n  index: {up: Index}\n", "the page itself"}, // by page title, not just base name
	} {
		src := writeDir(t, map[string]string{"index.md": "# Index\n", "site.yml": tc.yml})
		_, err := BuildDir(src, "", fakeFrontend(t), t.TempDir())
		if err == nil {
			t.Fatalf("%q: a bad up must fail the build", tc.yml)
		}
		for _, want := range []string{"index", "up", tc.want} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should name %q", err, want)
			}
		}
	}
}

// A pages entry that says nothing — no icon, no tags, no up, an entry YAML decodes to its zero value —
// is the orphan entry's sibling: it does nothing, and an entry that does nothing is never what its
// author meant.
func TestBuildDirSiteConfigRejectsEmptyPage(t *testing.T) {
	for _, yml := range []string{
		"pages:\n  index: {}\n",
		"pages:\n  index:\n", // YAML null
	} {
		src := writeDir(t, map[string]string{"index.md": "# Index\n", "site.yml": yml})
		_, err := BuildDir(src, "", fakeFrontend(t), t.TempDir())
		if err == nil {
			t.Fatalf("%q: a pages entry that says nothing must fail the build", yml)
		}
		for _, want := range []string{"pages", "index"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should name %q", err, want)
			}
		}
	}
}

// A page's cover image comes from its pages entry, spelled like the sidecar's field: an
// "assets/<file>" reference. It publishes with the page (feeding gallery layouts and og:image); a
// value in any other form, or one naming no asset, is the orphan entry's sibling — a cover that
// would silently never appear.
func TestBuildDirSiteConfigImage(t *testing.T) {
	src := writeDir(t, map[string]string{
		"index.md":         "# Index\n",
		"assets/cover.png": "png",
		"site.yml":         "pages:\n  index: {image: assets/cover.png}\n",
	})
	out := t.TempDir()
	if _, err := BuildDir(src, "", fakeFrontend(t), out); err != nil {
		t.Fatalf("BuildDir: %v", err)
	}
	list := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	if len(list.Notes) != 1 || list.Notes[0].Image == "" {
		t.Fatalf("the page should publish its site-config cover image, got %+v", list.Notes)
	}

	for _, tc := range []struct{ yml, want string }{
		{"pages:\n  index: {image: cover.png}\n", "assets/<file>"},
		{"pages:\n  index: {image: assets/ghost.png}\n", "ghost.png"},
	} {
		src := writeDir(t, map[string]string{
			"index.md":         "# Index\n",
			"assets/cover.png": "png",
			"site.yml":         tc.yml,
		})
		_, err := BuildDir(src, "", fakeFrontend(t), t.TempDir())
		if err == nil {
			t.Fatalf("%q: a bad image must fail the build", tc.yml)
		}
		for _, want := range []string{"image", tc.want} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should name %q", err, want)
			}
		}
	}
}
