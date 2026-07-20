package webui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

// TestGuard exercises the browser-facing guard: DNS rebinding (foreign Host) is refused on every
// method, CSRF (foreign Origin on a write) is refused, and the local clients the server exists
// for — same-origin browser fetches and Origin-less CLI requests — pass through.
func TestGuard(t *testing.T) {
	cfg := &config.Config{
		VaultDir:   t.TempDir(),
		DBPath:     filepath.Join(t.TempDir(), "index.db"),
		Extensions: []string{".md"},
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	server := httptest.NewServer(New(cfg, s).Handler())
	t.Cleanup(server.Close)

	do := func(method, host, origin string) int {
		req, err := http.NewRequest(method, server.URL+"/api/search?q=x", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		if host != "" {
			req.Host = host
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}

	if got := do(http.MethodGet, "", ""); got != http.StatusOK {
		t.Fatalf("loopback GET: got %d, want 200", got)
	}
	if got := do(http.MethodGet, "evil.example:8765", ""); got != http.StatusForbidden {
		t.Fatalf("rebound Host GET: got %d, want 403", got)
	}
	if got := do(http.MethodPost, "", "http://evil.example"); got != http.StatusForbidden {
		t.Fatalf("cross-origin POST: got %d, want 403", got)
	}
	if got := do(http.MethodPost, "", "null"); got != http.StatusForbidden {
		t.Fatalf("null-origin POST: got %d, want 403", got)
	}
	if got := do(http.MethodPost, "", server.URL); got == http.StatusForbidden {
		t.Fatalf("same-origin POST: got 403, want pass-through")
	}
	if got := do(http.MethodPost, "", ""); got == http.StatusForbidden {
		t.Fatalf("origin-less POST (CLI): got 403, want pass-through")
	}
}
