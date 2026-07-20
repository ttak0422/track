package cli

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

// noteKeywords returns the note-kind keyword terms, filtering out the journal/summary keywords that note
// creation now auto-generates for the day (the day's journal plus its month/year summaries).
func noteKeywords(list []any) []string {
	var out []string
	for _, item := range list {
		m := item.(map[string]any)
		if m["file_kind"] == "note" {
			out = append(out, m["term"].(string))
		}
	}
	return out
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
	if string(noteContent) != "# リンク\n" {
		t.Fatalf("new without a body should apply the builtin default template, got %q", noteContent)
	}
	metaContent, err := os.ReadFile(vault + "/.track/notes/1000.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Creating a note records its activity day, so the sidecar is written at version 3 with a days list.
	if !strings.Contains(string(metaContent), "version: 3") || !strings.Contains(string(metaContent), "title: リンク") {
		t.Fatalf("unexpected metadata content: %q", metaContent)
	}
	if !strings.Contains(string(metaContent), "days:") {
		t.Fatalf("metadata should record the creation day: %q", metaContent)
	}

	kws, code := runIn(t, vault, "keywords")
	if code != 0 {
		t.Fatalf("keywords failed: %v", kws)
	}
	if terms := noteKeywords(kws["keywords"].([]any)); len(terms) != 1 || terms[0] != "リンク" {
		t.Fatalf("unexpected note keywords: %v", kws["keywords"])
	}

	res, code := runIn(t, vault, "resolve", "--term", "リンク")
	if code != 0 || res["found"] != true {
		t.Fatalf("resolve failed: %v", res)
	}
	if res["note_id"].(float64) != 1000 {
		t.Fatalf("resolve note_id: %v", res["note_id"])
	}

	// The keyword may also be passed positionally, mirroring `asset import`.
	pos, code := runIn(t, vault, "resolve", "リンク")
	if code != 0 || pos["found"] != true || pos["note_id"].(float64) != 1000 {
		t.Fatalf("positional resolve failed: %v", pos)
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

	// Exactly one note (keyword) exists for the title (journal keywords are auto-generated and ignored).
	kws, _ := runIn(t, vault, "keywords")
	if terms := noteKeywords(kws["keywords"].([]any)); len(terms) != 1 || terms[0] != "Go" {
		t.Fatalf("expected a single note keyword after repeated opens, got %v", kws["keywords"])
	}
}

func TestNewRequiresTitle(t *testing.T) {
	out, code := runIn(t, t.TempDir(), "new")
	if code != 1 || !strings.Contains(out["error"].(string), "title") {
		t.Fatalf("expected title error, got code=%d out=%v", code, out)
	}
}

func TestDefaultsToHomeTrackVault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())

	// With nothing configured the vault defaults to $HOME/track (ADR 0015): the command succeeds and
	// creates the note under that vault rather than failing.
	out, code := capture(t, func() int { return Run([]string{"new", "--title", "Solo", "--id", "100"}) })
	if code != 0 {
		t.Fatalf("expected default-vault success, got exit %d: %s", code, out)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	path, _ := decoded["path"].(string)
	if !strings.HasSuffix(path, filepath.Join("track", "note", "100.md")) {
		t.Fatalf("note should be created under the default $HOME/track vault, got %q", path)
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
	// Two note links (200->Go, Go->Test) plus two from the auto-generated journal summaries
	// (month->day, year->month).
	if rep["links"].(float64) != 4 {
		t.Fatalf("expected 4 links, got %v", rep["links"])
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

func TestAgendaListsNotesByDay(t *testing.T) {
	vault := t.TempDir()
	created, code := runIn(t, vault, "new", "--title", "Today A")
	if code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	idA := int64(created["id"].(float64))

	// The default date is today, which is when the note above was created and stamped.
	out, code := runIn(t, vault, "agenda")
	if code != 0 {
		t.Fatalf("agenda failed: %v", out)
	}
	notes, ok := out["notes"].([]any)
	if !ok || len(notes) != 1 {
		t.Fatalf("agenda notes = %v, want one note", out["notes"])
	}
	first := notes[0].(map[string]any)
	if int64(first["note_id"].(float64)) != idA || first["title"] != "Today A" {
		t.Fatalf("unexpected agenda note: %v", first)
	}

	// A day with no activity returns an empty list rather than an error.
	empty, code := runIn(t, vault, "agenda", "--date", "2020-01-01")
	if code != 0 {
		t.Fatalf("agenda failed: %v", empty)
	}
	if empty["date"] != "2020-01-01" {
		t.Fatalf("agenda echoed date = %v, want 2020-01-01", empty["date"])
	}
	if list, _ := empty["notes"].([]any); len(list) != 0 {
		t.Fatalf("agenda for empty day = %v, want none", empty["notes"])
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
	if terms := noteKeywords(kws["keywords"].([]any)); len(terms) != 1 || terms[0] != "Old" {
		t.Fatalf("expected keyword to stay Old, got %v", kws["keywords"])
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
	if !strings.Contains(string(body), "# "+name) {
		t.Fatalf("journal without a body should apply the builtin journal template, got %q", body)
	}
}

func TestJournalCreatesMonthAndYearSummaries(t *testing.T) {
	vault := t.TempDir()
	wantVault := canonicalTestPath(t, vault)
	first, code := runIn(t, vault, "journal", "--offset", "0")
	if code != 0 || first["created"] != true {
		t.Fatalf("first journal: %v", first)
	}
	day := strings.TrimSuffix(filepath.Base(first["path"].(string)), ".md")
	month, year := day[:6], day[:4]

	monthPath := filepath.Join(wantVault, "journal", month+".md")
	monthBody, err := os.ReadFile(monthPath)
	if err != nil {
		t.Fatalf("month summary not created: %v", err)
	}
	if !strings.Contains(string(monthBody), "[["+day+"]]") {
		t.Fatalf("month summary should link the day, got %q", monthBody)
	}

	yearPath := filepath.Join(wantVault, "journal", year+".md")
	yearBody, err := os.ReadFile(yearPath)
	if err != nil {
		t.Fatalf("year summary not created: %v", err)
	}
	if !strings.Contains(string(yearBody), "[["+month+"]]") {
		t.Fatalf("year summary should link the month, got %q", yearBody)
	}

	// Reopening the day must not append a duplicate link to the month summary.
	if _, code := runIn(t, vault, "journal", "--offset", "0"); code != 0 {
		t.Fatalf("reopen journal failed")
	}
	monthBody2, err := os.ReadFile(monthPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(monthBody2), "[["+day+"]]"); got != 1 {
		t.Fatalf("day link should appear exactly once, got %d: %q", got, monthBody2)
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

	// Create the journal from the template first: creating a note auto-creates today's journal (with the
	// configured/builtin journal template), so an explicit `journal --template` must run before any note
	// that day to take effect.
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

	search, code := runIn(t, vault, "search", "--query", "standard")
	if code != 0 {
		t.Fatalf("search failed: %v", search)
	}
	if got := len(search["results"].([]any)); got != 0 {
		t.Fatalf("templates should not appear in note search, got %v", search["results"])
	}
}

func TestTemplateParentSubstitution(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Project X", "--id", "900"); code != 0 {
		t.Fatalf("create parent failed")
	}
	parentPath := filepath.Join(vault, "note", "900.md")

	if _, code := runIn(t, vault, "template", "new", "--name", "child", "--id", "710"); code != 0 {
		t.Fatalf("template new failed")
	}
	tmpl := "<!-- track-template\nname: child\n-->\nparent={{ parent }}\n"
	if err := os.WriteFile(filepath.Join(vault, "template", "710.template.md"), []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}

	created, code := runIn(t, vault, "open", "--title", "Child", "--template", "child", "--parent-path", parentPath)
	if code != 0 {
		t.Fatalf("open from template failed: %v", created)
	}
	body, err := os.ReadFile(created["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "parent=Project X") {
		t.Fatalf("template {{ parent }} should resolve to the parent title, got %q", body)
	}

	// Without --parent-path, {{ parent }} renders empty rather than failing.
	orphan, code := runIn(t, vault, "open", "--title", "Orphan", "--template", "child")
	if code != 0 {
		t.Fatalf("open without parent failed: %v", orphan)
	}
	body2, err := os.ReadFile(orphan["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body2), "Project X") || !strings.Contains(string(body2), "parent=") {
		t.Fatalf("missing parent should leave {{ parent }} empty, got %q", body2)
	}
}

func TestSearch(t *testing.T) {
	vault := t.TempDir()
	wantVault := canonicalTestPath(t, vault)
	// Create the journal with explicit content first: creating a note now auto-creates today's journal,
	// so the explicit `journal --body` must run before any note that day.
	journal, code := runIn(t, vault, "journal", "--body", "# Journal\n\njournal body needle\n")
	if code != 0 {
		t.Fatalf("journal failed: %v", journal)
	}
	runIn(t, vault, "new", "--title", "Golang notes", "--id", "300")
	runIn(t, vault, "new", "--title", "Body note", "--id", "301")
	if err := os.WriteFile(filepath.Join(vault, "note", "301.md"), []byte("# Body note\n\nneedle body text\n"), 0o644); err != nil {
		t.Fatal(err)
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

// TestSearchBodyFTSReindexAndFallback exercises the FTS body search end to end: text inside a code
// fence is searchable, an edit picked up only by the self-heal reindex changes results, and CJK works
// on both the trigram-indexed path (3+ chars) and the short-term scan fallback (2 chars).
func TestSearchBodyFTSReindexAndFallback(t *testing.T) {
	vault := t.TempDir()

	runIn(t, vault, "new", "--title", "Deploy runbook", "--id", "800")
	notePath := filepath.Join(vault, "note", "800.md")
	writeBody := func(body string, mtime time.Time) {
		t.Helper()
		if err := os.WriteFile(notePath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(notePath, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	bodyHitIDs := func(query string) []int64 {
		t.Helper()
		res, code := runIn(t, vault, "search", "--scope", "body", "--query", query)
		if code != 0 {
			t.Fatalf("body search %q failed: %v", query, res)
		}
		var ids []int64
		for _, r := range res["results"].([]any) {
			ids = append(ids, int64(r.(map[string]any)["note_id"].(float64)))
		}
		return ids
	}

	// A term that only appears inside a fenced code block is still indexed and searchable.
	writeBody("# Deploy\n\n```yaml\nruntime: containerd\n```\n", time.Now())
	if rep, code := runIn(t, vault, "reindex", "--full"); code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}
	if got := bodyHitIDs("containerd"); !slices.Equal(got, []int64{800}) {
		t.Fatalf("code-block term should be searchable, got %v", got)
	}

	// Edit the file directly and bump its mtime so only the pre-read self-heal reindex (not an explicit
	// reindex) refreshes the FTS index. The old term must disappear and the new one appear.
	writeBody("# Deploy\n\nmigrated to servicemesh routing\n", time.Now().Add(10*time.Second))
	if got := bodyHitIDs("containerd"); len(got) != 0 {
		t.Fatalf("stale term should be gone after self-heal reindex, got %v", got)
	}
	if got := bodyHitIDs("servicemesh"); !slices.Equal(got, []int64{800}) {
		t.Fatalf("edited-in term should be searchable after self-heal reindex, got %v", got)
	}

	// CJK: 3-character テスト uses the trigram index; 2-character 世界 uses the scan fallback. Both find it.
	runIn(t, vault, "new", "--title", "日本語メモ", "--id", "801")
	cjkPath := filepath.Join(vault, "note", "801.md")
	if err := os.WriteFile(cjkPath, []byte("# メモ\n\nこれは世界についてのテスト本文です\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rep, code := runIn(t, vault, "reindex", "--full"); code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}
	if got := bodyHitIDs("テスト"); !slices.Equal(got, []int64{801}) {
		t.Fatalf("3-char CJK query (FTS path) should match, got %v", got)
	}
	if got := bodyHitIDs("世界"); !slices.Equal(got, []int64{801}) {
		t.Fatalf("2-char CJK query (scan fallback) should match, got %v", got)
	}

	// OR spans notes served by different paths: servicemesh (800) and the 2-char 世界 (801). A short
	// term routes the whole query through the scan fallback, which honours the same OR grouping.
	if got := bodyHitIDs("世界 OR servicemesh"); func() bool { slices.Sort(got); return !slices.Equal(got, []int64{800, 801}) }() {
		t.Fatalf("OR across both notes should match both, got %v", got)
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
	// The note also carries an activity day from creation, so its sidecar is at version 3 while still
	// storing the babel block result.
	if !strings.Contains(string(metaContent), "version: 3") || !strings.Contains(string(metaContent), "hi:") {
		t.Fatalf("sidecar should store the block result: %q", metaContent)
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
		"--body", "first line\n\n[[Other]] reference", "--tag", "lang,zettel")
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
	for _, tag := range []string{"lang", "zettel"} {
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

func TestNewWithoutBodyAppliesDefaultTemplate(t *testing.T) {
	vault := t.TempDir()
	created, code := runIn(t, vault, "new", "--title", "Empty", "--id", "130")
	if code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	got, err := os.ReadFile(filepath.Join(vault, "note", "130.md"))
	if err != nil {
		t.Fatal(err)
	}
	// With neither --template nor --body, the shipped builtin "default" template is applied.
	if string(got) != "# Empty\n" {
		t.Fatalf("body = %q, want the builtin default template", got)
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
	// An explicit --body keeps this note's content independent of the default template.
	runIn(t, vault, "new", "--title", "Source", "--id", "100", "--body", "intro")

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
	if want := "intro\n\nsee [[Target]] for details\n"; string(body) != want {
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

	if _, code := runIn(t, vault, "append", "--title", "Go", "--tag", "lang,zettel"); code != 0 {
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
	for _, tag := range []string{"zettel"} {
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

func TestUpdateReplacesBodyAndBacklinksWithoutReindex(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Old", "--id", "100")
	runIn(t, vault, "new", "--title", "Target", "--id", "200")
	runIn(t, vault, "new", "--title", "Source", "--id", "300", "--body", "see [[Old]]")

	updated, code := runIn(t, vault, "update", "--title", "Source", "--body", "now see [[Target]]")
	if code != 0 {
		t.Fatalf("update failed: %v", updated)
	}
	if updated["body_updated"] != true || updated["tags_updated"] != false {
		t.Fatalf("unexpected update output: %v", updated)
	}
	body, err := os.ReadFile(filepath.Join(vault, "note", "300.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "now see [[Target]]\n" {
		t.Fatalf("updated body = %q", body)
	}

	oldBacklinks, code := runIn(t, vault, "backlinks", "--id", "100")
	if code != 0 {
		t.Fatalf("old backlinks failed: %v", oldBacklinks)
	}
	if len(oldBacklinks["backlinks"].([]any)) != 0 {
		t.Fatalf("old target should have no backlinks after update: %v", oldBacklinks)
	}
	targetBacklinks, code := runIn(t, vault, "backlinks", "--id", "200")
	if code != 0 {
		t.Fatalf("target backlinks failed: %v", targetBacklinks)
	}
	list := targetBacklinks["backlinks"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["note_id"].(float64) != 300 {
		t.Fatalf("expected note 300 backlink without reindex, got %v", list)
	}
}

func TestUpdateCanClearBodyAndReplaceTags(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Go", "--id", "100", "--body", "body", "--tag", "old,lang")

	updated, code := runIn(t, vault, "update", "--id", "100", "--body", "", "--clear-tags", "--tag", "zettel,lang")
	if code != 0 {
		t.Fatalf("update failed: %v", updated)
	}
	if updated["body_updated"] != true || updated["tags_updated"] != true {
		t.Fatalf("unexpected update output: %v", updated)
	}
	body, err := os.ReadFile(filepath.Join(vault, "note", "100.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "" {
		t.Fatalf("body should be cleared, got %q", body)
	}
	meta, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), "- old\n") {
		t.Fatalf("metadata should not keep old tag: %q", meta)
	}
	for _, tag := range []string{"zettel", "lang"} {
		if !strings.Contains(string(meta), "- "+tag+"\n") {
			t.Fatalf("metadata %q missing tag %q", meta, tag)
		}
	}
}

func TestUpdateRequiresContent(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Go", "--id", "100")
	out, code := runIn(t, vault, "update", "--id", "100")
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

func TestDoctorReportsOrphanSidecar(t *testing.T) {
	vault := t.TempDir()
	// A clean note plus an orphan sidecar (its markdown was removed by a sync gap).
	if _, code := runIn(t, vault, "new", "--title", "Alpha", "--id", "100"); code != 0 {
		t.Fatalf("new failed")
	}
	orphan := filepath.Join(vault, ".track", "notes", "999.yaml")
	if err := os.WriteFile(orphan, []byte("version: 1\ntitle: Gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "doctor")
	if code != 0 {
		t.Fatalf("doctor exit = %d, want 0 (issues are not errors): %v", code, out)
	}
	if out["ok"] != false {
		t.Fatalf("ok = %v, want false", out["ok"])
	}
	issues, _ := out["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("issues = %v, want one orphan_sidecar", out["issues"])
	}
	first, _ := issues[0].(map[string]any)
	if first["kind"] != "orphan_sidecar" || first["id"].(float64) != 999 {
		t.Fatalf("unexpected issue: %v", first)
	}
}

func TestDoctorFixRestoresAndReindexes(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Alpha", "--id", "100"); code != 0 {
		t.Fatalf("new failed")
	}
	// Drop the sidecar so the note looks lost (a sync gap).
	if err := os.Remove(filepath.Join(vault, ".track", "notes", "100.yaml")); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "doctor", "--fix")
	if code != 0 {
		t.Fatalf("doctor --fix exit = %d: %v", code, out)
	}
	if out["changed"] != true {
		t.Fatalf("expected changed=true, got %v", out)
	}
	fixed, _ := out["fixed"].([]any)
	if len(fixed) != 1 || fixed[0].(map[string]any)["kind"] != "missing_sidecar" {
		t.Fatalf("expected one missing_sidecar fix, got %v", out["fixed"])
	}
	if out["reindexed"] == nil {
		t.Fatalf("fix should reindex, got %v", out)
	}

	// A second pass is a clean no-op.
	out2, _ := runIn(t, vault, "doctor", "--fix")
	if out2["changed"] != false {
		t.Fatalf("second --fix should be a no-op, got %v", out2)
	}
}

func TestDefaultTemplateAppliedAndOverridden(t *testing.T) {
	vault := t.TempDir()
	// A template literally named "default" is the zero-config default for new notes.
	if _, code := runIn(t, vault, "template", "new", "--name", "default", "--id", "900"); code != 0 {
		t.Fatalf("create default template failed")
	}
	if err := os.WriteFile(
		filepath.Join(vault, "template", "900.template.md"),
		[]byte("<!-- track-template\nname: default\n-->\n# {{ title }}\n\nfrom-default kind={{ kind }}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	// A second template used to prove --template overrides the default.
	if _, code := runIn(t, vault, "template", "new", "--name", "alt", "--id", "901"); code != 0 {
		t.Fatalf("create alt template failed")
	}
	if err := os.WriteFile(
		filepath.Join(vault, "template", "901.template.md"),
		[]byte("<!-- track-template\nname: alt\n-->\n# {{ title }}\n\nfrom-alt\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// No --template, no --body: the default template is applied.
	if created, code := runIn(t, vault, "new", "--title", "Auto", "--id", "1001"); code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	got := readFileString(t, filepath.Join(vault, "note", "1001.md"))
	if !strings.Contains(got, "# Auto") || !strings.Contains(got, "from-default kind=note") {
		t.Fatalf("default template not applied: %q", got)
	}
	if strings.Contains(got, "track-template") {
		t.Fatalf("template directive leaked into note: %q", got)
	}

	// An explicit --template overrides the default.
	if created, code := runIn(t, vault, "new", "--title", "Picked", "--id", "1002", "--template", "alt"); code != 0 {
		t.Fatalf("new --template failed: %v", created)
	}
	got = readFileString(t, filepath.Join(vault, "note", "1002.md"))
	if !strings.Contains(got, "from-alt") || strings.Contains(got, "from-default") {
		t.Fatalf("explicit --template did not override default: %q", got)
	}

	// An explicit --body opts out of the default entirely.
	if created, code := runIn(t, vault, "new", "--title", "Manual", "--id", "1003", "--body", "just text"); code != 0 {
		t.Fatalf("new --body failed: %v", created)
	}
	got = readFileString(t, filepath.Join(vault, "note", "1003.md"))
	if got != "just text\n" {
		t.Fatalf("--body should skip the default template, got %q", got)
	}
}

func TestDefaultTemplateConfiguredName(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "template", "new", "--name", "alt", "--id", "902"); code != 0 {
		t.Fatalf("create alt template failed")
	}
	if err := os.WriteFile(
		filepath.Join(vault, "template", "902.template.md"),
		[]byte("<!-- track-template\nname: alt\n-->\n# {{ title }}\n\nconfigured-default\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	// config (here via the env override) names the default; no template literally named "default" exists.
	t.Setenv("TRACK_DEFAULT_TEMPLATE", "alt")

	if created, code := runIn(t, vault, "new", "--title", "Cfg", "--id", "1004"); code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	got := readFileString(t, filepath.Join(vault, "note", "1004.md"))
	if !strings.Contains(got, "configured-default") {
		t.Fatalf("configured default template not applied: %q", got)
	}
}

func TestJournalDefaultTemplate(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "template", "new", "--name", "journal", "--id", "903"); code != 0 {
		t.Fatalf("create journal template failed")
	}
	if err := os.WriteFile(
		filepath.Join(vault, "template", "903.template.md"),
		[]byte("<!-- track-template\nname: journal\n-->\n# {{ title }}\n\ndaily kind={{ kind }}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	journal, code := runIn(t, vault, "journal")
	if code != 0 {
		t.Fatalf("journal failed: %v", journal)
	}
	got := readFileString(t, journal["path"].(string))
	if !strings.Contains(got, "daily kind=journal") {
		t.Fatalf("journal default template not applied: %q", got)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBuiltinDefaultProvidedAndOverridden(t *testing.T) {
	vault := t.TempDir()
	// With no user template, the builtin "default" (shipped in the binary) applies.
	if _, code := runIn(t, vault, "new", "--title", "Plain", "--id", "140"); code != 0 {
		t.Fatalf("new failed")
	}
	if got := readFileString(t, filepath.Join(vault, "note", "140.md")); got != "# Plain\n" {
		t.Fatalf("builtin default template not applied: %q", got)
	}
	// builtin templates are not written into the vault.
	if _, err := os.Stat(filepath.Join(vault, "builtin")); !os.IsNotExist(err) {
		t.Fatalf("builtin templates should not be materialized in the vault, stat err=%v", err)
	}

	// A user template of the same name overrides the builtin.
	if _, code := runIn(t, vault, "template", "new", "--name", "default", "--id", "950"); code != 0 {
		t.Fatalf("create user default template failed")
	}
	if err := os.WriteFile(
		filepath.Join(vault, "template", "950.template.md"),
		[]byte("<!-- track-template\nname: default\n-->\n# {{ title }}\n\nuser-default\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if _, code := runIn(t, vault, "new", "--title", "Custom", "--id", "141"); code != 0 {
		t.Fatalf("new failed")
	}
	if got := readFileString(t, filepath.Join(vault, "note", "141.md")); !strings.Contains(got, "user-default") {
		t.Fatalf("user template should override the builtin default, got %q", got)
	}
}

func TestToggleCheckbox(t *testing.T) {
	vault := t.TempDir()
	body := "# Tasks\n\n- [ ] first\n- [x] second\n- not a checkbox\n"
	if _, code := runInWithStdin(t, vault, body, "new", "--title", "Tasks", "--id", "500"); code != 0 {
		t.Fatalf("new failed")
	}
	path := filepath.Join(vault, "note", "500.md")

	// Toggle the unchecked item on; the JSON reports the new state and the file is rewritten.
	res, code := runIn(t, vault, "toggle", "--id", "500", "--line", "3")
	if code != 0 {
		t.Fatalf("toggle failed: %v", res)
	}
	if res["checked"] != true || res["changed"] != true {
		t.Fatalf("expected checked+changed, got %v", res)
	}
	if got := readFileString(t, path); !strings.Contains(got, "- [x] first") {
		t.Fatalf("first item should be checked: %q", got)
	}

	// --state uncheck is idempotent: re-running reports no change.
	if res, code := runIn(t, vault, "toggle", "--id", "500", "--line", "4", "--state", "uncheck"); code != 0 || res["checked"] != false {
		t.Fatalf("uncheck failed: %v", res)
	}
	again, _ := runIn(t, vault, "toggle", "--id", "500", "--line", "4", "--state", "uncheck")
	if again["checked"] != false || again["changed"] != false {
		t.Fatalf("uncheck should be idempotent, got %v", again)
	}

	// A non-checkbox line is rejected without touching the file.
	if out, code := runIn(t, vault, "toggle", "--id", "500", "--line", "5"); code == 0 || out["error"] == nil {
		t.Fatalf("expected error toggling a non-checkbox line, got %v", out)
	}
	if out, code := runIn(t, vault, "toggle", "--id", "500", "--line", "99"); code == 0 || out["error"] == nil {
		t.Fatalf("expected error for out-of-range line, got %v", out)
	}
}

func TestTaskSetAndTasks(t *testing.T) {
	vault := t.TempDir()
	body := "# Sprint [0/3]\n\n- [ ] alpha [#B] [due:2000-01-02]\n- [ ] beta [#A] [due:2999-12-31]\n- [ ] gamma\n"
	if _, code := runInWithStdin(t, vault, body, "new", "--title", "Board", "--id", "700"); code != 0 {
		t.Fatalf("new failed")
	}
	path := filepath.Join(vault, "note", "700.md")

	// Move alpha into DOING: no completion stamp, but the transition is logged in the sidecar.
	res, code := runIn(t, vault, "task", "set", "--id", "700", "--line", "3", "--state", "doing")
	if code != 0 {
		t.Fatalf("task set failed: %v", res)
	}
	if res["state"] != "DOING" || res["from"] != "TODO" || res["changed"] != true || res["done"] != false {
		t.Fatalf("unexpected task set result: %v", res)
	}
	if got := readFileString(t, path); !strings.Contains(got, "- [/] alpha [#B] [due:2000-01-02]") {
		t.Fatalf("state marker not rewritten: %q", got)
	}

	// Completing beta stamps [done:...] and recomputes the heading cookie.
	if res, code := runIn(t, vault, "task", "set", "--id", "700", "--line", "4", "--state", "DONE"); code != 0 || res["done"] != true {
		t.Fatalf("task set done failed: %v", res)
	}
	got := readFileString(t, path)
	if !strings.Contains(got, "- [x] beta [#A] [due:2999-12-31] [done:") {
		t.Fatalf("completion stamp missing: %q", got)
	}
	if !strings.Contains(got, "# Sprint [1/3]") {
		t.Fatalf("progress cookie not recomputed: %q", got)
	}
	sidecar := readFileString(t, filepath.Join(vault, ".track", "notes", "700.yaml"))
	if !strings.Contains(sidecar, "task_log:") || !strings.Contains(sidecar, "to: DONE") || !strings.Contains(sidecar, "to: DOING") {
		t.Fatalf("sidecar should log both transitions: %q", sidecar)
	}

	// An unknown state is rejected without touching the file.
	if out, code := runIn(t, vault, "task", "set", "--id", "700", "--line", "3", "--state", "bogus"); code == 0 || out["error"] == nil {
		t.Fatalf("expected unknown-state error, got %v", out)
	}

	// tasks lists everything; --state filters; --overdue keeps only the past-due open task; --sort
	// priority puts open [#B] alpha before unprioritized gamma and done beta last.
	list, code := runIn(t, vault, "tasks")
	if code != 0 {
		t.Fatalf("tasks failed: %v", list)
	}
	if all := list["tasks"].([]any); len(all) != 3 {
		t.Fatalf("expected 3 tasks, got %v", list)
	}
	list, _ = runIn(t, vault, "tasks", "--state", "DOING")
	if rows := list["tasks"].([]any); len(rows) != 1 || rows[0].(map[string]any)["text"] != "alpha" {
		t.Fatalf("state filter failed: %v", list)
	}
	list, _ = runIn(t, vault, "tasks", "--overdue")
	if rows := list["tasks"].([]any); len(rows) != 1 || rows[0].(map[string]any)["due"] != "2000-01-02" {
		t.Fatalf("overdue filter failed: %v", list)
	}
	list, _ = runIn(t, vault, "tasks", "--due", "2999-12-31")
	if rows := list["tasks"].([]any); len(rows) != 1 || rows[0].(map[string]any)["text"] != "alpha" {
		t.Fatalf("due filter should keep open tasks due by the date: %v", list)
	}
	list, _ = runIn(t, vault, "tasks", "--sort", "priority")
	rows := list["tasks"].([]any)
	if len(rows) != 3 || rows[0].(map[string]any)["text"] != "alpha" || rows[2].(map[string]any)["text"] != "beta" {
		t.Fatalf("priority sort failed: %v", list)
	}

	// Reopening beta clears the stamp.
	if res, code := runIn(t, vault, "task", "set", "--id", "700", "--line", "4", "--state", "TODO"); code != 0 || res["completed"] != "" {
		t.Fatalf("reopen should clear completion: %v", res)
	}
	if got := readFileString(t, path); strings.Contains(got, "[done:") || !strings.Contains(got, "# Sprint [0/3]") {
		t.Fatalf("stamp/cookie not reverted: %q", got)
	}
}

func TestTaskCycle(t *testing.T) {
	vault := t.TempDir()
	body := "# T\n\n- [ ] alpha\n\nprose\n"
	if _, code := runInWithStdin(t, vault, body, "new", "--title", "Cycle", "--id", "710"); code != 0 {
		t.Fatalf("new failed")
	}

	steps := []struct{ from, to string }{
		{"TODO", "DOING"},
		{"DOING", "WAITING"},
		{"WAITING", "DONE"},
		{"DONE", "CANCELLED"},
		{"CANCELLED", "TODO"},
	}
	for _, step := range steps {
		res, code := runIn(t, vault, "task", "cycle", "--id", "710", "--line", "3")
		if code != 0 || res["from"] != step.from || res["state"] != step.to {
			t.Fatalf("cycle %s→%s failed: %v", step.from, step.to, res)
		}
	}
	// Wrapping past the last state lands back on a clean open line, stamp removed.
	if got := readFileString(t, filepath.Join(vault, "note", "710.md")); !strings.Contains(got, "- [ ] alpha") || strings.Contains(got, "[done:") {
		t.Fatalf("wrap should return to a clean TODO line: %q", got)
	}

	if out, code := runIn(t, vault, "task", "cycle", "--id", "710", "--line", "5"); code == 0 || out["error"] == nil {
		t.Fatalf("expected error cycling a non-task line, got %v", out)
	}
}

func TestInitScaffoldsVault(t *testing.T) {
	vault := t.TempDir()
	res, code := runIn(t, vault, "init")
	if code != 0 {
		t.Fatalf("init failed: %v", res)
	}
	for _, rel := range []string{
		"note", "journal", "assets",
		"template", filepath.Join(".track", "notes"),
	} {
		if info, err := os.Stat(filepath.Join(vault, rel)); err != nil || !info.IsDir() {
			t.Fatalf("init should create %s: err=%v", rel, err)
		}
	}
}

func TestFirstLaunchAutoScaffolds(t *testing.T) {
	// A vault that does not exist yet is scaffolded the first time a command touches it.
	base := t.TempDir()
	vault := filepath.Join(base, "fresh-vault")
	if _, code := runIn(t, vault, "search", "--query", "anything"); code != 0 {
		t.Fatalf("search on a fresh vault should succeed")
	}
	if info, err := os.Stat(filepath.Join(vault, "assets")); err != nil || !info.IsDir() {
		t.Fatalf("first launch should have created the vault skeleton: err=%v", err)
	}
}

func TestAssetImportAndDir(t *testing.T) {
	vault := t.TempDir()
	src := filepath.Join(t.TempDir(), "Cover Image.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, code := runIn(t, vault, "asset", "import", "--file", src)
	if code != 0 {
		t.Fatalf("asset import failed: %v", res)
	}
	if res["ref"] != "assets/Cover-Image.png" {
		t.Fatalf("unexpected ref: %v", res)
	}
	stored := filepath.Join(vault, "assets", "Cover-Image.png")
	if got := readFileString(t, stored); got != "png" {
		t.Fatalf("asset not copied into assets/: %q", got)
	}

	// dir --ensure reports and creates the vault's assets directory. The vault path is canonicalized
	// (symlinks resolved), so assert on the suffix and that the reported dir now exists.
	dirRes, code := runIn(t, vault, "asset", "dir", "--ensure")
	if code != 0 {
		t.Fatalf("asset dir failed: %v", dirRes)
	}
	dir, _ := dirRes["dir"].(string)
	if !strings.HasSuffix(dir, filepath.Join(string(filepath.Separator), "assets")) {
		t.Fatalf("unexpected dir: %v", dirRes)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("assets dir should be created: err=%v", err)
	}
}

func TestMetaSetsAndClearsPageMetadata(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Meta", "--id", "100", "--body", "body")
	if err := os.MkdirAll(filepath.Join(vault, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "assets", "cover.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set both fields, trimming the description.
	out, code := runIn(t, vault, "meta", "--id", "100", "--description", "  a summary  ", "--image", "assets/cover.png")
	if code != 0 {
		t.Fatalf("meta set failed: %v", out)
	}
	if out["description"] != "a summary" || out["image"] != "assets/cover.png" || out["updated"] != true {
		t.Fatalf("unexpected meta output: %v", out)
	}

	// Read-only invocation reports the stored values.
	out, code = runIn(t, vault, "meta", "--id", "100")
	if code != 0 || out["description"] != "a summary" || out["image"] != "assets/cover.png" || out["updated"] != false {
		t.Fatalf("unexpected meta read: %v (code %d)", out, code)
	}

	// The sidecar carries the fields at version 4 and export --frontmatter surfaces them.
	meta, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"version: 4", "description: a summary", "image: assets/cover.png"} {
		if !strings.Contains(string(meta), want) {
			t.Fatalf("sidecar %q missing %q", meta, want)
		}
	}

	// An explicitly empty value clears one field without touching the other.
	out, code = runIn(t, vault, "meta", "--id", "100", "--image", "")
	if code != 0 || out["image"] != "" || out["description"] != "a summary" {
		t.Fatalf("clear image failed: %v (code %d)", out, code)
	}
}

func TestMetaRejectsBadImages(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Meta", "--id", "100", "--body", "body")
	if err := os.MkdirAll(filepath.Join(vault, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "assets", "cover.svg"), []byte("svg"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, image := range map[string]string{
		"missing file":   "assets/nope.png",
		"outside assets": "note/100.md",
		"traversal":      "assets/../secret.png",
		"non-raster":     "assets/cover.svg",
		"bare filename":  "cover.png",
	} {
		if _, code := runIn(t, vault, "meta", "--id", "100", "--image", image); code == 0 {
			t.Errorf("%s: image %q should be rejected", name, image)
		}
	}
}

func TestMetaSetAndUnsetProperties(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Props", "--id", "100", "--body", "body")

	out, code := runIn(t, vault, "meta", "--id", "100", "--set", "status=draft", "--set", "rating=8")
	if code != 0 || out["updated"] != true {
		t.Fatalf("meta set failed: %v (code %d)", out, code)
	}
	props, ok := out["props"].(map[string]any)
	if !ok || props["status"] != "draft" || props["rating"] != float64(8) {
		t.Fatalf("unexpected props: %v", out["props"])
	}

	// A read-only invocation reports the stored properties.
	out, code = runIn(t, vault, "meta", "--id", "100")
	if code != 0 {
		t.Fatalf("meta read failed: %v", out)
	}
	props, _ = out["props"].(map[string]any)
	if props["status"] != "draft" {
		t.Fatalf("props after read: %v", out["props"])
	}

	// The properties are indexed: the sidecar rows land in the props table.
	var count int
	db := openDB(t, vault)
	if err := db.QueryRow(`SELECT COUNT(*) FROM props WHERE note_id = 100`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("props rows = %d, want 2", count)
	}

	out, code = runIn(t, vault, "meta", "--id", "100", "--unset", "rating")
	if code != 0 {
		t.Fatalf("meta unset failed: %v", out)
	}
	props, _ = out["props"].(map[string]any)
	if _, still := props["rating"]; still {
		t.Fatalf("rating should be unset: %v", out["props"])
	}

	if out, code := runIn(t, vault, "meta", "--id", "100", "--set", "bad key=x"); code == 0 {
		t.Fatalf("invalid key should fail: %v", out)
	}
}

// TestMetaEditAppliesDocumentAndRenames drives the full editor round-trip through the CLI: the doc
// from `track meta` applies via --edit -, and a changed title goes through the rename path so
// backlinks are rewritten. A conflicting title rejects the whole document, changing nothing.
func TestMetaEditAppliesDocumentAndRenames(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Doc", "--id", "100", "--body", "body")
	runIn(t, vault, "new", "--title", "Source", "--id", "200", "--body", "see [[Doc]]\n")
	if rep, code := runIn(t, vault, "reindex", "--full"); code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}

	doc := "title: Doc v2\ntags:\n  - go\ndescription: from the editor\nprops:\n  status: draft\n"
	out, code := runInWithStdin(t, vault, doc, "meta", "--id", "100", "--edit", "-")
	if code != 0 {
		t.Fatalf("meta --edit failed: %v", out)
	}
	if out["title"] != "Doc v2" || out["description"] != "from the editor" {
		t.Fatalf("unexpected meta output: %v", out)
	}
	body, err := os.ReadFile(filepath.Join(vault, "note", "200.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "[[Doc v2]]") {
		t.Fatalf("backlink not rewritten: %q", body)
	}

	// A title already owned by another note rejects the document before anything is written.
	out, code = runInWithStdin(t, vault, "title: Source\ntags:\n  - later\n", "meta", "--id", "100", "--edit", "-")
	if code == 0 || !strings.Contains(out["error"].(string), "already in use") {
		t.Fatalf("expected title conflict, got code=%d out=%v", code, out)
	}
	out, code = runIn(t, vault, "meta", "--id", "100")
	if code != 0 || out["title"] != "Doc v2" {
		t.Fatalf("state after rejection: %v", out)
	}
	if tags, _ := out["tags"].([]any); len(tags) != 1 || tags[0] != "go" {
		t.Fatalf("tags must be untouched by the rejected doc: %v", out["tags"])
	}

	// --edit is whole-document: it cannot be combined with the point-edit flags.
	if out, code := runInWithStdin(t, vault, doc, "meta", "--id", "100", "--edit", "-", "--set", "x=1"); code == 0 {
		t.Fatalf("--edit with --set should fail: %v", out)
	}
}

// openDB opens the index database runIn's TRACK_CACHE_DIR produced for this vault.
func openDB(t *testing.T, vault string) *sql.DB {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(vault, ".test-cache", "*", "index.db"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("locate index.db: matches=%v err=%v", matches, err)
	}
	db, err := sql.Open("sqlite", matches[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
