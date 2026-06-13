package cli

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// capture redirects stdout while fn runs and returns what it printed.
func capture(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), code
}

// run sets up an isolated vault for one Run invocation.
func runIn(t *testing.T, vault string, args ...string) (map[string]any, int) {
	t.Helper()
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(vault, ".test-cache"))
	out, code := capture(t, func() int { return Run(args) })
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q (err %v)", out, err)
	}
	return decoded, code
}

func runInWithStdin(t *testing.T, vault, stdin string, args ...string) (map[string]any, int) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(stdin); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		r.Close()
	})
	return runIn(t, vault, args...)
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

func TestVersion(t *testing.T) {
	out, code := capture(t, func() int { return Run([]string{"version"}) })
	if code != 0 || out != "track "+Version+"\n" {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, code := capture(t, func() int { return Run([]string{"bogus"}) })
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}

func TestNewResolveKeywordsFlow(t *testing.T) {
	vault := t.TempDir()

	created, code := runIn(t, vault, "new", "--title", "リンク", "--id", "1000")
	if code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	if created["id"].(float64) != 1000 {
		t.Fatalf("unexpected id: %v", created["id"])
	}
	noteContent, err := os.ReadFile(filepath.Join(vault, "note", "1000.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(noteContent), "<!--track") {
		t.Fatalf("note file should not contain metadata: %q", noteContent)
	}
	if string(noteContent) != "" {
		t.Fatalf("new without a body should write an empty note body, got %q", noteContent)
	}
	metaContent, err := os.ReadFile(vault + "/.track/notes/1000.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaContent), "version: 1") || !strings.Contains(string(metaContent), "title: リンク") {
		t.Fatalf("unexpected metadata content: %q", metaContent)
	}

	kws, code := runIn(t, vault, "keywords")
	if code != 0 {
		t.Fatalf("keywords failed: %v", kws)
	}
	list := kws["keywords"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["term"] != "リンク" {
		t.Fatalf("unexpected keywords: %v", list)
	}

	res, code := runIn(t, vault, "resolve", "--term", "リンク")
	if code != 0 || res["found"] != true {
		t.Fatalf("resolve failed: %v", res)
	}
	if res["note_id"].(float64) != 1000 {
		t.Fatalf("resolve note_id: %v", res["note_id"])
	}

	missing, _ := runIn(t, vault, "resolve", "--term", "なし")
	if missing["found"] != false {
		t.Fatalf("expected found=false, got %v", missing)
	}
}

func TestNewRefusesDuplicateTitle(t *testing.T) {
	vault := t.TempDir()

	if _, code := runIn(t, vault, "new", "--title", "Go", "--id", "100"); code != 0 {
		t.Fatalf("first new should succeed")
	}
	// A second note with the same title would make the keyword ambiguous; new must refuse it.
	out, code := runIn(t, vault, "new", "--title", "Go")
	if code != 1 || !strings.Contains(out["error"].(string), "already exists") {
		t.Fatalf("expected duplicate-title error, got code=%d out=%v", code, out)
	}
}

func TestOpenCreatesThenReopens(t *testing.T) {
	vault := t.TempDir()

	// First open creates the note and reports created=true.
	first, code := runIn(t, vault, "open", "--title", "Go")
	if code != 0 {
		t.Fatalf("open create failed: %v", first)
	}
	if first["created"] != true {
		t.Fatalf("expected created=true on first open, got %v", first)
	}
	id := first["id"].(float64)

	// Second open with the same title resolves to the same note without creating a duplicate.
	second, code := runIn(t, vault, "open", "--title", "Go")
	if code != 0 {
		t.Fatalf("open reopen failed: %v", second)
	}
	if second["created"] != false {
		t.Fatalf("expected created=false on reopen, got %v", second)
	}
	if second["id"].(float64) != id || second["path"] != first["path"] {
		t.Fatalf("reopen should return the same note: first=%v second=%v", first, second)
	}

	// Exactly one note (keyword) exists for the title.
	kws, _ := runIn(t, vault, "keywords")
	if list := kws["keywords"].([]any); len(list) != 1 {
		t.Fatalf("expected a single keyword after repeated opens, got %v", list)
	}
}

func TestNewRequiresTitle(t *testing.T) {
	out, code := runIn(t, t.TempDir(), "new")
	if code != 1 || !strings.Contains(out["error"].(string), "title") {
		t.Fatalf("expected title error, got code=%d out=%v", code, out)
	}
}

func TestRequiresTrackVault(t *testing.T) {
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", "")
	out, code := capture(t, func() int { return Run([]string{"keywords"}) })
	if code != 1 {
		t.Fatalf("expected exit 1 without TRACK_VAULT, got %d", code)
	}
	if !strings.Contains(out, "vault_dir is required") {
		t.Fatalf("expected vault config error, got %q", out)
	}
}

func TestBacklinksAndReindex(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Go", "--id", "100")
	runIn(t, vault, "new", "--title", "Other", "--id", "200")
	runIn(t, vault, "new", "--title", "Test", "--id", "300")

	// Make note 200 reference Go, and Go reference Test, then full reindex to build the link graph.
	if err := os.WriteFile(filepath.Join(vault, "note", "200.md"),
		[]byte("[[Go]] を参照\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "note", "100.md"),
		[]byte("# Go\n\n[[Test]] を参照\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, code := runIn(t, vault, "reindex", "--full")
	if code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}
	if rep["links"].(float64) != 2 {
		t.Fatalf("expected 2 links, got %v", rep["links"])
	}

	back, code := runIn(t, vault, "backlinks", "--id", "100")
	if code != 0 {
		t.Fatalf("backlinks failed: %v", back)
	}
	list := back["backlinks"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["note_id"].(float64) != 200 {
		t.Fatalf("expected note 200 backlink, got %v", list)
	}

	graph, code := runIn(t, vault, "graph", "--id", "100")
	if code != 0 {
		t.Fatalf("graph failed: %v", graph)
	}
	g := graph["graph"].(map[string]any)
	nodes := g["nodes"].([]any)
	edges := g["edges"].([]any)
	if len(nodes) != 3 || len(edges) != 2 {
		t.Fatalf("expected local graph with 3 nodes and 2 edges, got nodes=%v edges=%v", nodes, edges)
	}
}

func TestReindexKeepsMetadataTitleIgnoringBodyH1(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Old", "--id", "100")

	// Hand-editing the body H1 must not rename the note: the sidecar title is authoritative.
	if err := os.WriteFile(filepath.Join(vault, "note", "100.md"), []byte("# New\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, code := runIn(t, vault, "reindex", "--full")
	if code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}

	kws, code := runIn(t, vault, "keywords")
	if code != 0 {
		t.Fatalf("keywords failed: %v", kws)
	}
	list := kws["keywords"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["term"] != "Old" {
		t.Fatalf("expected keyword to stay Old, got %v", list)
	}
	metaContent, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaContent), "title: Old") {
		t.Fatalf("expected metadata title to stay Old, got %q", metaContent)
	}
	if _, err := os.Stat(vault + "/.track/renames.yaml"); !os.IsNotExist(err) {
		t.Fatalf("expected no rename history from a body edit, stat err=%v", err)
	}
}

func TestReindexResetsLegacyCacheDB(t *testing.T) {
	vault := t.TempDir()
	dbPath := filepath.Join(vault, "legacy-index.db")
	t.Setenv("TRACK_DB", dbPath)

	if err := os.MkdirAll(filepath.Join(vault, "note"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "note", "100.md"), []byte("# Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vault, ".track", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".track", "notes", "100.yaml"), []byte("version: 1\ntitle: Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  created TEXT,
  mtime INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE aliases (note_id INTEGER NOT NULL, alias TEXT NOT NULL);
CREATE TABLE tags (note_id INTEGER NOT NULL, tag TEXT NOT NULL);
CREATE TABLE links (src_id INTEGER NOT NULL, dst_id INTEGER NOT NULL);
CREATE VIEW keywords AS SELECT title AS term, id AS note_id, 'title' AS kind FROM notes;
PRAGMA user_version = 1;
`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	rep, code := runIn(t, vault, "reindex", "--full")
	if code != 0 {
		t.Fatalf("reindex should reset legacy db and rebuild: %v", rep)
	}
	if rep["indexed"].(float64) != 1 {
		t.Fatalf("expected one indexed note, got %v", rep)
	}

	kws, code := runIn(t, vault, "keywords")
	if code != 0 {
		t.Fatalf("keywords should work after reset: %v", kws)
	}
	list := kws["keywords"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["term"] != "Legacy" {
		t.Fatalf("unexpected keywords after reset: %v", list)
	}
}

func TestJournalIdempotent(t *testing.T) {
	vault := t.TempDir()
	wantVault := canonicalTestPath(t, vault)
	first, code := runIn(t, vault, "journal", "--offset", "0")
	if code != 0 || first["created"] != true {
		t.Fatalf("first journal: %v", first)
	}
	path := first["path"].(string)
	if filepath.Dir(path) != filepath.Join(wantVault, "journal") {
		t.Fatalf("journal path should be under journal dir, got %q", path)
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if len(name) != 8 || first["id"].(float64) == 0 {
		t.Fatalf("journal name/id should be yyyyMMdd, got name=%q id=%v", name, first["id"])
	}
	second, _ := runIn(t, vault, "journal", "--offset", "0")
	if second["created"] != false {
		t.Fatalf("second journal should not recreate, got %v", second)
	}
	if first["id"] != second["id"] {
		t.Fatalf("journal id changed between calls: %v vs %v", first["id"], second["id"])
	}
	if first["path"] != second["path"] {
		t.Fatalf("journal path changed between calls: %v vs %v", first["path"], second["path"])
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "" {
		t.Fatalf("journal without a body should write an empty note body, got %q", body)
	}
}

func TestTemplateCommands(t *testing.T) {
	vault := t.TempDir()
	wantVault := canonicalTestPath(t, vault)

	created, code := runIn(t, vault, "template", "new", "--name", "daily", "--id", "700")
	if code != 0 {
		t.Fatalf("template new failed: %v", created)
	}
	if created["created"] != true || created["name"] != "daily" || created["id"].(float64) != 700 {
		t.Fatalf("unexpected created template: %v", created)
	}
	path := filepath.Join(wantVault, "template", "700.template.md")
	if created["path"] != path {
		t.Fatalf("template path = %v, want %s", created["path"], path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "name: daily") || !strings.Contains(string(body), "# {{ title }}") {
		t.Fatalf("unexpected default template body: %q", body)
	}

	opened, code := runIn(t, vault, "template", "open", "--name", "daily")
	if code != 0 {
		t.Fatalf("template open failed: %v", opened)
	}
	if opened["created"] != false || opened["path"] != path {
		t.Fatalf("open should return the existing template: %v", opened)
	}

	listed, code := runIn(t, vault, "template", "list")
	if code != 0 {
		t.Fatalf("template list failed: %v", listed)
	}
	templates := listed["templates"].([]any)
	if len(templates) != 1 || templates[0].(map[string]any)["name"] != "daily" {
		t.Fatalf("unexpected template list: %v", templates)
	}

	dupe, code := runIn(t, vault, "template", "new", "--name", "daily")
	if code != 1 || !strings.Contains(dupe["error"].(string), "already exists") {
		t.Fatalf("expected duplicate template error, code=%d out=%v", code, dupe)
	}
}

func TestCreateFromTemplate(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "template", "new", "--name", "standard", "--id", "701"); code != 0 {
		t.Fatalf("template new failed")
	}
	templatePath := filepath.Join(vault, "template", "701.template.md")
	templateBody := "<!-- track-template\nname: standard\n-->\n# {{ title }}\n\nid={{ id }}\ndate={{ date }}\nkind={{ kind }}\n"
	if err := os.WriteFile(templatePath, []byte(templateBody), 0o644); err != nil {
		t.Fatal(err)
	}

	created, code := runIn(t, vault, "new", "--title", "Templated", "--id", "800", "--template", "standard")
	if code != 0 {
		t.Fatalf("new from template failed: %v", created)
	}
	noteBody, err := os.ReadFile(filepath.Join(vault, "note", "800.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(noteBody)
	if strings.Contains(got, "track-template") {
		t.Fatalf("rendered note should not contain template directive: %q", got)
	}
	if !strings.Contains(got, "# Templated") || !strings.Contains(got, "id=800") || !strings.Contains(got, "kind=note") {
		t.Fatalf("unexpected rendered note: %q", got)
	}

	journal, code := runIn(t, vault, "journal", "--template", "standard")
	if code != 0 {
		t.Fatalf("journal from template failed: %v", journal)
	}
	journalBody, err := os.ReadFile(journal["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journalBody), "kind=journal") {
		t.Fatalf("unexpected rendered journal: %q", journalBody)
	}

	search, code := runIn(t, vault, "search", "--query", "standard")
	if code != 0 {
		t.Fatalf("search failed: %v", search)
	}
	if got := len(search["results"].([]any)); got != 0 {
		t.Fatalf("templates should not appear in note search, got %v", search["results"])
	}
}

func TestSearch(t *testing.T) {
	vault := t.TempDir()
	wantVault := canonicalTestPath(t, vault)
	runIn(t, vault, "new", "--title", "Golang notes", "--id", "300")
	runIn(t, vault, "new", "--title", "Body note", "--id", "301")
	if err := os.WriteFile(filepath.Join(vault, "note", "301.md"), []byte("# Body note\n\nneedle body text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, code := runIn(t, vault, "journal", "--body", "# Journal\n\njournal body needle\n")
	if code != 0 {
		t.Fatalf("journal failed: %v", journal)
	}
	journalID := strconv.FormatInt(int64(journal["id"].(float64)), 10)
	journalPath := filepath.Join(wantVault, "journal", journalID+".md")
	if rep, code := runIn(t, vault, "reindex", "--full"); code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}

	res, code := runIn(t, vault, "search", "--query", "Golang")
	if code != 0 {
		t.Fatalf("search failed: %v", res)
	}
	if len(res["results"].([]any)) != 1 {
		t.Fatalf("expected 1 result, got %v", res["results"])
	}

	title, code := runIn(t, vault, "search", "--scope", "title", "--query", "Golang")
	if code != 0 {
		t.Fatalf("title search failed: %v", title)
	}
	if len(title["results"].([]any)) != 1 {
		t.Fatalf("expected 1 title result, got %v", title["results"])
	}

	journalTitle, code := runIn(t, vault, "search", "--scope", "title", "--query", journalID)
	if code != 0 {
		t.Fatalf("journal title search failed: %v", journalTitle)
	}
	journalTitleResults := journalTitle["results"].([]any)
	if len(journalTitleResults) != 1 {
		t.Fatalf("expected 1 journal title result, got %v", journalTitleResults)
	}
	journalTitleHit := journalTitleResults[0].(map[string]any)
	if journalTitleHit["file_kind"] != "journal" || journalTitleHit["path"] != journalPath {
		t.Fatalf("journal title result should point to journal path, got %v", journalTitleHit)
	}

	titleMiss, code := runIn(t, vault, "search", "--scope", "title", "--query", "needle")
	if code != 0 {
		t.Fatalf("title search miss failed: %v", titleMiss)
	}
	if len(titleMiss["results"].([]any)) != 0 {
		t.Fatalf("expected no title results, got %v", titleMiss["results"])
	}

	body, code := runIn(t, vault, "search", "--scope", "body", "--query", "needle body text")
	if code != 0 {
		t.Fatalf("body search failed: %v", body)
	}
	bodyResults := body["results"].([]any)
	if len(bodyResults) != 1 || bodyResults[0].(map[string]any)["title"] != "Body note" {
		t.Fatalf("expected body result, got %v", bodyResults)
	}
	// Body hits carry the matched line number (1-based) and its text for the picker/preview.
	bodyHit := bodyResults[0].(map[string]any)
	if bodyHit["line"].(float64) != 3 {
		t.Fatalf("expected body match on line 3, got %v", bodyHit["line"])
	}
	if bodyHit["snippet"] != "needle body text" {
		t.Fatalf("expected matched line snippet, got %v", bodyHit["snippet"])
	}

	journalBody, code := runIn(t, vault, "search", "--scope", "body", "--query", "journal body needle")
	if code != 0 {
		t.Fatalf("journal body search failed: %v", journalBody)
	}
	journalBodyResults := journalBody["results"].([]any)
	if len(journalBodyResults) != 1 {
		t.Fatalf("expected 1 journal body result, got %v", journalBodyResults)
	}
	journalBodyHit := journalBodyResults[0].(map[string]any)
	if journalBodyHit["file_kind"] != "journal" || journalBodyHit["path"] != journalPath {
		t.Fatalf("journal body result should point to journal path, got %v", journalBodyHit)
	}

	bad, code := runIn(t, vault, "search", "--scope", "bogus", "--query", "needle")
	if code != 1 || !strings.Contains(bad["error"].(string), "unknown search scope") {
		t.Fatalf("expected invalid scope error, code=%d out=%v", code, bad)
	}
}

func TestBabelExecRunsAndStores(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	if _, code := runIn(t, vault, "new", "--title", "Demo", "--id", "500"); code != 0 {
		t.Fatal("new failed")
	}
	body := "# Demo\n\n```sh :name hi :results output\necho hello\n```\n"
	if err := os.WriteFile(filepath.Join(vault, "note", "500.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "babel", "exec", "--id", "500", "--name", "hi")
	if code != 0 {
		t.Fatalf("babel exec failed: %v", out)
	}
	if out["status"] != "success" || out["stdout"] != "hello\n" || out["stored"] != true {
		t.Fatalf("unexpected result: %v", out)
	}

	metaContent, err := os.ReadFile(vault + "/.track/notes/500.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaContent), "version: 2") || !strings.Contains(string(metaContent), "hi:") {
		t.Fatalf("sidecar should store the block result at v2: %q", metaContent)
	}

	restored, code := runIn(t, vault, "babel", "restore", "--id", "500")
	if code != 0 {
		t.Fatalf("babel restore failed: %v", restored)
	}
	restoredBlocks := restored["blocks"].([]any)
	if len(restoredBlocks) != 1 {
		t.Fatalf("expected one restored block, got %v", restoredBlocks)
	}
	restoredBlock := restoredBlocks[0].(map[string]any)
	if restoredBlock["id"] != "hi" || restoredBlock["stdout"] != "hello\n" || restoredBlock["restored"] != true {
		t.Fatalf("unexpected restored block: %v", restoredBlock)
	}
	if int(restoredBlock["end_line"].(float64)) != 4 {
		t.Fatalf("expected restored end_line 4, got %v", restoredBlock["end_line"])
	}

	if err := os.WriteFile(filepath.Join(vault, "note", "500.md"), []byte("# Demo\n\n```sh :name hi :results output\necho changed\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, code := runIn(t, vault, "babel", "restore", "--id", "500")
	if code != 0 {
		t.Fatalf("babel restore after edit failed: %v", stale)
	}
	if got := len(stale["blocks"].([]any)); got != 0 {
		t.Fatalf("stale result should not be restored, got %v", stale["blocks"])
	}
}

func TestBabelExecResolvesJournalID(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	body := "# Journal\n\n```sh :name hi :results output\necho journal\n```\n"
	created, code := runIn(t, vault, "journal", "--body", body)
	if code != 0 {
		t.Fatalf("journal failed: %v", created)
	}
	id := strconv.FormatInt(int64(created["id"].(float64)), 10)

	out, code := runIn(t, vault, "babel", "exec", "--id", id, "--name", "hi")
	if code != 0 {
		t.Fatalf("babel exec journal by id failed: %v", out)
	}
	if out["status"] != "success" || out["stdout"] != "journal\n" {
		t.Fatalf("unexpected result: %v", out)
	}
}

func TestBabelExecByLine(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	runIn(t, vault, "new", "--title", "Demo", "--id", "502")
	// Two blocks; the cursor row (0-based) lands inside the second one.
	body := "# Demo\n\n```sh\necho first\n```\n\n```sh\necho second\n```\n"
	if err := os.WriteFile(filepath.Join(vault, "note", "502.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "babel", "exec", "--id", "502", "--line", "7")
	if code != 0 {
		t.Fatalf("babel exec by line failed: %v", out)
	}
	if out["stdout"] != "second\n" {
		t.Fatalf("expected the second block to run, got %v", out)
	}
	if int(out["end_line"].(float64)) != 8 {
		t.Fatalf("expected end_line 8, got %v", out["end_line"])
	}
}

func TestBabelExecCanUseStdinBody(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	runIn(t, vault, "new", "--title", "Demo", "--id", "503")
	if err := os.WriteFile(filepath.Join(vault, "note", "503.md"), []byte("# Demo\n\n```sh\necho saved\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unsaved := "# Demo\n\n```sh\necho unsaved\n```\n"

	out, code := runInWithStdin(t, vault, unsaved, "babel", "exec", "--id", "503", "--line", "3", "--body-stdin")
	if code != 0 {
		t.Fatalf("babel exec with stdin body failed: %v", out)
	}
	if out["stdout"] != "unsaved\n" {
		t.Fatalf("expected stdin body to run, got %v", out)
	}
}

func TestBabelExecRefusesEvalNo(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	runIn(t, vault, "new", "--title", "D", "--id", "501")
	if err := os.WriteFile(filepath.Join(vault, "note", "501.md"), []byte("# D\n\n```sh :name x :eval no\necho hi\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "babel", "exec", "--id", "501", "--name", "x")
	if code != 1 {
		t.Fatalf("expected refusal exit 1, got %d (%v)", code, out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "eval no") {
		t.Fatalf("expected :eval no error, got %v", out)
	}
}

func TestNewWithBodyAndTags(t *testing.T) {
	vault := t.TempDir()

	created, code := runIn(t, vault, "new", "--title", "Go", "--id", "100",
		"--body", "first line\n\n[[Other]] reference", "--tag", "lang,zettel", "--ai")
	if code != 0 {
		t.Fatalf("new with body failed: %v", created)
	}

	body, err := os.ReadFile(filepath.Join(vault, "note", "100.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "first line\n\n[[Other]] reference\n"
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}

	meta, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"lang", "zettel", "generated-by-ai"} {
		if !strings.Contains(string(meta), tag) {
			t.Fatalf("metadata %q missing tag %q", meta, tag)
		}
	}
}

func TestNewBodyFromStdin(t *testing.T) {
	vault := t.TempDir()

	created, code := runInWithStdin(t, vault, "piped body line\n", "new", "--title", "Piped", "--id", "110")
	if code != 0 {
		t.Fatalf("new with stdin body failed: %v", created)
	}
	body, err := os.ReadFile(filepath.Join(vault, "note", "110.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "piped body line\n"; string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestNewBodyAcceptsH1Verbatim(t *testing.T) {
	vault := t.TempDir()
	body := "# 記事\n\n## 背景\n本文 [[他ノート]]\n"
	created, code := runIn(t, vault, "new", "--title", "記事", "--id", "120", "--body", body)
	if code != 0 {
		t.Fatalf("new with H1 body failed: %v", created)
	}
	got, err := os.ReadFile(filepath.Join(vault, "note", "120.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
	meta, err := os.ReadFile(vault + "/.track/notes/120.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), "title: 記事") {
		t.Fatalf("expected sidecar title, got %q", meta)
	}
}

func TestNewWithoutBodyWritesEmptyFile(t *testing.T) {
	vault := t.TempDir()
	created, code := runIn(t, vault, "new", "--title", "Empty", "--id", "130")
	if code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	got, err := os.ReadFile(filepath.Join(vault, "note", "130.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "" {
		t.Fatalf("body = %q, want empty", got)
	}
	meta, err := os.ReadFile(vault + "/.track/notes/130.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), "title: Empty") {
		t.Fatalf("expected sidecar title, got %q", meta)
	}
}

func TestJournalBodyAcceptsH1Verbatim(t *testing.T) {
	vault := t.TempDir()
	body := "# Journal Heading\n\nentry\n"
	created, code := runIn(t, vault, "journal", "--body", body)
	if code != 0 {
		t.Fatalf("journal with body failed: %v", created)
	}
	got, err := os.ReadFile(created["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestNewBodyTemplateExclusive(t *testing.T) {
	out, code := runIn(t, t.TempDir(), "new", "--title", "Go", "--template", "x", "--body", "text")
	if code != 1 || !strings.Contains(out["error"].(string), "--body cannot be combined with --template") {
		t.Fatalf("expected body/template exclusivity error, got code=%d out=%v", code, out)
	}
}

func TestAppendUpdatesBodyAndBacklinksWithoutReindex(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Target", "--id", "200")
	runIn(t, vault, "new", "--title", "Source", "--id", "100")

	// Append a link from Source to Target via the CLI; index.One must pick up the new outgoing link,
	// so Target's backlinks reflect it without any full reindex.
	appended, code := runIn(t, vault, "append", "--id", "100", "--body", "see [[Target]] for details")
	if code != 0 {
		t.Fatalf("append failed: %v", appended)
	}

	body, err := os.ReadFile(filepath.Join(vault, "note", "100.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "see [[Target]] for details\n"; string(body) != want {
		t.Fatalf("appended body = %q, want %q", body, want)
	}

	back, code := runIn(t, vault, "backlinks", "--id", "200")
	if code != 0 {
		t.Fatalf("backlinks failed: %v", back)
	}
	list := back["backlinks"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["note_id"].(float64) != 100 {
		t.Fatalf("expected note 100 backlink without reindex, got %v", list)
	}
}

func TestAppendByTitleMergesTags(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Go", "--id", "100", "--tag", "lang")

	if _, code := runIn(t, vault, "append", "--title", "Go", "--tag", "lang,zettel", "--ai"); code != 0 {
		t.Fatalf("append tags failed")
	}
	meta, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// "lang" was already present; it must not be duplicated, and new tags must be added.
	if got := strings.Count(string(meta), "- lang\n"); got != 1 {
		t.Fatalf("expected lang tag once, got %d in %q", got, meta)
	}
	for _, tag := range []string{"zettel", "generated-by-ai"} {
		if !strings.Contains(string(meta), tag) {
			t.Fatalf("metadata %q missing tag %q", meta, tag)
		}
	}
}

func TestAppendRequiresContent(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Go", "--id", "100")
	out, code := runIn(t, vault, "append", "--id", "100")
	if code != 1 || !strings.Contains(out["error"].(string), "nothing to do") {
		t.Fatalf("expected nothing-to-do error, got code=%d out=%v", code, out)
	}
}

func TestOpenExistingWithContentErrors(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "open", "--title", "Go")
	out, code := runIn(t, vault, "open", "--title", "Go", "--body", "more")
	if code != 1 || !strings.Contains(out["error"].(string), "track append") {
		t.Fatalf("expected append guidance on existing note, got code=%d out=%v", code, out)
	}
}

func TestRenameUpdatesTitleAndBacklinks(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Old", "--id", "100")
	runIn(t, vault, "new", "--title", "Source", "--id", "200", "--body", "see [[Old]]\n")
	if rep, code := runIn(t, vault, "reindex", "--full"); code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}

	renamed, code := runIn(t, vault, "rename", "--id", "100", "--to", "New")
	if code != 0 {
		t.Fatalf("rename failed: %v", renamed)
	}
	if renamed["old_title"] != "Old" || renamed["new_title"] != "New" || renamed["backlinks_updated"].(float64) != 1 {
		t.Fatalf("unexpected rename output: %v", renamed)
	}
	source, err := os.ReadFile(filepath.Join(vault, "note", "200.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != "see [[New]]\n" {
		t.Fatalf("expected backlink rewrite, got %q", source)
	}
	meta, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), "title: New") {
		t.Fatalf("expected new title metadata, got %q", meta)
	}
	res, code := runIn(t, vault, "resolve", "--term", "New")
	if code != 0 || res["found"] != true || res["note_id"].(float64) != 100 {
		t.Fatalf("resolve New failed: %v", res)
	}
	renames, err := os.ReadFile(vault + "/.track/renames.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(renames), "from: Old") || !strings.Contains(string(renames), "to: New") || !strings.Contains(string(renames), "note_id: 100") {
		t.Fatalf("expected rename history, got %q", renames)
	}
}

func TestRenameRejectsDuplicateTitle(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Old", "--id", "100")
	runIn(t, vault, "new", "--title", "Other", "--id", "200")

	out, code := runIn(t, vault, "rename", "--id", "100", "--to", "Other")
	if code != 1 || !strings.Contains(out["error"].(string), "already in use") {
		t.Fatalf("expected duplicate title error, code=%d out=%v", code, out)
	}
}

func TestRenameNoOp(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Old", "--id", "100")

	out, code := runIn(t, vault, "rename", "--id", "100", "--to", "Old")
	if code != 0 {
		t.Fatalf("rename no-op failed: %v", out)
	}
	if out["old_title"] != "Old" || out["new_title"] != "Old" || out["backlinks_updated"].(float64) != 0 {
		t.Fatalf("unexpected no-op output: %v", out)
	}
}
