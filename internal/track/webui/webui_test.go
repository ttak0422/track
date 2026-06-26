package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func TestAPIHandlers(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(100), []byte("# Alpha\n\nSee [[Beta]].\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(200), []byte("# Beta\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sidecars carry the authoritative titles/tags, and the file mtimes are pinned to the indexed values
	// below, so the server's read-time freshness check sees the index as already in sync with disk.
	if err := note.WriteMetadata(cfg.MetadataPath(100), note.Metadata{Title: "Alpha", Tags: []string{"project"}}); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(200), note.Metadata{Title: "Beta", Tags: []string{"draft"}}); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Now().Unix()
	if err := s.UpsertNote(&note.Note{ID: 100, Mtime: now, Meta: note.Metadata{Title: "Alpha", Tags: []string{"project"}, Days: []string{"2026-06-15"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNote(&note.Note{ID: 200, Mtime: now - 86400, Meta: note.Metadata{Title: "Beta", Tags: []string{"draft"}, Days: []string{"2026-06-15"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cfg.NotePath(100), time.Unix(now, 0), time.Unix(now, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cfg.NotePath(200), time.Unix(now-86400, 0), time.Unix(now-86400, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceLinks(100, []int64{200}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	search := getJSON(t, server.URL+"/api/search?q=Alpha")
	results := search["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["title"] != "Alpha" {
		t.Fatalf("unexpected search results: %v", results)
	}
	tags := results[0].(map[string]any)["tags"].([]any)
	if len(tags) != 1 || tags[0] != "project" {
		t.Fatalf("unexpected search result tags: %v", results[0])
	}
	emptySearch := getJSON(t, server.URL+"/api/search?q="+url.QueryEscape("検索"))
	emptyResults, ok := emptySearch["results"].([]any)
	if !ok || len(emptyResults) != 0 {
		t.Fatalf("search miss should return an empty array, got %T %v", emptySearch["results"], emptySearch["results"])
	}
	resolved := getJSON(t, server.URL+"/api/resolve?term=Beta")
	if resolved["found"] != true || resolved["note"].(map[string]any)["note_id"].(float64) != 200 {
		t.Fatalf("unexpected resolve result: %v", resolved)
	}

	noteResp := getJSON(t, server.URL+"/api/note?id=200")
	noteBody := noteResp["note"].(map[string]any)
	noteTags := noteBody["tags"].([]any)
	if noteBody["title"] != "Beta" || len(noteTags) != 1 || noteTags[0] != "draft" {
		t.Fatalf("unexpected note response: %v", noteBody)
	}
	if cp, _ := noteBody["copy_path"].(string); cp == "" {
		t.Fatalf("note response should carry a copy_path: %v", noteBody)
	}

	graph := getJSON(t, server.URL+"/api/graph/local?id=100")["graph"].(map[string]any)
	if len(graph["nodes"].([]any)) != 2 || len(graph["edges"].([]any)) != 1 {
		t.Fatalf("unexpected graph: %v", graph)
	}

	full := getJSON(t, server.URL+"/api/graph")["graph"].(map[string]any)
	if len(full["nodes"].([]any)) != 2 || len(full["edges"].([]any)) != 1 {
		t.Fatalf("unexpected full graph: %v", full)
	}
	if full["center_id"].(float64) != 0 {
		t.Fatalf("full graph should have no center, got %v", full["center_id"])
	}
	firstNode := full["nodes"].([]any)[0].(map[string]any)
	if firstNode["path"] == nil || firstNode["path"] == "" {
		t.Fatalf("full graph node should carry a resolved path: %v", firstNode)
	}

	activity := getJSON(t, server.URL+"/api/activity?since=2026-06-15&until=2026-06-15")["activity"].(map[string]any)
	if activity["since"] != "2026-06-15" || activity["until"] != "2026-06-15" || activity["total"].(float64) != 2 {
		t.Fatalf("unexpected activity response: %v", activity)
	}
}

func TestAgendaEndpoint(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// A note whose sidecar records an activity day. The server self-heals on read, indexing the on-disk
	// note and its day, so the agenda endpoint can surface it.
	if err := os.WriteFile(cfg.NotePath(100), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(100), note.Metadata{Title: "Worked", Days: []string{"2026-06-22"}}); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	agenda := getJSON(t, server.URL+"/api/agenda?date=2026-06-22")
	if agenda["date"] != "2026-06-22" {
		t.Fatalf("agenda echoed date = %v, want 2026-06-22", agenda["date"])
	}
	notes := agenda["notes"].([]any)
	if len(notes) != 1 || notes[0].(map[string]any)["title"] != "Worked" {
		t.Fatalf("unexpected agenda notes: %v", notes)
	}

	empty := getJSON(t, server.URL+"/api/agenda?date=2020-01-01")
	if list, _ := empty["notes"].([]any); len(list) != 0 {
		t.Fatalf("agenda for empty day should be empty: %v", empty["notes"])
	}
}

func TestFollowEndpointStoresNeovimState(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	addIndexedTestNote(t, cfg, s, 100, "Tracked")

	srv := New(cfg, s)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	initial := getJSON(t, server.URL+"/api/follow")
	if initial["active"] != false {
		t.Fatalf("initial follow state should be inactive: %v", initial)
	}

	resp, err := http.Post(server.URL+"/api/follow", "application/json", strings.NewReader(`{
		"note_id": 100,
		"file_kind": "note",
		"line": 12,
		"top_line": 8,
		"line_count": 40
	}`))
	if err != nil {
		t.Fatalf("post follow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post follow status = %d", resp.StatusCode)
	}

	current := getJSON(t, server.URL+"/api/follow")
	if current["active"] != true {
		t.Fatalf("follow should be active after POST: %v", current)
	}
	state := current["state"].(map[string]any)
	if state["note_id"].(float64) != 100 || state["file_kind"] != "note" || state["top_line"].(float64) != 8 {
		t.Fatalf("unexpected follow state: %v", state)
	}
	if state["path"] != cfg.NotePath(100) {
		t.Fatalf("follow path should be derived from config, got %v want %s", state["path"], cfg.NotePath(100))
	}
	if state["updated_at"] == "" {
		t.Fatalf("follow state should include updated_at: %v", state)
	}

	srv.followMu.Lock()
	srv.follow.UpdatedAt = time.Now().Add(-followStateTTL - time.Second).Format(time.RFC3339Nano)
	srv.followMu.Unlock()
	stale := getJSON(t, server.URL+"/api/follow")
	if stale["active"] != false {
		t.Fatalf("stale follow state should be inactive: %v", stale)
	}
}

func TestFollowEndpointRejectsUnknownNote(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Post(server.URL+"/api/follow", "application/json", strings.NewReader(`{
		"note_id": 999,
		"file_kind": "note",
		"line": 1,
		"top_line": 1,
		"line_count": 20
	}`))
	if err != nil {
		t.Fatalf("post follow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("post follow status = %d, want 404: %s", resp.StatusCode, body)
	}
}

func addIndexedTestNote(t *testing.T, cfg *config.Config, s *store.Store, id int64, title string) {
	t.Helper()
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := cfg.NotePath(id)
	if err := os.WriteFile(path, []byte("# "+title+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := s.UpsertNote(&note.Note{ID: id, Kind: config.KindNote, Mtime: now, Meta: note.Metadata{Title: title}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Unix(now, 0), time.Unix(now, 0)); err != nil {
		t.Fatal(err)
	}
}

// TestWatcherReconcileStampsEditDay guards against the watcher swallowing a note's edit-day activity.
// The watcher must reconcile through RefreshIfStale (which stamps each changed note's mtime day into its
// sidecar), not a bare Full() that only syncs mtimes. Otherwise an edited note never surfaces under "on
// this day" for the day it was edited — it stays pinned to its creation day.
func TestWatcherReconcileStampsEditDay(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// A note created on an earlier day, with its mtime pinned to that day so the first reconcile records
	// only the creation day.
	created := time.Date(2026, 6, 20, 9, 0, 0, 0, time.Local)
	if err := os.WriteFile(cfg.NotePath(400), []byte("# Delta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(400), note.Metadata{Title: "Delta", Created: "2026-06-20", Days: []string{"2026-06-20"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cfg.NotePath(400), created, created); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	server := New(cfg, s)
	server.reconcileAfterChange() // initial index, records the creation day

	// The note is edited later: bump its body and mtime to a new day, then run the same reconcile the
	// watcher fires on a filesystem event.
	editDay := time.Date(2026, 6, 25, 14, 0, 0, 0, time.Local)
	if err := os.WriteFile(cfg.NotePath(400), []byte("# Delta edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cfg.NotePath(400), editDay, editDay); err != nil {
		t.Fatal(err)
	}
	server.reconcileAfterChange()

	// The edit day must be stamped into the sidecar...
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(400))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(meta.Days, "2026-06-25") {
		t.Fatalf("watcher reconcile did not stamp edit day; Days = %v", meta.Days)
	}
	// ...so the note surfaces under "on this day" for the day it was edited.
	notes, err := s.NotesOnDay("2026-06-25")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Title != "Delta" {
		t.Fatalf("edited note missing from agenda for edit day: %+v", notes)
	}
}

func TestJournalEndpointCreatesAndReopens(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	created := postJSON(t, server.URL+"/api/journal?date=2026-06-22")
	if created["created"] != true || created["note_id"].(float64) != 20260622 {
		t.Fatalf("unexpected journal create response: %v", created)
	}
	if _, err := os.Stat(cfg.JournalPath("20260622")); err != nil {
		t.Fatalf("journal file not created: %v", err)
	}

	// Clicking the same day again is idempotent: it reopens rather than recreating.
	reopened := postJSON(t, server.URL+"/api/journal?date=2026-06-22")
	if reopened["created"] != false || reopened["note_id"].(float64) != 20260622 {
		t.Fatalf("unexpected journal reopen response: %v", reopened)
	}
}

// TestReadReflectsExternalChange covers a note that appears on disk without going through this server
// (another editor's CLI, or a cloud sync that raised no filesystem event). The read-time freshness
// check must reconcile the index so the note shows up without an explicit reindex.
func TestReadReflectsExternalChange(t *testing.T) {
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	if err := os.WriteFile(cfg.NotePath(300), []byte("# Gamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(300), note.Metadata{Title: "Gamma"}); err != nil {
		t.Fatal(err)
	}

	search := getJSON(t, server.URL+"/api/search?q=Gamma")
	results := search["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["title"] != "Gamma" {
		t.Fatalf("external note not reflected in search: %v", results)
	}
}

// TestAssetServesVaultFile covers /api/asset: a note's "assets/<file>" attachment must be served from
// the vault's per-kind assets directory, not swallowed by the SPA index fallback, and traversal out of
// that directory must be rejected.
func TestAssetServesVaultFile(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}}
	if err := os.MkdirAll(cfg.AssetsDirForKind(config.KindNote), 0o755); err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.4 fake pdf bytes")
	if err := os.WriteFile(filepath.Join(cfg.AssetsDirForKind(config.KindNote), "report.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/api/asset?kind=note&name=report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
		t.Fatalf("asset content-type = %q, want application/pdf", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(pdf) {
		t.Fatalf("asset body = %q, want %q", string(body), string(pdf))
	}

	missing, err := http.Get(server.URL + "/api/asset?kind=note&name=nope.pdf")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missing.StatusCode)
	}

	// A traversal attempt must never reach a file outside the assets directory. The leading-slash clean
	// neutralizes "../" so the path stays inside assets/ and simply misses (404); it must not serve the
	// secret placed at the vault root.
	if err := os.WriteFile(filepath.Join(cfg.VaultDir, "secret.md"), []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	traversal, err := http.Get(server.URL + "/api/asset?kind=note&name=" + url.QueryEscape("../../secret.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer traversal.Body.Close()
	if traversal.StatusCode == http.StatusOK {
		leaked, _ := io.ReadAll(traversal.Body)
		t.Fatalf("traversal must not serve files outside assets/, got 200 with body %q", string(leaked))
	}
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s status = %d", url, resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return decoded
}

func postJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post %s status = %d", url, resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return decoded
}

// putNoteSetup builds a server with one indexed note (id, title) whose markdown and sidecar exist on
// disk, ready for save/conflict tests.
func putNoteSetup(t *testing.T, id int64, title, body string) (*httptest.Server, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		VaultDir:          t.TempDir(),
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertNote(&note.Note{ID: id, Path: cfg.NotePath(id), Meta: note.Metadata{Title: title}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)
	return server, cfg
}

func putNote(t *testing.T, url, jsonBody string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", url, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func TestPutNoteSavesBodyWithEtag(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Alpha", "old body\n")

	got := getJSON(t, server.URL+"/api/note?id=100")["note"].(map[string]any)
	etag, _ := got["etag"].(string)
	if etag == "" {
		t.Fatalf("GET should return an etag, got %v", got)
	}

	body := `{"body":"new body line\n","etag":"` + etag + `"}`
	code, resp := putNote(t, server.URL+"/api/note?id=100", body)
	if code != http.StatusOK || resp["saved"] != true {
		t.Fatalf("put status=%d resp=%v", code, resp)
	}
	if resp["etag"] == etag {
		t.Fatalf("etag should change after save")
	}
	raw, err := os.ReadFile(cfg.NotePath(100))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new body line\n" {
		t.Fatalf("file body = %q, want %q", string(raw), "new body line\n")
	}
}

func TestPutNoteRejectsStaleEtag(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Alpha", "old body\n")

	code, resp := putNote(t, server.URL+"/api/note?id=100", `{"body":"x\n","etag":"deadbeef"}`)
	if code != http.StatusConflict {
		t.Fatalf("stale etag should be 409, got %d (%v)", code, resp)
	}
	raw, _ := os.ReadFile(cfg.NotePath(100))
	if string(raw) != "old body\n" {
		t.Fatalf("file must be unchanged on conflict, got %q", string(raw))
	}
}

func TestPutNoteRequiresEtag(t *testing.T) {
	server, _ := putNoteSetup(t, 100, "Alpha", "old body\n")
	code, _ := putNote(t, server.URL+"/api/note?id=100", `{"body":"x\n"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("missing etag should be 400, got %d", code)
	}
}

func TestRenderSanitizesActionLinksKeepsWiki(t *testing.T) {
	server, _ := putNoteSetup(t, 100, "Alpha", "old body\n")

	resp, err := http.Post(server.URL+"/api/render", "application/json",
		strings.NewReader(`{"body":"see [[Go]] and [今日](<journal?offset=0>) here\n"}`))
	if err != nil {
		t.Fatalf("post render: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d", resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode render: %v", err)
	}
	if got, want := decoded["markdown"], "see [[Go]] and 今日 here\n"; got != want {
		t.Fatalf("render markdown = %q, want %q", got, want)
	}
}

func TestIndexInjectsConfiguredTheme(t *testing.T) {
	cfg := &config.Config{
		VaultDir:   t.TempDir(),
		DBPath:     filepath.Join(t.TempDir(), "index.db"),
		Extensions: []string{".md"},
		WebTheme:   "dark",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	html := string(bodyBytes)
	if !strings.Contains(html, `var serverDefault = "dark"`) {
		t.Fatalf("served HTML should inject the dark default theme")
	}
	if strings.Contains(html, "__TRACK_DEFAULT_THEME__") {
		t.Fatalf("placeholder should be replaced")
	}
	if !strings.Contains(html, `id="root"`) {
		t.Fatalf("served HTML should include the React mount point")
	}
}

// TestAppServesSPAFallback verifies that a client-side route that is not a real file (e.g. /notes/123)
// is served the frontend index so the router can handle the deep link instead of returning 404.
func TestAppServesSPAFallback(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/notes/123")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deep link should fall back to index, got status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("deep link should serve HTML, got content-type %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatalf("deep link fallback should serve the frontend index")
	}
}

func TestIndexInjectsPaletteOverrides(t *testing.T) {
	palettePath := filepath.Join(t.TempDir(), "colors.yml")
	if err := os.WriteFile(palettePath, []byte("dark:\n  accent: \"#123456\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		VaultDir:      t.TempDir(),
		DBPath:        filepath.Join(t.TempDir(), "index.db"),
		Extensions:    []string{".md"},
		WebColorsPath: palettePath,
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, `id="track-colors"`) || !strings.Contains(html, "--accent:#123456;") {
		t.Fatalf("served HTML should inject palette overrides, got:\n%s", html)
	}
}

func TestIndexNoPaletteRemovesPlaceholder(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if strings.Contains(html, "__TRACK_COLOR_OVERRIDES__") {
		t.Fatalf("color placeholder should be replaced even with no palette")
	}
	if strings.Contains(html, `id="track-colors"`) {
		t.Fatalf("no palette should mean no override style block")
	}
}
