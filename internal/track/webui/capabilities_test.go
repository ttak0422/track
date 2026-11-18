package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

func TestCapabilitiesIdentifyImmutableModeWithoutConfigurationIO(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		var policy *readPolicy
		if enabled {
			var err error
			policy, err = newReadPolicy("/unused-private-root", []string{"/unused-machine-config"})
			if err != nil {
				t.Fatal(err)
			}
		}
		b := newBootstrapWithReadPolicy(context.Background(), func(context.Context, string, bool) (*config.Config, error) {
			t.Fatal("capability loaded config")
			return nil, nil
		}, func(string) (*store.Store, error) { t.Fatal("capability opened cache"); return nil, nil }, func(*config.Config) (func(), error) { t.Fatal("capability acquired lease"); return nil, nil }, policy)
		r := httptest.NewRequest(http.MethodGet, "http://localhost/api/capabilities", nil)
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		var got struct {
			Protocol          string `json:"protocol"`
			Version           int    `json:"version"`
			CanonicalReadOnly bool   `json:"canonical_read_only"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got.Protocol != "track-canonical-read-only" || got.Version != 1 || got.CanonicalReadOnly != enabled {
			t.Fatalf("capability: %d %+v", w.Code, got)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable handshake")
		}
		if b.Status().Phase != "initializing" {
			t.Fatal("handshake altered readiness")
		}
		w = httptest.NewRecorder()
		b.placeholder.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("placeholder: %d %s", w.Code, w.Body.String())
		}
		b.Close()
	}
}

func TestCapabilityHandlerIsGETOnly(t *testing.T) {
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := httptest.NewRecorder()
		writeCapabilities(w, httptest.NewRequest(method, "/api/capabilities", nil), true)
		if w.Code != 405 || w.Header().Get("Allow") != "GET" {
			t.Fatalf("%s: %d", method, w.Code)
		}
	}
}

func TestCanonicalReadOnlyBootstrapRequiresExistingPrivateRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", filepath.Join(root, "missing-machine.yml"))
	missing := filepath.Join(root, "not-created")
	for _, path := range []string{"", "relative", missing} {
		if b, err := NewCanonicalReadOnlyBootstrap(context.Background(), path, nil); err == nil || b != nil {
			t.Fatalf("admitted root %q", path)
		}
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatal("created missing root")
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCanonicalReadOnlyBootstrap(context.Background(), root, nil); err == nil {
		t.Fatal("shared root admitted")
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := NewCanonicalReadOnlyBootstrap(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("constructor wrote files", entries, err)
	}
	if b.readPolicy == nil || b.readPolicy.configurationFiles[0] != config.ConfigPath() {
		t.Fatal("configuration path not captured")
	}
}

func TestCapabilityModeDoesNotReplaceStorageHealth(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		var policy *readPolicy
		if enabled {
			policy = &readPolicy{root: "/unused-private-root", configurationFiles: []string{"/unused-config"}}
		}
		cfg := &config.Config{}
		srv := newServerWithReadPolicy(cfg, nil, true, policy)
		srv.active.progress.verified = true
		srv.active.progress.state.CheckedAt = 1
		peer := &vaultView{cfg: cfg}
		peer.progress.verified = true
		peer.progress.state.CheckedAt = 1
		srv.views["peer"] = peer
		calls := 0
		srv.mux = http.NewServeMux()
		srv.mux.HandleFunc("/api/calendar-events/day", func(w http.ResponseWriter, r *http.Request) { calls++; writeJSON(w, map[string]any{"events": []any{}}) })
		b := newBootstrapWithReadPolicy(context.Background(), nil, nil, nil, policy)
		b.server = srv
		b.serverGeneration = 1
		b.generation = 1
		b.status.Generation = 1
		if b.Status().ReadOnly || b.Status().Phase != "ready" {
			t.Fatalf("mode replaced health: %+v", b.Status())
		}
		for _, query := range []string{"", "?vault=peer"} {
			req := httptest.NewRequest(http.MethodGet, "http://localhost/api/calendar-events/day"+query, nil)
			if b.statusFor(req).ReadOnly {
				t.Fatal("healthy calendar marked unavailable")
			}
			w := httptest.NewRecorder()
			b.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("healthy calendar %s: %d %s", query, w.Code, w.Body.String())
			}
		}
		if calls != 2 {
			t.Fatalf("calendar not reached: %d", calls)
		}
		srv.active.progress.state.Partial = true
		peer.progress.state.Partial = true
		for _, query := range []string{"", "?vault=peer"} {
			w := httptest.NewRecorder()
			b.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/calendar-events/day"+query, nil))
			if w.Code != 503 {
				t.Fatalf("partial calendar admitted: %d", w.Code)
			}
		}
		if calls != 2 {
			t.Fatal("partial reached calendar")
		}
		if enabled {
			w := httptest.NewRecorder()
			b.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "http://localhost/api/note", nil))
			if w.Code != 403 {
				t.Fatal("mode no longer blocks mutation")
			}
		}
		// No worker/store was started; do not call Close on the synthetic nil store.
	}
}
