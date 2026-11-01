package webui

import (
	"database/sql"
	"encoding/json"
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestColdStartupIsolatesSidecarAndRefusesUncertainCreation(t *testing.T) {
	vault := t.TempDir()
	writeVaultNote(t, vault, 1, "Healthy", "Readable body")
	writeVaultNote(t, vault, 2, "Hidden", "Unavailable metadata")
	configPath := filepath.Join(t.TempDir(), "machine.yml")
	if err := os.WriteFile(configPath, []byte("vault_dir: "+vault+"\ncache_dir: "+t.TempDir()+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".track", "config.yml"), []byte("link_format: id-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Malformed and unreadable files follow the same per-item startup boundary.
	if err := os.WriteFile(filepath.Join(vault, ".track", "notes", "2.yaml"), []byte("version: 1\ntitle: [\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_CACHE_DIR", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("sidecar blocked startup: %v", err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(cfg, st)
	read := partialRequest(s, "GET", "/api/note?id=1", "")
	if read.Code != 200 || !strings.Contains(read.Body.String(), "Readable body") || !strings.Contains(read.Body.String(), `"partial":true`) {
		t.Fatalf("cold healthy read: %d %s", read.Code, read.Body.String())
	}
	beforeMeta, err := os.ReadFile(cfg.MetadataPath(1))
	if err != nil {
		t.Fatal(err)
	}
	edit := partialRequest(s, "POST", "/api/note/meta?id=1", `{"title":"Hidden","tags":["must-not-save"]}`)
	afterMeta, err := os.ReadFile(cfg.MetadataPath(1))
	if err != nil {
		t.Fatal(err)
	}
	if edit.Code != 409 || string(beforeMeta) != string(afterMeta) {
		t.Fatalf("rejected rename partly saved metadata: %d %s", edit.Code, edit.Body.String())
	}
	before, err := os.ReadDir(cfg.NoteDir())
	if err != nil {
		t.Fatal(err)
	}
	create := partialRequest(s, "POST", "/api/note", `{"title":"Hidden"}`)
	if create.Code != 409 {
		t.Fatalf("uncertain namespace allowed creation: %d %s", create.Code, create.Body.String())
	}
	after, err := os.ReadDir(cfg.NoteDir())
	if err != nil || len(after) != len(before) {
		t.Fatalf("creation wrote files: %v", err)
	}
	// A rejected dependent mutation does not poison subsequent independent reads.
	again := partialRequest(s, "GET", "/api/search?q=Healthy", "")
	if again.Code != 200 || !strings.Contains(again.Body.String(), "Healthy") {
		t.Fatalf("later search failed: %d %s", again.Code, again.Body.String())
	}
}

func TestOptionalIndexPanelFailureRetainsVerifiedBodyReadOnly(t *testing.T) {
	s, cfg := partialTestServer(t)
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE links"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_read_diagnostics BEFORE UPDATE ON index_state WHEN NEW.key='read_state' BEGIN SELECT RAISE(FAIL,'synthetic diagnostics writer unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	response := partialRequest(s, "GET", "/api/note?id=1", "")
	var body struct {
		Note struct {
			Body     string `json:"body"`
			ReadOnly bool   `json:"read_only"`
		} `json:"note"`
		State store.ReadState `json:"read_state"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || body.Note.Body != "xy needle" || !body.Note.ReadOnly || !body.State.Partial {
		t.Fatalf("optional failure collapsed body: %d %s", response.Code, response.Body.String())
	}
	search := partialRequest(s, "GET", "/api/search?q=Synthetic", "")
	if search.Code != 200 {
		t.Fatalf("subsequent independent search failed: %d %s", search.Code, search.Body.String())
	}
}
