package webui

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
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

func TestServesFrontendWithThemeInjectionAndAssets(t *testing.T) {
	// Inject a controllable frontend as the served webRoot (the handlers read s.webRoot at request
	// time), so this exercises theme-placeholder injection, immutable asset caching, and the JSON 404
	// for a missing asset without depending on any on-disk build.
	index := `<!doctype html><html><head><script>window.theme="__TRACK_DEFAULT_THEME__"</script>__TRACK_COLOR_OVERRIDES__<script type="module" src="/assets/app.js"></script></head><body><div id="root">local app</div></body></html>`
	srv := New(&config.Config{WebTheme: "dark"}, nil)
	srv.webRoot = fstest.MapFS{
		"index.html":    {Data: []byte(index)},
		"assets/app.js": {Data: []byte(`console.log("local app")`)},
	}
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "local app") || !strings.Contains(string(body), `window.theme="dark"`) {
		t.Fatalf("index did not come from local web/dist with injected theme: %s", body)
	}
	if strings.Contains(string(body), "__TRACK_DEFAULT_THEME__") || strings.Contains(string(body), "__TRACK_COLOR_OVERRIDES__") {
		t.Fatalf("index still contains placeholders: %s", body)
	}

	asset, err := http.Get(server.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer asset.Body.Close()
	assetBody, _ := io.ReadAll(asset.Body)
	if asset.StatusCode != http.StatusOK || string(assetBody) != `console.log("local app")` {
		t.Fatalf("asset response = %d %q", asset.StatusCode, assetBody)
	}
	if got := asset.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("asset cache-control = %q, want immutable", got)
	}

	missingAsset, err := http.Get(server.URL + "/assets/missing.js")
	if err != nil {
		t.Fatal(err)
	}
	defer missingAsset.Body.Close()
	if missingAsset.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(missingAsset.Body)
		t.Fatalf("missing asset status = %d, want 404; body = %q", missingAsset.StatusCode, body)
	}
	if ct := missingAsset.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("missing asset content-type = %q, want JSON error rather than SPA HTML", ct)
	}
}

// track serves only the embedded frontend. A web/dist in the working directory must never shadow it —
// so a Nix binary run from the repo root can't silently serve a stale hand-built dist. (Frontend
// iteration goes through the Vite dev server, which proxies /api instead.)
func TestServesEmbeddedFrontendIgnoringLocalWebDist(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	dist := filepath.Join(cwd, "web", "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A complete-looking local build: index.html plus the asset it references.
	index := `<!doctype html><script type="module" src="/assets/app.js"></script>`
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("// local"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(&config.Config{}, nil)
	raw, err := fs.ReadFile(srv.webRoot, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/assets/app.js") {
		t.Fatalf("a local web/dist must not replace the embedded frontend")
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

	// The notes listing carries each note's activity days, which the calendar derives its per-day note
	// lists from.
	listing := getJSON(t, server.URL+"/api/notes")
	refs := listing["notes"].([]any)
	if len(refs) != 1 {
		t.Fatalf("expected 1 listed note, got %v", refs)
	}
	// The self-heal on read also stamps the file's mtime day, so assert the seeded day is present rather
	// than the exact set.
	days, _ := refs[0].(map[string]any)["days"].([]any)
	if !slices.Contains(days, any("2026-06-22")) {
		t.Fatalf("notes listing should carry the seeded activity day, got %v", refs[0])
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

// TestWatchBroadcastsDataChanges guards the live-chart refresh path: a write under the vault's data/
// directory (the JSONL files View Spec data.source / overlays[].source read) must reach SSE subscribers
// as a `data` event, and the directory must be created and watched even when the vault starts without
// it. Data files are not indexed, so this event is distinct from the reindex-backed `change` event.
func TestWatchBroadcastsDataChanges(t *testing.T) {
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

	// The vault has no data/ yet; startWatch must create and watch it.
	server := New(cfg, s)
	server.startWatch()
	ch := server.events.subscribe()
	t.Cleanup(func() { server.events.unsubscribe(ch) })

	if err := os.WriteFile(filepath.Join(cfg.DataDir(), "metrics.jsonl"), []byte("{\"time\":\"2026-07-05\",\"value\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-ch:
		if ev.name != "data" {
			t.Fatalf("event = %q, want %q", ev.name, "data")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no data event after writing a data file")
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
// the vault's assets directory, not swallowed by the SPA index fallback, and traversal out of that
// directory must be rejected.
func TestAssetServesVaultFile(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}}
	if err := os.MkdirAll(cfg.AssetsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.4 fake pdf bytes")
	if err := os.WriteFile(filepath.Join(cfg.AssetsDir(), "report.pdf"), pdf, 0o644); err != nil {
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

func deleteNote(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete %s: %v", url, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func TestDeleteNoteRemovesFileSidecarAndIndex(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Alpha", "body\n")

	code, resp := deleteNote(t, server.URL+"/api/note?id=100")
	if code != http.StatusOK || resp["deleted"] != true {
		t.Fatalf("delete status=%d resp=%v", code, resp)
	}
	if _, err := os.Stat(cfg.NotePath(100)); !os.IsNotExist(err) {
		t.Fatalf("note file should be gone, stat err=%v", err)
	}
	if _, err := os.Stat(cfg.MetadataPath(100)); !os.IsNotExist(err) {
		t.Fatalf("sidecar should be gone, stat err=%v", err)
	}
	// The note is no longer served, and a second delete reports it missing.
	getResp, err := http.Get(server.URL + "/api/note?id=100")
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted note should 404, got %d", getResp.StatusCode)
	}
	if code, _ := deleteNote(t, server.URL+"/api/note?id=100"); code != http.StatusNotFound {
		t.Fatalf("deleting a missing note should 404, got %d", code)
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

func TestRenderExpandsQueryBlocks(t *testing.T) {
	server, _ := putNoteSetup(t, 100, "Alpha", "status:: open\n")

	resp, err := http.Post(server.URL+"/api/render", "application/json",
		strings.NewReader(`{"body":"before\n\n`+"```track-query\\nTABLE title WHERE props.status = open\\n```"+`\n"}`))
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
	markdown, _ := decoded["markdown"].(string)
	if strings.Contains(markdown, "```track-query") || !strings.Contains(markdown, "| [[Alpha]] |") {
		t.Fatalf("query fence should expand to a result table, got %q", markdown)
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

// TestViewSpecReturnsEChartsOption verifies the embedded-chart endpoint: a valid spec comes back as
// an ECharts option, and a broken spec is a 400 whose message the frontend shows at the block position.
func TestViewSpecReturnsEChartsOption(t *testing.T) {
	server, _ := putNoteSetup(t, 100, "Alpha", "body\n")

	spec := `{"version":2,"mark":"bar","title":"Demo","data":{"kind":"metric","records":[
		{"name":"A","time":"t1","value":3}]},"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}`
	body, _ := json.Marshal(map[string]string{"spec": spec})
	resp, err := http.Post(server.URL+"/api/viewspec", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post viewspec: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewspec status = %d", resp.StatusCode)
	}
	var decoded struct {
		ECharts json.RawMessage `json:"echarts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	opt := string(decoded.ECharts)
	if !strings.Contains(opt, `"type":"bar"`) || !strings.Contains(opt, `"text":"Demo"`) {
		t.Fatalf("expected an ECharts option, got %.120s", opt)
	}

	bad, _ := json.Marshal(map[string]string{"spec": `{"version":2,"mark":"pie"}`})
	resp2, err := http.Post(server.URL+"/api/viewspec", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatalf("post bad viewspec: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad spec status = %d, want 400", resp2.StatusCode)
	}
}

func TestNoteMetaEndpoint(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Alpha", "Body.\n")

	if err := os.MkdirAll(cfg.AssetsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.AssetsDir(), "cover.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	post := func(payload map[string]any) (*http.Response, map[string]any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		resp, err := http.Post(server.URL+"/api/note/meta?id=100", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var decoded map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&decoded)
		return resp, decoded
	}

	// GET seeds every typed field; the note has a title, so it comes back verbatim.
	seed := getJSON(t, server.URL+"/api/note/meta?id=100")
	if seed["title"] != "Alpha" {
		t.Fatalf("seed title = %v", seed["title"])
	}
	for _, key := range []string{"title", "kind", "tags", "description", "image", "props"} {
		if _, ok := seed[key]; !ok {
			t.Fatalf("seed missing field %q: %v", key, seed)
		}
	}
	// The kind lets the dialog disable title editing for journals; a plain note reports "note".
	if seed["kind"] != "note" {
		t.Fatalf("seed kind = %v, want note", seed["kind"])
	}

	// A structured edit applies through the engine's validated write path; the response echoes the
	// stored fields, props rendered back as a YAML block, tags deduped.
	resp, res := post(map[string]any{
		"title":       "Alpha",
		"tags":        []string{"go", "go"},
		"description": "a summary",
		"image":       "assets/cover.png",
		"props":       "status: draft\n",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("meta save status = %d: %v", resp.StatusCode, res)
	}
	stored := getJSON(t, server.URL+"/api/note/meta?id=100")
	if stored["description"] != "a summary" || stored["image"] != "assets/cover.png" {
		t.Fatalf("stored description/image = %v / %v", stored["description"], stored["image"])
	}
	if tags, _ := stored["tags"].([]any); len(tags) != 1 || tags[0] != "go" {
		t.Fatalf("tags should dedup to [go]: %v", stored["tags"])
	}
	if props, _ := stored["props"].(string); !strings.Contains(props, "status: draft") {
		t.Fatalf("stored props missing status: %q", props)
	}

	// A bad image is a 400 carrying the engine's message, and changes nothing.
	resp, _ = post(map[string]any{"image": "assets/nope.png"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad image should be a 400, got %d", resp.StatusCode)
	}
	unchanged := getJSON(t, server.URL+"/api/note/meta?id=100")
	if unchanged["description"] != "a summary" {
		t.Fatalf("rejected edit must change nothing: %v", unchanged)
	}

	// A props block that is not a map (a bare list) is rejected server-side.
	resp, _ = post(map[string]any{"props": "- just\n- a list\n"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-map props should be a 400, got %d", resp.StatusCode)
	}

	// A changed title routes through the rename path.
	resp, res = post(map[string]any{"title": "Alpha v2", "tags": []string{"go"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("title change failed: %d %v", resp.StatusCode, res)
	}
	noteRes := getJSON(t, server.URL+"/api/note?id=100")["note"].(map[string]any)
	if noteRes["title"] != "Alpha v2" {
		t.Fatalf("note title after rename = %v", noteRes["title"])
	}
}

func TestAssetUploadImportsImage(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	upload := func(filename string, data []byte) (*http.Response, map[string]any) {
		t.Helper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
		mw.Close()
		resp, err := http.Post(server.URL+"/api/asset", mw.FormDataContentType(), &buf)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var decoded map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&decoded)
		return resp, decoded
	}

	// A valid image import lands under assets/ and returns its assets/<name> reference.
	resp, res := upload("cover.png", []byte("\x89PNG fake image bytes"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d: %v", resp.StatusCode, res)
	}
	if res["ref"] != "assets/cover.png" {
		t.Fatalf("ref = %v, want assets/cover.png", res["ref"])
	}
	if _, err := os.Stat(filepath.Join(cfg.AssetsDir(), "cover.png")); err != nil {
		t.Fatalf("uploaded file missing from assets: %v", err)
	}

	// A non-image is rejected and leaves nothing behind (the cover-image gate, reused).
	resp, _ = upload("notes.txt", []byte("hello"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-image should be a 400, got %d", resp.StatusCode)
	}
	if entries, _ := os.ReadDir(cfg.AssetsDir()); len(entries) != 1 {
		t.Fatalf("non-image upload must not persist; assets = %v", entries)
	}

	// A path-escaping filename cannot write outside assets/: it is reduced to its base name.
	resp, res = upload("../../escape.png", []byte("png"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sanitized upload status = %d: %v", resp.StatusCode, res)
	}
	if res["ref"] != "assets/escape.png" {
		t.Fatalf("escape ref = %v, want assets/escape.png (base name)", res["ref"])
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.AssetsDir()), "escape.png")); !os.IsNotExist(err) {
		t.Fatalf("escape upload must not write outside assets/ (stat err=%v)", err)
	}

	// An oversized upload is rejected by the body cap.
	resp, _ = upload("big.png", make([]byte, maxAssetUploadBytes+1))
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("oversized upload should be rejected, got 200")
	}
}
