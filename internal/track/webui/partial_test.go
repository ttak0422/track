package webui

import (
	"encoding/json"
	"fmt"
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func partialTestServer(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}, DateFormat: "2006-01-02", JournalOff: true}
	cfg.DBPath = filepath.Join(t.TempDir(), "index.db")
	writeVaultNote(t, cfg.VaultDir, 1, "Synthetic", "xy needle")
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := index.New(cfg, st).Full(); err != nil {
		t.Fatal(err)
	}
	return New(cfg, st), cfg
}

// Invoke handlers in memory; these tests do not bind a port or contact an app.
func partialRequest(s *Server, method, target, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "http://127.0.0.1"+target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestPartialWebSearchRetainsMissingAndExplainsRefreshFailure(t *testing.T) {
	s, cfg := partialTestServer(t)
	if err := os.Rename(cfg.NotePath(1), cfg.NotePath(1)+".held"); err != nil {
		t.Fatal(err)
	}
	w := partialRequest(s, "GET", "/api/search?q=Synthetic", "")
	if w.Code != 200 {
		t.Fatalf("search %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Results []store.SearchResult `json:"results"`
		Partial []struct {
			State store.ReadState `json:"read_state"`
		} `json:"partial"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || len(response.Partial) != 1 || !response.Partial[0].State.CacheUsed {
		t.Fatalf("missing state: %s", w.Body.String())
	}
	s.active.reindexMu.Lock()
	s.active.refreshErr = fmt.Errorf("synthetic refresh failure")
	s.active.lastStale = time.Now()
	s.active.reindexMu.Unlock()
	w = partialRequest(s, "GET", "/api/search?q=Synthetic", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "synthetic refresh failure") {
		t.Fatalf("cached fallback hid failure: %d %s", w.Code, w.Body.String())
	}
}

func TestPartialWebMissingSidecarCannotBecomeEmptyEditableMetadata(t *testing.T) {
	s, cfg := partialTestServer(t)
	original, err := os.ReadFile(cfg.MetadataPath(1))
	if err != nil {
		t.Fatal(err)
	}
	held := cfg.MetadataPath(1) + ".held"
	if err := os.Rename(cfg.MetadataPath(1), held); err != nil {
		t.Fatal(err)
	}
	// During refresh throttling a direct metadata read must still explain the gap.
	s.active.lastStale = time.Now()
	w := partialRequest(s, "GET", "/api/note?id=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"read_state"`) {
		t.Fatalf("direct metadata failure hidden: %d %s", w.Code, w.Body.String())
	}
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/api/note/meta?id=1", ""},
		{"POST", "/api/note/meta?id=1", `{"title":"Changed","tags":[]}`},
		{"POST", "/api/note/read?id=1", `{"event":"seen"}`},
	} {
		w := partialRequest(s, req.method, req.path, req.body)
		if w.Code < 400 {
			t.Fatalf("missing metadata became writable: %s %d %s", req.path, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(cfg.MetadataPath(1)); !os.IsNotExist(err) {
		t.Fatalf("metadata recreated: %v", err)
	}
	raw, err := os.ReadFile(held)
	if err != nil || string(raw) != string(original) {
		t.Fatal("metadata altered", err)
	}
	if err := s.store.RequireMetadata(1, cfg.MetadataPath(1)); err == nil {
		t.Fatal("known lost metadata accepted")
	}
	// A genuinely new note may still lack a sidecar.
	if _, found, err := s.store.ReadMetadata(2, cfg.MetadataPath(2)); err != nil || found {
		t.Fatalf("new note rejected: %v %v", found, err)
	}
}
