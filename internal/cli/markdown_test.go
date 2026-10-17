package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/markdownhandoff"
)

func TestMarkdownCLI(t *testing.T) {
	file := filepath.Join(t.TempDir(), "日本語 file.md")
	if err := os.WriteFile(file, []byte("# Saved"), 0644); err != nil {
		t.Fatal(err)
	}
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]string{"protocol": markdownhandoff.Protocol})
			return
		}
		posts++
		var snapshot markdownhandoff.Snapshot
		if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
			t.Error(err)
		}
		if snapshot.Active != "日本語 file.md" || len(snapshot.Files) != 1 || string(snapshot.Files[0].Data) != "# Saved" {
			t.Errorf("bad transfer: %+v", snapshot)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"route": "/markdown#handoff=" + strings.Repeat("a", 64)})
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	out, code := capture(t, func() int { return Run([]string{"markdown", "--addr", addr, "--check"}) })
	if code != 0 || !strings.Contains(out, `"ready":true`) || posts != 0 {
		t.Fatalf("probe: %d %s", code, out)
	}
	out, code = capture(t, func() int { return Run([]string{"markdown", "--addr", addr, "--", file}) })
	if code != 0 || !strings.Contains(out, server.URL+"/markdown#handoff=") || strings.Contains(out, file) || posts != 1 {
		t.Fatalf("handoff: %d %s", code, out)
	}
	server.Close()
	out, code = capture(t, func() int { return cmdMarkdown([]string{"--addr", addr, "--check"}) })
	if code != 0 || !strings.Contains(out, `"ready":false`) {
		t.Fatalf("stopped probe: %d %s", code, out)
	}
}

func TestMarkdownRefusesRemoteAndIncompatibleServers(t *testing.T) {
	for _, addr := range []string{"example.com:8765", "0.0.0.0:8765", "192.168.1.1:8765", "127.0.0.1:0", "localhost:bad"} {
		if _, err := markdownBaseURL(addr); err == nil {
			t.Errorf("accepted %s", addr)
		}
	}
	for _, redirect := range []bool{false, true} {
		posts := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posts++
			}
			if redirect {
				http.Redirect(w, r, "http://example.com", 302)
			} else {
				w.Write([]byte(`{"protocol":"other"}`))
			}
		}))
		out, code := capture(t, func() int {
			return cmdMarkdown([]string{"--addr", strings.TrimPrefix(s.URL, "http://"), "--", "/not/read.md"})
		})
		s.Close()
		if code == 0 || posts != 0 {
			t.Fatalf("incompatible server: %d %s", code, out)
		}
	}
}

func TestMarkdownLiteralFilenameFlags(t *testing.T) {
	args, ok := applyVaultFlag([]string{"markdown", "--", "--vault=literal.md"})
	if !ok || len(args) != 3 || args[2] != "--vault=literal.md" {
		t.Fatalf("filename parsed as a global flag: %v", args)
	}
}
