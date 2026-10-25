package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func TestRetiredRequestsOnlyReadHistory(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	if err := os.MkdirAll(cfg.RequestsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"version":1,"id":"old","status":"applying","intent":"update","update_target":{"note_id":100,"etag":"old"},"attempts":[{"id":"dispatch-old","status":"completed"}],"result":{"proposed_body":"must not apply"}}`)
	path := filepath.Join(cfg.RequestsDir(), "old.json")
	if err := os.WriteFile(path, record, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(100), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, nil)
	handler := srv.Handler()
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/requests", 200}, {"GET", "/api/requests/old", 200},
		{"GET", "/api/requests/missing", 404}, {"GET", "/api/requests?vault=unknown", 400},
		{"POST", "/api/requests", 405}, {"PUT", "/api/requests/old", 405},
		{"GET", "/api/agents", 410}, {"POST", "/api/agents", 410},
		{"POST", "/api/requests/old/claim", 410}, {"POST", "/api/requests/old/result", 410},
		{"POST", "/api/requests/old/fail", 410}, {"POST", "/api/requests/old/cancel", 410},
		{"POST", "/api/requests/old/retry", 410}, {"POST", "/api/requests/old/save", 410},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "http://localhost"+test.path, bytes.NewBufferString(`{"agent_id":"old-agent","dispatch_id":"dispatch-old","proposed_body":"must not apply"}`))
			req.RemoteAddr = "127.0.0.1:12345"
			req.Header.Set("Authorization", "Bearer old-token")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != test.status {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if test.status == 200 && !bytes.Contains(res.Body.Bytes(), []byte(`"status":"applying"`)) {
				t.Fatalf("lost applying history: %s", res.Body.String())
			}
		})
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, record) {
		t.Fatalf("history mutated: %s %v", raw, err)
	}
	raw, err = os.ReadFile(cfg.NotePath(100))
	if err != nil || string(raw) != "original\n" {
		t.Fatalf("pending proposal applied: %s %v", raw, err)
	}
	entries, err := os.ReadDir(cfg.RequestsDir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("new history created: %v %v", entries, err)
	}
}

func TestArchivedRequestReadGuards(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	srv := New(cfg, nil)
	for _, test := range []struct {
		host, remote, origin, bind string
		status                     int
	}{
		{"localhost", "127.0.0.1:12345", "", "", 200},
		{"localhost", "192.0.2.1:12345", "", "", 403},
		{"localhost", "127.0.0.1:12345", "https://evil.example", "", 403},
		{"evil.example", "127.0.0.1:12345", "", "", 403},
		{"localhost", "127.0.0.1:12345", "", "0.0.0.0", 403},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/api/requests", nil)
		req.RemoteAddr = test.remote
		req.Header.Set("Origin", test.origin)
		srv.bindHost = test.bind
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		if res.Code != test.status {
			t.Errorf("%+v: %d %s", test, res.Code, res.Body.String())
		}
		if test.status == 200 {
			var result struct {
				Requests []json.RawMessage `json:"requests"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.Requests == nil {
				t.Errorf("empty history response: %s %v", res.Body.String(), err)
			}
		}
	}
	if _, err := os.Stat(cfg.TrackDir()); !os.IsNotExist(err) {
		t.Fatalf("read created archive: %v", err)
	}
}

func TestArchivedHistoryKeepsVaultIdentity(t *testing.T) {
	other := t.TempDir()
	cfg := &config.Config{VaultDir: t.TempDir(), VaultName: "main", Vaults: map[string]string{"research": other}}
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	t.Setenv("TRACK_VAULT", "")
	for _, v := range []struct{ dir, label string }{{cfg.VaultDir, "main"}, {other, "research"}} {
		dir := filepath.Join(v.dir, ".track", "requests")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw := `{"version":1,"id":"same-id","vault":"` + v.label + `","status":"completed","instruction":"` + v.label + ` answer"}`
		if err := os.WriteFile(filepath.Join(dir, "same-id.json"), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(cfg, nil)
	defer srv.closeViews()
	for _, v := range []string{"main", "research"} {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/api/requests/same-id?vault="+v, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		if res.Code != 200 || !bytes.Contains(res.Body.Bytes(), []byte(`"instruction":"`+v+` answer"`)) {
			t.Fatalf("vault %s: %d %s", v, res.Code, res.Body.String())
		}
	}
}
