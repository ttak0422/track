package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/markdownhandoff"
)

func TestMarkdownHandoffCapabilitiesAndGuard(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	s.mux.Handle("/api/markdown", s.requestLoopbackOnly(http.HandlerFunc(s.handleMarkdownHandoff)))
	s.mux.Handle("/api/markdown/consume", s.requestLoopbackOnly(http.HandlerFunc(s.handleMarkdownConsume)))
	handler := s.Handler()
	call := func(route, method, body, host, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:8765"+route, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		if host != "" {
			r.Host = host
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body, _ := json.Marshal(markdownhandoff.Snapshot{Active: "日本語 file.md", Files: []markdownhandoff.File{{Path: "日本語 file.md", Data: []byte("# Hello")}}})
	for _, variant := range []struct{ host, origin string }{{"evil.test", ""}, {"", "https://evil.test"}} {
		if w := call("/api/markdown", "POST", string(body), variant.host, variant.origin); w.Code != 403 {
			t.Fatalf("foreign request accepted: %d", w.Code)
		}
	}
	if w := call("/api/markdown", "POST", `{"path":"/etc/passwd"}`, "", ""); w.Code != 400 {
		t.Fatalf("file-read request accepted: %d", w.Code)
	}
	if w := call("/api/markdown", "GET", "", "", ""); w.Code != 200 || strings.Contains(w.Body.String(), "route") {
		t.Fatalf("probe exposed grants: %s", w.Body)
	}
	w := call("/api/markdown", "POST", string(body), "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("handoff: %d %s", w.Code, w.Body)
	}
	var result struct {
		Route string `json:"route"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	token := strings.TrimPrefix(result.Route, "/markdown#handoff=")
	if len(token) != 64 {
		t.Fatalf("bad capability %q", result.Route)
	}
	consume := `{"token":"` + token + `"}`
	if w := call("/api/markdown/consume", "POST", consume, "", "https://evil.test"); w.Code != 403 {
		t.Fatalf("cross-origin consumption accepted: %d", w.Code)
	}
	if w := call("/api/markdown/consume", "POST", consume, "", ""); w.Code != 200 || !bytes.Equal(bytes.TrimSpace(w.Body.Bytes()), body) {
		t.Fatalf("consume: %d %s", w.Code, w.Body)
	}
	if w := call("/api/markdown/consume", "POST", consume, "", ""); w.Code != 410 {
		t.Fatalf("replay accepted: %d", w.Code)
	}
	s.markdown.grants[token] = markdownGrant{body: body, expires: time.Now().Add(-time.Second)}
	if w := call("/api/markdown/consume", "POST", consume, "", ""); w.Code != 410 {
		t.Fatalf("expired grant accepted: %d", w.Code)
	}
	for i := 0; i < 8; i++ {
		if w := call("/api/markdown", "POST", string(body), "", ""); w.Code != 200 {
			t.Fatalf("bounded queue failed early: %d", w.Code)
		}
	}
	if w := call("/api/markdown", "POST", string(body), "", ""); w.Code != 503 {
		t.Fatalf("queue limit ignored: %d", w.Code)
	}
	for key, grant := range s.markdown.grants {
		grant.expires = time.Now().Add(-time.Second)
		s.markdown.grants[key] = grant
	}
	if w := call("/api/markdown", "POST", string(body), "", ""); w.Code != 200 || len(s.markdown.grants) != 1 {
		t.Fatalf("expired queue was not reclaimed: %d", w.Code)
	}
	s.bindHost = "0.0.0.0"
	if w := call("/api/markdown", "POST", string(body), "", ""); w.Code != 403 {
		t.Fatalf("LAN bind accepted: %d", w.Code)
	}
}
