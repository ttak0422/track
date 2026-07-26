package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	// The launch vault carries no label: an unqualified id means "the vault you are in", so a
	// workspace serving one vault answers exactly as it did before it could serve several.
	if vault, _ := active["vault"].(string); vault != "" {
		t.Fatalf("the launch vault must not be labeled, got %q", vault)
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

func postBody(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestFollowCarriesTheEditorsVault(t *testing.T) {
	server, _, work := twoVaultServer(t)
	// Build both indexes so either vault can answer for note 100.
	getVaultJSON(t, server.URL+"/api/note?id=100")
	getVaultJSON(t, server.URL+"/api/note?id=100&vault=work")

	// The editor reports the vault its buffer lives in, so the workspace follows the right note of
	// two that share an id.
	resp := postBody(t, server.URL+"/api/follow", map[string]any{
		"vault_path": work,
		"note_id":    100,
		"file_kind":  "note",
		"line":       1,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow from a served vault should be accepted, got %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	state := out["state"].(map[string]any)
	if state["vault"] != "work" {
		t.Fatalf("follow state must name the editor's vault, got %v", state["vault"])
	}
	// Compare against the canonical vault path: the server resolves symlinks (on macOS /var is one).
	canonicalWork, err := config.CanonicalPath(work)
	if err != nil {
		t.Fatal(err)
	}
	if path, _ := state["path"].(string); !strings.HasPrefix(path, canonicalWork) {
		t.Fatalf("follow path must point inside the editor's vault, got %q", path)
	}
}

func TestFollowRefusesAnUnservedVault(t *testing.T) {
	server, _, _ := twoVaultServer(t)
	getVaultJSON(t, server.URL+"/api/note?id=100")

	// A buffer in some other vault must not be reported as a position in this workspace: with a
	// journal id the note would always exist here, so the mismatch has to be refused explicitly.
	resp := postBody(t, server.URL+"/api/follow", map[string]any{
		"vault_path": t.TempDir(),
		"note_id":    100,
		"file_kind":  "note",
		"line":       1,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("follow from an unserved vault should be refused, got %d", resp.StatusCode)
	}
}

func TestSearchSpansEveryServedVault(t *testing.T) {
	server, _, _ := twoVaultServer(t)

	// One search box, both vaults: without a ?vault= the workspace searches everything it serves.
	out := getVaultJSON(t, server.URL+"/api/search?q=Alpha")
	hits := out["results"].([]any)
	if len(hits) != 2 {
		t.Fatalf("expected a hit from each vault, got %v", hits)
	}
	seen := map[string]string{}
	for _, raw := range hits {
		hit := raw.(map[string]any)
		vault, _ := hit["vault"].(string) // absent for the launch vault
		seen[vault] = hit["title"].(string)
	}
	// The launch vault's hit stays unlabeled and the other vault's carries its name, so the two are
	// told apart the same way [[title]] and [[vault:title]] are.
	if seen[""] != "Alpha in main" || seen["work"] != "Alpha in work" {
		t.Fatalf("hits must be labeled by vault, unlabeled for the launch vault, got %v", seen)
	}
	if _, ok := out["unavailable"].([]any); !ok {
		t.Fatalf("cross-vault search must report unreachable vaults, got %v", out["unavailable"])
	}
}

func TestSearchReportsAnUnreachableVault(t *testing.T) {
	// A registry entry pointing at a vault that is not there — an unmounted drive, a cloud folder
	// that has not synced — must be named in the response. Silently omitting it would read as
	// "nothing matched there", which is a different and misleading answer.
	main := t.TempDir()
	writeVaultNote(t, main, 100, "Alpha in main", "# Alpha in main\n")
	missing := filepath.Join(t.TempDir(), "not-mounted")

	configPath := filepath.Join(t.TempDir(), "config.yml")
	body := "cache_dir: " + t.TempDir() + "\nvaults:\n  main: " + main + "\n  offline: " + missing + "\n"
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
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(cfg, st)
	t.Cleanup(srv.closeViews)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	out := getVaultJSON(t, server.URL+"/api/search?q=Alpha")
	if hits := out["results"].([]any); len(hits) != 1 {
		t.Fatalf("the reachable vault must still answer, got %v", hits)
	}
	gaps := out["unavailable"].([]any)
	if len(gaps) != 1 {
		t.Fatalf("the unreachable vault must be reported, got %v", gaps)
	}
	gap := gaps[0].(map[string]any)
	if gap["name"] != "offline" || gap["error"] == "" {
		t.Fatalf("the gap must name the vault and say why, got %v", gap)
	}
}

// mustView is used by the launch-vault test below; the registry itself refuses two names for one
// vault (config.resolveVaults), so the only vault reachable under two names is the launch one.
func mustView(t *testing.T, srv *Server, name string) *vaultView {
	t.Helper()
	v, err := srv.viewByName(name)
	if err != nil {
		t.Fatalf("view %q: %v", name, err)
	}
	return v
}

func TestAliasOfTheLaunchVaultIsTheLaunchVault(t *testing.T) {
	// Addressing the launch vault through a second registered name must not produce a second view:
	// its notes would then answer under a label, i.e. under different ids than the same notes reached
	// without one.
	server, _, _ := twoVaultServer(t)
	unqualified := getVaultJSON(t, server.URL+"/api/note?id=100")["note"].(map[string]any)
	viaName := getVaultJSON(t, server.URL+"/api/note?id=100&vault=main")["note"].(map[string]any)
	if unqualified["title"] != viaName["title"] {
		t.Fatalf("the launch vault must answer the same either way: %v vs %v", unqualified, viaName)
	}
	if v, _ := viaName["vault"].(string); v != "" {
		t.Fatalf("reaching the launch vault by name must still leave it unlabeled, got %q", v)
	}
}

func TestASearchReportsAVaultThatWentAway(t *testing.T) {
	// The index lives in the cache directory, not inside the vault, so a vault that is unmounted
	// mid-session would keep answering from a stale index. That has to read as a gap, not as results.
	server, _, work := twoVaultServer(t)
	if hits := getVaultJSON(t, server.URL+"/api/search?q=Alpha")["results"].([]any); len(hits) != 2 {
		t.Fatalf("both vaults should answer while both are present, got %v", hits)
	}
	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}

	out := getVaultJSON(t, server.URL+"/api/search?q=Alpha")
	if hits := out["results"].([]any); len(hits) != 1 {
		t.Fatalf("only the reachable vault should answer, got %v", hits)
	}
	gaps := out["unavailable"].([]any)
	if len(gaps) != 1 || gaps[0].(map[string]any)["name"] != "work" {
		t.Fatalf("the vault that went away must be reported, got %v", gaps)
	}
}

func TestNoteShowsInboundReferencesFromOtherVaults(t *testing.T) {
	// A [[main:Alpha in main]] written in another vault is an edge in THAT vault's index, keyed by
	// title. The workspace has to go look for it, or the reference the CLI reports would simply be
	// absent here.
	main, work := t.TempDir(), t.TempDir()
	writeVaultNote(t, main, 100, "Alpha in main", "# Alpha in main\n")
	writeVaultNote(t, work, 300, "Refers across", "See [[main:Alpha in main]].\n")

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
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(cfg, st)
	t.Cleanup(srv.closeViews)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	// Reading the other vault builds its index, which is where the reference lives.
	getVaultJSON(t, server.URL+"/api/note?id=300&vault=work")

	out := getVaultJSON(t, server.URL+"/api/note?id=100")
	external, ok := out["external"].([]any)
	if !ok || len(external) != 1 {
		t.Fatalf("expected one inbound reference from another vault, got %v", out["external"])
	}
	ref := external[0].(map[string]any)
	if ref["vault"] != "work" || ref["title"] != "Refers across" {
		t.Fatalf("the reference must name its vault and note, got %v", ref)
	}
}

func TestConcurrentRequestsShareOneViewPerVault(t *testing.T) {
	// Views open lazily under a mutex while the server handles requests in parallel. Drive both
	// vaults' endpoints at once so the race detector sees the cache being filled and read together,
	// and check every request still got the vault it asked for.
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
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(cfg, st)
	t.Cleanup(srv.closeViews)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			url := server.URL + "/api/note?id=100"
			want := "Alpha in main"
			if i%2 == 1 {
				url += "&vault=work"
				want = "Alpha in work"
			}
			resp, err := http.Get(url)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			var out map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				errs <- err
				return
			}
			note, ok := out["note"].(map[string]any)
			if !ok || note["title"] != want {
				errs <- fmt.Errorf("concurrent request for %s got %v", url, out)
			}
		}(i)
	}
	// Cross-vault search runs alongside, reaching every served vault through the same cache.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(server.URL + "/api/search?q=Alpha")
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// One vault, one open index: a racing cache would have left duplicate handles behind.
	srv.viewsMu.Lock()
	opened := len(srv.views)
	srv.viewsMu.Unlock()
	if opened != 1 {
		t.Fatalf("the second vault must be opened exactly once, got %d", opened)
	}
}
