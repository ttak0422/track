package webui

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/store"
)

func runHealthRefresh(t *testing.T, s *Server) {
	t.Helper()
	p := &s.active.progress
	p.mu.Lock()
	p.last = time.Time{}
	p.mu.Unlock()
	s.refreshAsync(s.active)
	waitBoot(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return !p.running })
}

func TestHealthyUnchangedRefreshDoesNotInvalidateClientsOrWriteCache(t *testing.T) {
	s, _ := partialTestServer(t)
	s.enableAsync()
	runHealthRefresh(t, s)
	if state := s.active.asyncState(); state.Partial {
		t.Fatalf("initial verification failed: %+v", state)
	}
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	before, err := s.active.store.Revision()
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		runHealthRefresh(t, s)
		if state := s.active.asyncState(); state.Partial {
			t.Fatalf("healthy refresh degraded: %+v", state)
		}
		select {
		case event := <-ch:
			t.Errorf("unchanged refresh emitted client invalidation: %s", event.name)
		default:
		}
	}
	after, err := s.active.store.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("unchanged refresh rewrote cache revision: %d -> %d", before, after)
	}
}

func TestBackgroundReadPassKeepsVerifiedStatusAndNoteWritable(t *testing.T) {
	s, _ := partialTestServer(t)
	s.enableAsync()
	b := newBootstrap(context.Background(), nil, nil, nil)
	b.server, b.cfg = s, s.cfg
	b.status = StorageStatus{Phase: "partial", ReadOnly: true, Reason: "Index verification is in progress"}
	if status := b.Status(); status.Phase == "ready" {
		t.Fatal("unknown initial snapshot reported ready")
	}
	runHealthRefresh(t, s)
	p := &s.active.progress
	p.mu.Lock()
	p.running = true // Hold the real production begin-pass state without relying on OS scheduling.
	p.beginPass(s.cfg.VaultDir)
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.running = false; p.mu.Unlock() }()
	if status := b.Status(); status.Phase != "ready" || status.ReadOnly {
		t.Fatalf("healthy background read reported recovery: %+v", status)
	}
	w := bootRequest(b, "GET", "/api/note?id=1", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"read_only":true`) {
		t.Fatalf("verified note degraded: %d %s", w.Code, w.Body.String())
	}
	s.active.mutationGate().Lock()
	if status := b.Status(); status.Phase == "ready" || !status.ReadOnly {
		t.Errorf("actual pending mutation not reported: %+v", status)
	}
	s.active.mutationGate().Unlock()
	p.mu.Lock()
	p.state.Add(store.ReadIssue{Path: s.cfg.VaultDir, Reason: "real read failure"})
	p.beginPass(s.cfg.VaultDir)
	p.mu.Unlock()
	if status := b.Status(); status.Phase == "ready" || !status.ReadOnly {
		t.Fatalf("real degraded state hidden: %+v", status)
	}
}

func TestHealthyRefreshPublishesPersistedReadFailureRecovery(t *testing.T) {
	s, _ := partialTestServer(t)
	s.enableAsync()
	runHealthRefresh(t, s)
	if err := s.active.store.AddReadIssue(store.ReadIssue{Path: s.cfg.VaultDir, Reason: "query-time read failure"}); err != nil {
		t.Fatal(err)
	}
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	runHealthRefresh(t, s)
	state, err := s.active.store.ReadState()
	if err != nil || state.Partial {
		t.Fatalf("verified recheck retained persisted write-blocking failure: %+v %v", state, err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("persisted recovery did not notify read-only clients")
	}
	w := httptest.NewRecorder()
	if !s.asyncMutation(s.active, w, httptest.NewRequest("PUT", "/api/note?id=1", nil)) {
		t.Fatalf("recovered cache still refuses mutation: %d %s", w.Code, w.Body.String())
	}
	s.active.mutationGate().Unlock()
}

func TestChangedAndMissingNotesStillNotifyAfterVerification(t *testing.T) {
	s, cfg := partialTestServer(t)
	s.enableAsync()
	runHealthRefresh(t, s)
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	if err := os.WriteFile(cfg.NotePath(1), []byte("changed body for refresh"), 0600); err != nil {
		t.Fatal(err)
	}
	runHealthRefresh(t, s)
	select {
	case <-ch:
	default:
		t.Fatal("changed note did not notify")
	}
	w := partialRequest(s, "GET", "/api/note?id=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "changed body for refresh") {
		t.Fatalf("changed body missing: %d %s", w.Code, w.Body.String())
	}
	if err := os.Remove(cfg.NotePath(1)); err != nil {
		t.Fatal(err)
	}
	runHealthRefresh(t, s)
	if !s.active.asyncState().Partial {
		t.Fatal("missing note incorrectly treated as no-op")
	}
	select {
	case <-ch:
	default:
		t.Fatal("missing note did not notify")
	}
	writeVaultNote(t, cfg.VaultDir, 1, "Recovered title", "recovered body")
	runHealthRefresh(t, s)
	if s.active.asyncState().Partial {
		t.Fatal("real missing-file recovery remained partial")
	}
	select {
	case <-ch:
	default:
		t.Fatal("real recovery did not notify")
	}
	w = partialRequest(s, "GET", "/api/note?id=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Recovered title") || !strings.Contains(w.Body.String(), "recovered body") {
		t.Fatalf("recovery lost note metadata/body: %d %s", w.Code, w.Body.String())
	}
}

func TestMutationInvalidationRequiresFreshVerificationWithoutDiscardingLastHealth(t *testing.T) {
	s, _ := partialTestServer(t)
	s.enableAsync()
	runHealthRefresh(t, s)
	s.active.invalidateRead()
	if s.active.asyncState().Partial {
		t.Fatal("invalidation unexpectedly discarded last completed health")
	}
	p := &s.active.progress
	p.mu.Lock()
	p.beginPass(s.cfg.VaultDir)
	p.mu.Unlock()
	if !s.active.asyncState().Partial {
		t.Fatal("invalidated snapshot became writable before verification")
	}
	runHealthRefresh(t, s)
	if s.active.asyncState().Partial {
		t.Fatal("successful re-verification did not restore readiness")
	}
}
