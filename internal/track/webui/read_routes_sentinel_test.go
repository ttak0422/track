//go:build linux || darwin

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/markdownhandoff"
	"github.com/ttak0422/track/internal/track/store"
)

func policyCall(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://localhost"+target, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestInternalReadAllowedComputeAndEphemeralCanonicalSentinels(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	f := newSentinelFixture(t)
	p, err := newReadPolicy(f.cache, []string{f.configuration})
	if err != nil {
		t.Fatal(err)
	}
	loader := func(_ context.Context, _ string, strict bool) (*config.Config, error) {
		copy := *f.cfg
		copy.Provisional = !strict
		return &copy, nil
	}
	b := newBootstrapWithReadPolicy(context.Background(), loader, store.Open, nil, p)
	defer b.Close()
	before := canonicalInventory(t, f.canonical()...)
	// Handoff is bounded, loopback-only process memory even before initialization.
	snapshot := markdownhandoff.Snapshot{Active: "a.md", Files: []markdownhandoff.File{{Path: "a.md", Data: []byte("# Snapshot")}}}
	raw, _ := json.Marshal(snapshot)
	grant := policyCall(b, "POST", "/api/markdown", string(raw))
	if grant.Code != 200 {
		t.Fatalf("initial handoff: %d %s", grant.Code, grant.Body)
	}
	var token struct {
		Route string `json:"route"`
	}
	if err := json.Unmarshal(grant.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	capability := strings.TrimPrefix(token.Route, "/markdown#handoff=")
	if len(capability) != 64 {
		t.Fatalf("bad capability: %s", grant.Body)
	}
	if w := policyCall(b, "POST", "/api/storage", ""); w.Code != 200 {
		t.Fatalf("retry: %d %s", w.Code, w.Body)
	}
	sentinelWait(t, func() bool { return b.Status().Phase == "ready" })
	b.mu.Lock()
	s := b.server
	b.mu.Unlock()
	if s.readPolicy == nil || b.readPolicy != p {
		t.Fatal("retry lost immutable policy")
	}
	sentinelViewReady(t, s.active)
	s.kickView("work")
	var peer *vaultView
	sentinelWait(t, func() bool {
		v, ok := s.cachedView("work")
		if !ok || v.cfg.Provisional {
			return false
		}
		peer = v
		return true
	})
	sentinelPeerReadReady(t, s, peer)
	assertCanonicalInventory(t, before, f.canonical()...)
	// Make both active and peer eligible for normal activity writes; subsequent
	// compute and Follow must refresh only private derived caches.
	sentinelEdit(t, f.active, "active compute changed\n", 2)
	sentinelEdit(t, f.peer, "peer compute changed\n", 3)
	before = canonicalInventory(t, f.canonical()...)
	sentinelResetRefresh(s.active)
	sentinelResetRefresh(peer)
	renderBody, _ := json.Marshal(map[string]string{"body": "![[work:Sentinel]]\n\n```track-query\nTABLE title\n```\n"})
	rendered := policyCall(b, "POST", "/api/render", string(renderBody))
	if rendered.Code != 200 || !strings.Contains(rendered.Body.String(), "peer compute changed") || strings.Contains(rendered.Body.String(), "```track-query") {
		t.Fatalf("render: %d %s", rendered.Code, rendered.Body)
	}
	spec := `{"version":2,"mark":"bar","data":{"kind":"metric","records":[{"name":"A","time":"t1","value":3}]},"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}`
	encoded, _ := json.Marshal(map[string]string{"spec": spec})
	chart := policyCall(b, "POST", "/api/viewspec", string(encoded))
	if chart.Code != 200 || !strings.Contains(chart.Body.String(), "echarts") {
		t.Fatalf("viewspec: %d %s", chart.Code, chart.Body)
	}
	follow, _ := json.Marshal(map[string]any{"note_id": 100, "file_kind": "note", "vault_path": f.peer, "line": 2})
	updated := policyCall(b, "POST", "/api/follow", string(follow))
	if updated.Code != 200 || !strings.Contains(updated.Body.String(), `"active":true`) {
		t.Fatalf("follow: %d %s", updated.Code, updated.Body)
	}
	if state, ok := s.currentFollowState(); !ok || state.Vault != "work" || state.Line != 2 {
		t.Fatalf("follow memory state %+v %v", state, ok)
	}
	consume := `{"token":"` + capability + `"}`
	opened := policyCall(b, "POST", "/api/markdown/consume", consume)
	if opened.Code != 200 || strings.TrimSpace(opened.Body.String()) != string(raw) {
		t.Fatalf("consume: %d %s", opened.Code, opened.Body)
	}
	if w := policyCall(b, "POST", "/api/markdown/consume", consume); w.Code != 410 {
		t.Fatalf("one-time consume: %d", w.Code)
	}
	sentinelViewReady(t, s.active)
	sentinelViewReady(t, peer)
	sentinelCachedMtime(t, s.active, f.active)
	sentinelCachedMtime(t, peer, f.peer)
	assertCanonicalInventory(t, before, f.canonical()...)
	// Bound/validate the memory capability remains mandatory under the policy.
	if w := policyCall(b, "POST", "/api/markdown", `{"path":"/outside"}`); w.Code != 400 {
		t.Fatalf("path payload accepted: %d", w.Code)
	}
	for range 8 {
		if w := policyCall(b, "POST", "/api/markdown", string(raw)); w.Code != 200 {
			t.Fatalf("queue failed early: %d", w.Code)
		}
	}
	if w := policyCall(b, "POST", "/api/markdown", string(raw)); w.Code != 503 {
		t.Fatalf("unbounded queue: %d", w.Code)
	}
	b.placeholder.markdown.mu.Lock()
	for key, value := range b.placeholder.markdown.grants {
		value.expires = time.Now().Add(-time.Second)
		b.placeholder.markdown.grants[key] = value
	}
	b.placeholder.markdown.mu.Unlock()
	if w := policyCall(b, "POST", "/api/markdown", string(raw)); w.Code != 200 {
		t.Fatalf("expired queue did not prune: %d", w.Code)
	}
	b.Close()
	assertCanonicalInventory(t, before, f.canonical()...)
}
