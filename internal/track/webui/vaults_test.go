package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// writeVaultNote lays down one note and its authoritative sidecar in a vault directory.
func writeVaultNote(t *testing.T, vault string, id int64, title, body string) {
	t.Helper()
	cfg := &config.Config{VaultDir: vault, Extensions: []string{".md"}}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
}

// twoVaultServer starts a workspace whose launch vault is registered as "main" and which also
// serves a second registered vault "work". Both hold a note under the same id, which is the case
// that matters: ids are vault-local, and journal ids collide across vaults outright.
func twoVaultServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	main, work := t.TempDir(), t.TempDir()
	writeVaultNote(t, main, 100, "Alpha in main", "# Alpha in main\n")
	writeVaultNote(t, work, 100, "Alpha in work", "# Alpha in work\n")

	configPath := filepath.Join(t.TempDir(), "config.yml")
	body := "cache_dir: " + t.TempDir() + "\nvaults:\n  main: " + main + "\n  work: " + work + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", main)
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	srv := New(cfg, s)
	t.Cleanup(srv.closeViews)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)
	return server, main, work
}

func getVaultJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return out
}

func TestVaultParamSelectsTheAddressedVault(t *testing.T) {
	server, _, _ := twoVaultServer(t)

	// The same id names a different note in each vault; the ?vault= parameter is what decides.
	active := getVaultJSON(t, server.URL+"/api/note?id=100")["note"].(map[string]any)
	if active["title"] != "Alpha in main" {
		t.Fatalf("unqualified request must read the launch vault, got %v", active["title"])
	}
	if active["vault"] != "main" {
		t.Fatalf("response must name its vault, got %v", active["vault"])
	}
	other := getVaultJSON(t, server.URL+"/api/note?id=100&vault=work")["note"].(map[string]any)
	if other["title"] != "Alpha in work" {
		t.Fatalf("?vault=work must read the other vault, got %v", other["title"])
	}
	if other["vault"] != "work" {
		t.Fatalf("response must name its vault, got %v", other["vault"])
	}
}

func TestUnknownVaultIsRefused(t *testing.T) {
	server, _, _ := twoVaultServer(t)

	// A typo must fail loudly rather than fall back to the launch vault, where it would read — and
	// on a write, modify — a same-numbered note that belongs to someone else.
	resp, err := http.Get(server.URL + "/api/note?id=100&vault=typo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown vault should be refused, got status %d", resp.StatusCode)
	}
}

func TestDeleteHitsOnlyTheAddressedVault(t *testing.T) {
	server, main, work := twoVaultServer(t)

	// Read both first so each vault's index is built, then delete the one in "work".
	getVaultJSON(t, server.URL+"/api/note?id=100")
	getVaultJSON(t, server.URL+"/api/note?id=100&vault=work")

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/note?id=100&vault=work", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete in the addressed vault should succeed, got %d", resp.StatusCode)
	}

	workCfg := &config.Config{VaultDir: work, Extensions: []string{".md"}}
	if _, err := os.Stat(workCfg.NotePath(100)); !os.IsNotExist(err) {
		t.Fatalf("note 100 should be gone from the addressed vault, stat err=%v", err)
	}
	mainCfg := &config.Config{VaultDir: main, Extensions: []string{".md"}}
	if _, err := os.Stat(mainCfg.NotePath(100)); err != nil {
		t.Fatalf("the launch vault's note 100 must survive a delete aimed elsewhere: %v", err)
	}
}

func TestVaultListing(t *testing.T) {
	server, _, _ := twoVaultServer(t)

	out := getVaultJSON(t, server.URL+"/api/vaults")
	if out["active"] != "main" {
		t.Fatalf("active vault = %v, want main", out["active"])
	}
	vaults := out["vaults"].([]any)
	if len(vaults) != 2 {
		t.Fatalf("expected both vaults listed, got %v", vaults)
	}
	first := vaults[0].(map[string]any)
	if first["name"] != "main" || first["active"] != true {
		t.Fatalf("the launch vault should be listed first and marked active, got %v", first)
	}
	second := vaults[1].(map[string]any)
	if second["name"] != "work" || second["available"] != true {
		t.Fatalf("registered vault should be listed as available, got %v", second)
	}
}

func TestSearchLabelsResultsWithTheirVault(t *testing.T) {
	server, _, _ := twoVaultServer(t)

	hits := getVaultJSON(t, server.URL+"/api/search?q=Alpha&vault=work")["results"].([]any)
	if len(hits) != 1 {
		t.Fatalf("expected one hit from the addressed vault, got %v", hits)
	}
	hit := hits[0].(map[string]any)
	if hit["vault"] != "work" || hit["title"] != "Alpha in work" {
		t.Fatalf("hit should be labeled with the vault it came from, got %v", hit)
	}
}
