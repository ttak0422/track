package site

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// fakeFrontend creates a minimal static-mode frontend build to copy into the site.
func fakeFrontend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<!doctype html><head><title>track</title><link rel="icon" type="image/svg+xml" href="/k_logo.svg" /><script>var t="__TRACK_DEFAULT_THEME__";window.__trackStartPage="__TRACK_START_PAGE__"</script>__TRACK_COLOR_OVERRIDES__</head><body><div id="root"></div></body>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func vaultStore(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:          vault,
		DBPath:            filepath.Join(vault, ".track", "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return cfg, s
}

func writeVaultNote(t *testing.T, cfg *config.Config, id int64, title, body string) {
	t.Helper()
	path := cfg.NotePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Version: note.CurrentMetadataVersion, Title: title}); err != nil {
		t.Fatal(err)
	}
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return v
}

func TestBuildVaultBundle(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n\ngo to [[Child]] and [[Outsider]]\n")
	writeVaultNote(t, cfg, 200, "Child", "# Child\n\nback [[Home]]\n")
	writeVaultNote(t, cfg, 300, "Outsider", "# Outsider\n")
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	res, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(res.Notes) != 2 {
		t.Fatalf("expected 2 published notes, got %v", res.Notes)
	}

	// Frontend copied in, with server-only placeholders substituted.
	if !fileExists(filepath.Join(out, "index.html")) || !fileExists(filepath.Join(out, "assets", "app.js")) {
		t.Fatalf("frontend not copied into site")
	}
	indexHTML, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if strings.Contains(string(indexHTML), "__TRACK_") {
		t.Fatalf("index.html still has unsubstituted placeholders:\n%s", indexHTML)
	}
	// The root's published id is baked in so the static site redirects to the start page on launch.
	if !strings.Contains(string(indexHTML), `window.__trackStartPage="`+PublishID(100)+`"`) {
		t.Fatalf("index.html should inject the root start page id:\n%s", indexHTML)
	}

	// notes.json holds the published set only.
	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	if len(notes.Notes) != 2 {
		t.Fatalf("notes.json should list 2 notes, got %d", len(notes.Notes))
	}

	// Root note: body keeps wiki links for the frontend; out-of-set note not in graph.
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if !strings.Contains(root.Note.Body, "[[Child]]") {
		t.Fatalf("root body should keep wiki links: %q", root.Note.Body)
	}
	// The published note carries the opaque slug, never the timestamp id or source path.
	if root.Note.NoteID != PublishID(100) || root.Note.Path != "" || root.Note.CopyPath != "" {
		t.Fatalf("note should publish the slug and drop the path: %+v", root.Note)
	}

	// Child note has a backlink from Home.
	child := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(200)+".json"))
	if len(child.Backlinks) != 1 || child.Backlinks[0].NoteID != PublishID(100) {
		t.Fatalf("child should have a backlink from 100, got %v", child.Backlinks)
	}

	// graph.json: edge 100->200 present; nothing references 300 (out of set).
	graph := readJSON[struct {
		Graph jsonGraph `json:"graph"`
	}](t, filepath.Join(out, "data", "graph.json"))
	if !hasEdge(graph.Graph.Edges, 100, 200) {
		t.Fatalf("graph missing edge 100->200: %+v", graph.Graph.Edges)
	}
	for _, n := range graph.Graph.Nodes {
		if n.NoteID == PublishID(300) {
			t.Fatalf("out-of-set note 300 should not be a graph node")
		}
	}

	// resolve.json maps titles to published notes only.
	resolve := readJSON[map[string]jsonRef](t, filepath.Join(out, "data", "resolve.json"))
	if resolve["Child"].NoteID != PublishID(200) {
		t.Fatalf("resolve[Child] should be 200, got %v", resolve["Child"])
	}
	if _, ok := resolve["Outsider"]; ok {
		t.Fatalf("out-of-set note should not be resolvable")
	}

	// site.json names the entry note; the calendar stays opt-in and defaults off.
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	if site.Root != PublishID(100) || site.Title != "Home" || site.Calendar {
		t.Fatalf("unexpected site.json: %+v", site)
	}
}

// TestBuildPublishesJournals covers the calendar's SSG source: journal notes live under journal/, not
// note/, so Build must resolve each selected id's path by its indexed file kind.
func TestBuildPublishesJournals(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n\nsee [[20260701]]\n")
	if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.JournalPath("20260701"), []byte("# day\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Journal sidecars written before the indexer skipped them may carry Days; the bundle must still
	// publish the journal without activity days, matching the live index.
	if err := note.WriteMetadata(
		cfg.MetadataPath(20260701),
		note.Metadata{
			Version: note.CurrentMetadataVersion,
			Title:   "20260701",
			Tags:    []string{"journal"},
			Days:    []string{"2026-07-01"},
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{20260701}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build with a journal in the set: %v", err)
	}

	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	var journal *jsonSearchResult
	for i := range notes.Notes {
		if notes.Notes[i].Title == "20260701" {
			journal = &notes.Notes[i]
		}
	}
	if journal == nil || journal.FileKind != "journal" {
		t.Fatalf("published journal should keep its kind and title, got %+v", notes.Notes)
	}
	if len(journal.Days) != 0 {
		t.Fatalf("a journal must publish no activity days, got %v", journal.Days)
	}
}

// TestBuildPublishesActivityDays covers the calendar's static data: notes.json carries each note's
// sidecar activity days so the published calendar can derive its per-day note lists.
func TestBuildPublishesActivityDays(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n")
	if err := note.WriteMetadata(
		cfg.MetadataPath(100),
		note.Metadata{Version: note.CurrentMetadataVersion, Title: "Home", Days: []string{"2026-07-03", "2026-07-05"}},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, Calendar: true}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	if len(notes.Notes) != 1 || len(notes.Notes[0].Days) != 2 || notes.Notes[0].Days[0] != "2026-07-03" {
		t.Fatalf("notes.json should carry activity days, got %+v", notes.Notes)
	}
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	if !site.Calendar {
		t.Fatalf("Options.Calendar should surface in site.json, got %+v", site)
	}
}

// TestBuildListsByRecency pins the shared note-list order in the bundle: notes.json and each note's
// backlinks list most recently updated first, matching the live server, so the published calendar,
// day pages, and backlinks read in the same order as the workspace.
func TestBuildListsByRecency(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Old", "# Old\n\n[[Target]]\n")
	writeVaultNote(t, cfg, 200, "New", "# New\n\n[[Target]]\n")
	writeVaultNote(t, cfg, 300, "Target", "# Target\n")
	base := time.Now().Add(-time.Hour)
	for id, offset := range map[int64]time.Duration{100: 0, 200: 2 * time.Minute, 300: time.Minute} {
		if err := os.Chtimes(cfg.NotePath(id), base.Add(offset), base.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 300, IDs: []int64{100, 200}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	notes := readJSON[struct {
		Notes []jsonSearchResult `json:"notes"`
	}](t, filepath.Join(out, "data", "notes.json"))
	titles := make([]string, 0, len(notes.Notes))
	for _, n := range notes.Notes {
		titles = append(titles, n.Title)
	}
	if len(titles) != 3 || titles[0] != "New" || titles[1] != "Target" || titles[2] != "Old" {
		t.Fatalf("notes.json should list most recently updated first, got %v", titles)
	}

	target := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(300)+".json"))
	if len(target.Backlinks) != 2 || target.Backlinks[0].Title != "New" || target.Backlinks[1].Title != "Old" {
		t.Fatalf("backlinks should list most recently updated first, got %+v", target.Backlinks)
	}
}

// TestBuildPublishesSearchCorpus pins the full-text half of the published search. The property that
// matters is exposure: the corpus must be byte-identical to the body already published in
// note/<slug>.json, so it can never become the one file in the built site that hands out what the
// rest of it hides — an original asset file name, an internal note id, or an unpublished note.
func TestBuildPublishesSearchCorpus(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n\nthe quick brown fox ![](assets/private-name.png)\n")
	writeVaultNote(t, cfg, 200, "Secret", "# Secret\n\nunpublished text\n")
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	corpus := readJSON[struct {
		Docs []jsonSearchDoc `json:"docs"`
	}](t, filepath.Join(out, "data", "search.json"))
	bodies := map[string]string{}
	for _, d := range corpus.Docs {
		bodies[d.NoteID] = d.Body
	}
	if len(bodies) != 1 {
		t.Fatalf("search.json should hold the published set only, got %v", bodies)
	}
	// The corpus body is the published body, not the source: same text as note/<slug>.json.
	published := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if bodies[PublishID(100)] != published.Note.Body {
		t.Fatalf("corpus body should equal the published body:\n corpus: %q\n note:   %q", bodies[PublishID(100)], published.Note.Body)
	}
	if !strings.Contains(bodies[PublishID(100)], "quick brown fox") {
		t.Fatalf("search.json should carry the note's text, got %q", bodies[PublishID(100)])
	}
	// Which means it cannot leak what the rest of the bundle hides.
	if strings.Contains(bodies[PublishID(100)], "private-name") {
		t.Fatalf("search.json leaked the source asset name: %q", bodies[PublishID(100)])
	}
	if _, ok := bodies["100"]; ok {
		t.Fatalf("search.json should key by publish slug, got %v", bodies)
	}
}

// TestBuildOrdersSearchCorpusLikeTheEngine pins the corpus order: a body search returns hits most
// recently updated first and breaks a tie by *highest* id (search.Sort), which is the opposite of
// the note listing's tie-break. The client keeps the file's order, so the file has to carry it.
func TestBuildOrdersSearchCorpusLikeTheEngine(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Root", "# Root\n\n[[Tie A]] [[Tie B]] [[Newer]]\n")
	writeVaultNote(t, cfg, 200, "Tie A", "# Tie A\n")
	writeVaultNote(t, cfg, 300, "Tie B", "# Tie B\n")
	writeVaultNote(t, cfg, 400, "Newer", "# Newer\n")
	base := time.Now().Add(-time.Hour)
	// 200 and 300 share an mtime, so only the id tie-break can order them.
	for id, offset := range map[int64]time.Duration{100: 0, 200: time.Minute, 300: time.Minute, 400: 2 * time.Minute} {
		if err := os.Chtimes(cfg.NotePath(id), base.Add(offset), base.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200, 300, 400}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	corpus := readJSON[struct {
		Docs []jsonSearchDoc `json:"docs"`
	}](t, filepath.Join(out, "data", "search.json"))
	got := make([]string, 0, len(corpus.Docs))
	for _, d := range corpus.Docs {
		got = append(got, d.NoteID)
	}
	want := []string{PublishID(400), PublishID(300), PublishID(200), PublishID(100)}
	if len(got) != len(want) {
		t.Fatalf("search.json should carry every published note, got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			// 300 before 200 is the point: the notes.json listing puts them the other way round.
			t.Fatalf("search.json order should be the engine's (mtime desc, id desc), got %v want %v", got, want)
		}
	}
}

// TestBuildRewritesChartNoteRefs pins the published-chart provenance contract: a chart datum's "note"
// reference (a vault note id) becomes the note's opaque publish slug, and a reference to a note
// outside the published set is dropped — the static site never navigates to a hidden internal id.
func TestBuildRewritesChartNoteRefs(t *testing.T) {
	cfg, s := vaultStore(t)
	body := "# Chart\n\n```viewspec\n" +
		`{"version":2,"mark":"line","title":"T",` +
		`"data":{"kind":"metric","records":[{"name":"m","time":"d1","value":1},{"name":"m","time":"d2","value":2}]},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"kind":"event","records":[` +
		`{"time":"d1","title":"linked","note":"200"},` +
		`{"time":"d2","title":"hidden","note":"999"}]}]}` +
		"\n```\n"
	writeVaultNote(t, cfg, 100, "Chart", body)
	writeVaultNote(t, cfg, 200, "Cited", "# Cited\n")
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	page := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if !strings.Contains(page.Note.Body, "```echarts") {
		t.Fatalf("viewspec fence should resolve: %s", page.Note.Body)
	}
	if !strings.Contains(page.Note.Body, `"note":"`+PublishID(200)+`"`) {
		t.Fatalf("published note ref should become its slug: %s", page.Note.Body)
	}
	if strings.Contains(page.Note.Body, "200") && strings.Contains(page.Note.Body, `"note":"200"`) {
		t.Fatalf("internal id must not leak: %s", page.Note.Body)
	}
	if strings.Contains(page.Note.Body, "999") {
		t.Fatalf("unpublished note ref should be dropped: %s", page.Note.Body)
	}
}

// TestBuildRewritesSpecAssetNoteRefs pins the same provenance contract for the isolated
// .viewspec.json asset path: the published .echarts.json carries publish slugs, never internal ids.
func TestBuildRewritesSpecAssetNoteRefs(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Chart", "# Chart\n\n![chart](assets/c.viewspec.json)\n")
	writeVaultNote(t, cfg, 200, "Cited", "# Cited\n")
	spec := `{"version":2,"mark":"line","title":"T",` +
		`"data":{"kind":"metric","records":[{"name":"m","time":"d1","value":1},{"name":"m","time":"d2","value":2}]},` +
		`"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]},` +
		`"overlays":[{"kind":"event","records":[` +
		`{"time":"d1","title":"linked","note":"200"},` +
		`{"time":"d2","title":"hidden","note":"999"}]}]}`
	if err := os.MkdirAll(filepath.Join(cfg.VaultDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.VaultDir, "assets", "c.viewspec.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(out, "assets", publishAssetName("c.viewspec.json")))
	if err != nil {
		t.Fatalf("resolved option not written: %v", err)
	}
	opt := string(raw)
	if !strings.Contains(opt, `"note":"`+PublishID(200)+`"`) {
		t.Fatalf("published note ref should become its slug: %s", opt)
	}
	if strings.Contains(opt, `"note":"200"`) || strings.Contains(opt, "999") {
		t.Fatalf("internal id must not leak from a spec asset: %s", opt)
	}
}

// TestBuildWritesPerNoteOGP pins the SSG's per-page OGP: every published note gets a real
// notes/<slug>/index.html carrying its own og:title/og:description, absolute og:url/og:image are gated
// on --base-url, and the root index.html previews the start note.
func TestBuildWritesPerNoteOGP(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home Page", "# Home Page\n\nlanding\n")
	if err := note.WriteMetadata(
		cfg.MetadataPath(200),
		note.Metadata{Version: note.CurrentMetadataVersion, Title: "Child Page", Description: "a child summary", Image: "assets/cover.png"},
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(200), []byte("# Child Page\n\nchild body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Without --base-url: relative-safe tags only, no absolute og:url/og:image.
	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}
	childPath := filepath.Join(out, "notes", PublishID(200), "index.html")
	child, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatalf("per-note page not written: %v", err)
	}
	page := string(child)
	if !strings.Contains(page, `<meta property="og:title" content="Child Page">`) {
		t.Fatalf("child page missing og:title: %s", page)
	}
	if !strings.Contains(page, `<meta property="og:description" content="a child summary">`) {
		t.Fatalf("child page missing og:description: %s", page)
	}
	if !strings.Contains(page, `<meta property="og:type" content="article">`) {
		t.Fatalf("child page missing og:type=article: %s", page)
	}
	if !strings.Contains(page, `<meta property="og:site_name" content="Home Page">`) {
		t.Fatalf("child page missing og:site_name (root title): %s", page)
	}
	if !strings.Contains(page, `content="summary_large_image"`) {
		t.Fatalf("a note with an image should use twitter summary_large_image: %s", page)
	}
	if strings.Contains(page, "og:url") || strings.Contains(page, "og:image") {
		t.Fatalf("absolute tags must be omitted without --base-url: %s", page)
	}
	// The root index.html previews the start note.
	rootHTML, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(rootHTML), `<meta property="og:title" content="Home Page">`) {
		t.Fatalf("root index.html should carry the start note's og:title: %s", rootHTML)
	}

	// With --base-url: absolute og:url and og:image are emitted.
	out2 := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}, BaseURL: "https://example.com/site/"}, fakeFrontend(t), out2); err != nil {
		t.Fatalf("build with base url: %v", err)
	}
	child2, _ := os.ReadFile(filepath.Join(out2, "notes", PublishID(200), "index.html"))
	page2 := string(child2)
	if !strings.Contains(page2, `<meta property="og:url" content="https://example.com/site/notes/`+PublishID(200)+`/">`) {
		t.Fatalf("with base url, child page should carry an absolute og:url: %s", page2)
	}
	if !strings.Contains(page2, `<meta property="og:image" content="https://example.com/site/assets/`+publishAssetName("cover.png")+`">`) {
		t.Fatalf("with base url, child page should carry an absolute og:image: %s", page2)
	}
}

func TestBuildRequiresRoot(t *testing.T) {
	cfg, s := vaultStore(t)
	if _, err := Build(cfg, s, Options{}, fakeFrontend(t), t.TempDir()); err == nil {
		t.Fatalf("expected error when root is missing")
	}
}

// A whole-line inline field is data that belongs in the prose (ADR 0032): a journal's "weight:: 68.2" is
// a line of the journal. It is indexed as a property AND published as the line the author wrote — the
// body is never edited on its way to a reader.
func TestBuildKeepsInlineFieldLinesInProse(t *testing.T) {
	cfg, s := vaultStore(t)
	body := "# Weight log\n\nMorning check-in.\n\nweight:: 68.2\n\nMet with [owner:: [[Ada]]] later.\n"
	writeVaultNote(t, cfg, 100, "Weight log", body)
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if !strings.Contains(root.Note.Body, "weight:: 68.2") {
		t.Errorf("the field line must render as the prose it is:\n%s", root.Note.Body)
	}
	if props := root.Note.Props; len(props) != 2 || props[0].Key != "weight" || props[1].Key != "owner" {
		t.Errorf("and still be indexed as properties: %+v", props)
	}
}

func hasEdge(edges []jsonGraphEdge, src, dst int64) bool {
	return hasSlugEdge(edges, PublishID(src), PublishID(dst))
}

func hasSlugEdge(edges []jsonGraphEdge, src, dst string) bool {
	for _, e := range edges {
		if e.SourceID == src && e.TargetID == dst {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestBuildRendersViewSpecFences verifies the note-embedded chart path: a valid ```viewspec block is
// replaced by a fenced ```echarts block carrying its resolved option (data.source read from the
// vault's data/), and a broken block publishes an inline error plus its source instead of failing the
// build.
func TestBuildRendersViewSpecFences(t *testing.T) {
	cfg, s := vaultStore(t)
	if err := os.MkdirAll(cfg.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := "{\"version\":1,\"name\":\"pi\",\"time\":\"01\",\"value\":3}\n{\"version\":1,\"name\":\"pi\",\"time\":\"02\",\"value\":7}\n"
	if err := os.WriteFile(filepath.Join(cfg.DataDir(), "metrics.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "# Charts\n\n```viewspec\n" +
		`{"version":2,"mark":"line","title":"PI","data":{"kind":"metric","source":"metrics.jsonl"},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}` +
		"\n```\n\n```viewspec\n{\"version\":2,\"mark\":\"pie\"}\n```\n\nafter\n"
	writeVaultNote(t, cfg, 100, "Charts", body)
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	note := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	if strings.Contains(note.Note.Body, "```viewspec") {
		t.Fatalf("viewspec fences should be replaced:\n%s", note.Note.Body)
	}
	// The valid block became a fenced echarts block carrying the resolved option inline.
	if !strings.Contains(note.Note.Body, "```echarts\n") {
		t.Fatalf("body should carry a resolved echarts block:\n%s", note.Note.Body)
	}
	if !strings.Contains(note.Note.Body, `"text":"PI"`) || !strings.Contains(note.Note.Body, `"data":["01","02"]`) {
		t.Fatalf("resolved option should inline the chart data:\n%s", note.Note.Body)
	}
	// The broken block shows an error at its position and keeps the source, and the page still built.
	if !strings.Contains(note.Note.Body, "> View Spec error:") || !strings.Contains(note.Note.Body, "```json") {
		t.Fatalf("broken spec should publish an inline error with its source:\n%s", note.Note.Body)
	}
	if !strings.Contains(note.Note.Body, "after") {
		t.Fatalf("content after the fences should survive:\n%s", note.Note.Body)
	}
}

// TestBuildPublishesProps covers the vault front-end's property flattening: sidecar props (via the
// metadata) and inline body fields both land in the published note JSON.
func TestBuildPublishesProps(t *testing.T) {
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n\nstatus:: draft\n")
	if err := note.WriteMetadata(cfg.MetadataPath(100), note.Metadata{
		Title: "Home",
		Props: map[string]any{"rating": 8},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	props := root.Note.Props
	if len(props) != 2 || props[0].Key != "rating" || props[0].Type != "number" ||
		props[1].Key != "status" || props[1].Value != "draft" {
		t.Fatalf("props = %+v", props)
	}
}

func TestPinnedSlugKeepsAPublishedURL(t *testing.T) {
	// A note imported from an already-published directory keeps whatever address it is reachable at;
	// without pinning, its new note id would derive a different slug and every existing link to it
	// would 404.
	cfg, s := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Home", "# Home\n\nsee [[Guide]]\n")
	writeVaultNote(t, cfg, 200, "Guide", "# Guide\n")
	const pinned = "LegacyGuideSlug0000000"
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(200))
	if err != nil {
		t.Fatal(err)
	}
	meta.Slug = pinned
	if err := note.WriteMetadata(cfg.MetadataPath(200), meta); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	// The page, its data file, and the link pointing at it all use the pinned address.
	if !fileExists(filepath.Join(out, "data", "note", pinned+".json")) {
		t.Fatalf("pinned slug should name the note's data file")
	}
	if fileExists(filepath.Join(out, "data", "note", PublishID(200)+".json")) {
		t.Fatalf("the id-derived slug must not be published alongside the pinned one")
	}
	// The listing and the link-resolution map — the two places a reader reaches the note through —
	// both address it by the pinned slug.
	for _, name := range []string{"notes.json", "resolve.json"} {
		raw, err := os.ReadFile(filepath.Join(out, "data", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), pinned) {
			t.Fatalf("%s should address the pinned note by its pinned slug:\n%s", name, raw)
		}
		if strings.Contains(string(raw), PublishID(200)) {
			t.Fatalf("%s still carries the id-derived slug:\n%s", name, raw)
		}
	}
}

// TestBuildPublishesSiteIcon covers the site icon (config web.icon): the vault file publishes under
// the fixed name icon.<ext> at the site root, site.json names it for the frontend's brand mark, and
// every emitted page's favicon link points at it instead of the built-in mark.
func TestBuildPublishesSiteIcon(t *testing.T) {
	cfg, s := vaultStore(t)
	cfg.WebIcon = "assets/logo.png"
	writeVaultNote(t, cfg, 100, "Home", "# Home\n")
	writeVaultNote(t, cfg, 200, "Child", "# Child\n")
	if err := os.MkdirAll(filepath.Join(cfg.VaultDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.VaultDir, "assets", "logo.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	out := t.TempDir()
	if _, err := Build(cfg, s, Options{Root: 100, IDs: []int64{200}}, fakeFrontend(t), out); err != nil {
		t.Fatalf("build: %v", err)
	}

	if raw, err := os.ReadFile(filepath.Join(out, "icon.png")); err != nil || string(raw) != "png-bytes" {
		t.Fatalf("icon.png should be the vault icon file, got %q, %v", raw, err)
	}
	site := readJSON[jsonSite](t, filepath.Join(out, "data", "site.json"))
	if site.Icon != "icon.png" {
		t.Fatalf("site.json should name the published icon, got %+v", site)
	}
	for _, rel := range []string{"index.html", filepath.Join("notes", PublishID(200), "index.html")} {
		raw, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `href="/icon.png"`) {
			t.Fatalf("%s favicon should point at the site icon:\n%s", rel, raw)
		}
		if strings.Contains(string(raw), "k_logo.svg") {
			t.Fatalf("%s still carries the built-in favicon:\n%s", rel, raw)
		}
	}
}

// A configured icon that is missing fails the build rather than publishing a brand-less site.
func TestBuildMissingSiteIconFails(t *testing.T) {
	cfg, s := vaultStore(t)
	cfg.WebIcon = "assets/nope.png"
	writeVaultNote(t, cfg, 100, "Home", "# Home\n")
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatalf("index: %v", err)
	}

	_, err := Build(cfg, s, Options{Root: 100}, fakeFrontend(t), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "web.icon") {
		t.Fatalf("a missing web.icon must fail the build naming the key, got %v", err)
	}
}

// swapFavicon keeps the href's directory prefix — the Vite base the frontend was built with — and
// swaps only the file name; without an icon or a favicon link it leaves the shell alone.
func TestSwapFavicon(t *testing.T) {
	cases := []struct {
		name, tmpl, icon, want string
	}{
		{
			"root base",
			`<head><link rel="icon" type="image/svg+xml" href="/k_logo.svg" /></head>`,
			"icon.png",
			`<head><link rel="icon" href="/icon.png" /></head>`,
		},
		{
			"subpath base survives",
			`<head><link rel="icon" type="image/svg+xml" href="/track/k_logo.svg" /></head>`,
			"icon.svg",
			`<head><link rel="icon" href="/track/icon.svg" /></head>`,
		},
		{
			"no icon is a no-op",
			`<head><link rel="icon" href="/k_logo.svg" /></head>`,
			"",
			`<head><link rel="icon" href="/k_logo.svg" /></head>`,
		},
		{
			"no favicon link is a no-op",
			`<head><title>t</title></head>`,
			"icon.png",
			`<head><title>t</title></head>`,
		},
	}
	for _, c := range cases {
		if got := swapFavicon(c.tmpl, c.icon); got != c.want {
			t.Errorf("%s: swapFavicon = %q, want %q", c.name, got, c.want)
		}
	}
}
