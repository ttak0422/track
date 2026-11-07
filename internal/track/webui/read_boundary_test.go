package webui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func boundaryRequest(b *readBoundary, target string, h http.HandlerFunc) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", target, nil)
	b.serve(w, r, h, func(http.ResponseWriter, string) bool { return false })
	return w
}
func TestReadBoundaryContainsPanicAndPreservesExactCachedIDs(t *testing.T) {
	b := newReadBoundary()
	target := "http://localhost/api/note?id=9223372036854775806"
	good := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"note":{"note_id":9223372036854775806,"body":"verified","etag":"safe"},"props":{"integer":18446744073709551615}}`))
	})
	if w := boundaryRequest(b, target, good); w.Code != 200 {
		t.Fatal(w.Code)
	}
	failed := boundaryRequest(b, target, func(http.ResponseWriter, *http.Request) { panic("controlled optional handler failure") })
	if failed.Code != 200 || !strings.Contains(failed.Body.String(), `9223372036854775806`) || !strings.Contains(failed.Body.String(), `18446744073709551615`) || !strings.Contains(failed.Body.String(), `"read_only":true`) || !strings.Contains(failed.Body.String(), `"partial":true`) {
		t.Fatalf("unsafe cached fallback: %d %s", failed.Code, failed.Body.String())
	}
	if w := boundaryRequest(b, target, good); w.Code != 200 {
		t.Fatalf("later action poisoned: %d", w.Code)
	}
	if w := boundaryRequest(b, "http://localhost/api/unknown", func(http.ResponseWriter, *http.Request) { panic("new failure") }); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestReadBoundaryDiscardsLateGenerationAndNeverCachesPostPreviews(t *testing.T) {
	b := newReadBoundary()
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		finished <- boundaryRequest(b, "http://localhost/api/note?id=1", func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-release
			w.Write([]byte(`{"note":{"body":"old vault"}}`))
		})
	}()
	<-entered
	b.advance(2)
	close(release)
	if w := <-finished; w.Code != 503 {
		t.Fatalf("late source accepted: %d %s", w.Code, w.Body.String())
	}
	if len(b.cache) != 0 {
		t.Fatal("late reply seeded new cache")
	}
	good := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"markdown":"first draft"}`)) })
	w := httptest.NewRecorder()
	b.serve(w, httptest.NewRequest("POST", "http://localhost/api/render", nil), good, func(http.ResponseWriter, string) bool { return false })
	failed := httptest.NewRecorder()
	b.serve(failed, httptest.NewRequest("POST", "http://localhost/api/render", nil), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fail", 500) }), func(http.ResponseWriter, string) bool { return false })
	if failed.Code != 500 || strings.Contains(failed.Body.String(), "first draft") || len(b.cache) != 0 {
		t.Fatalf("wrong draft reused: %d %s", failed.Code, failed.Body.String())
	}
}
func TestReadBoundaryKeepsBlockedAdmissionChargedAndServesPeer(t *testing.T) {
	b := newReadBoundary()
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	handlerDone := make(chan struct{}, 2)
	block := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.Write([]byte(`{"note":{"body":"late"}}`))
		handlerDone <- struct{}{}
	})
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "http://localhost/api/note?id=1", nil).WithContext(ctx)
			b.serve(w, r, block, func(http.ResponseWriter, string) bool { return false })
			close(done)
		}()
		<-entered
		cancel()
		<-done
	}
	called := false
	if w := boundaryRequest(b, "http://localhost/api/note?id=2", func(w http.ResponseWriter, r *http.Request) { called = true }); w.Code != 503 || called {
		t.Fatalf("blocked slot silently recycled: %d %v", w.Code, called)
	}
	if w := boundaryRequest(b, "http://localhost/api/search?vault=healthy", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"results":[]}`)) }); w.Code != 200 {
		t.Fatalf("peer blocked: %d", w.Code)
	}
	close(release)
	<-handlerDone
	<-handlerDone
}

// A note refresh also starts metadata, graph, link and preview reads. A third
// healthy request must wait for capacity rather than fail before rendering.
func TestReadBoundaryQueuesPreviewDuringHealthyReadBurst(t *testing.T) {
	b := newReadBoundary()
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{}, 2)
	finished := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			finished <- boundaryRequest(b, "http://localhost/api/note?id=1", func(w http.ResponseWriter, r *http.Request) {
				entered <- struct{}{}
				<-release
				w.Write([]byte(`{"note":{"body":"healthy"}}`))
			})
		}()
	}
	<-entered
	<-entered
	preview := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		b.serve(w, httptest.NewRequest("POST", "http://localhost/api/render", strings.NewReader(`{"body":"draft"}`)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"markdown":"fresh draft"}`))
		}), func(http.ResponseWriter, string) bool { return false })
		preview <- w
	}()
	select {
	case w := <-preview:
		t.Fatalf("preview rejected a healthy read burst: %d %s", w.Code, w.Body.String())
	case <-time.After(20 * time.Millisecond):
	}
	// Release just one slot; the remaining read must not hold up the preview.
	release <- struct{}{}
	if w := <-preview; w.Code != 200 || !strings.Contains(w.Body.String(), "fresh draft") {
		t.Fatalf("queued preview did not render: %d %s", w.Code, w.Body.String())
	}
	release <- struct{}{}
	<-finished
	<-finished
}

func waitReadQueue(t *testing.T, b *readBoundary, count int) {
	t.Helper()
	waitBoot(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.waiting) == count
	})
}

func TestReadBoundaryQueueKeepsOwnerOrderAndPeerIsolation(t *testing.T) {
	b := newReadBoundary()
	for range readOwnerLimit {
		if !b.admit("busy") {
			t.Fatal("initial admission failed")
		}
	}
	defer b.release("busy")
	for i := range 3 {
		pending := i
		go func() {
			if err := b.acquire(context.Background(), "busy"); err != nil {
				t.Errorf("queued acquisition: %v", err)
				return
			}
			b.mu.Lock()
			if len(b.waiting) != 2-pending {
				t.Errorf("owner order lost: request %d, %d remain", pending, len(b.waiting))
			}
			b.mu.Unlock()
			b.release("busy")
		}()
		waitReadQueue(t, b, i+1)
	}
	// Busy has both slots and queued work; a healthy peer remains independent.
	if err := b.acquire(context.Background(), "peer"); err != nil {
		t.Fatal(err)
	}
	b.release("peer")
	b.release("busy")
	waitReadQueue(t, b, 0)
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.active["busy"] == 1 })
}

func TestReadBoundaryQueueLimitsAndCancellation(t *testing.T) {
	b := newReadBoundary()
	for i := range readActiveLimit {
		if !b.admit(fmt.Sprint(i / readOwnerLimit)) {
			t.Fatal("initial admission failed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, readQueueLimit)
	for i := range readQueueLimit {
		owner := fmt.Sprint(i / readOwnerQueueLimit)
		go func() { done <- b.acquire(ctx, owner) }()
		waitReadQueue(t, b, i+1)
		if i == readOwnerQueueLimit-1 {
			if err := b.acquire(context.Background(), "0"); !errors.Is(err, errReadCapacity) {
				t.Fatalf("owner queue limit not enforced: %v", err)
			}
		}
	}
	if err := b.acquire(context.Background(), "new"); !errors.Is(err, errReadCapacity) {
		t.Fatalf("global queue limit not enforced: %v", err)
	}
	cancel()
	for range readQueueLimit {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	waitReadQueue(t, b, 0)
	for i := range readActiveLimit {
		b.release(fmt.Sprint(i / readOwnerLimit))
	}
	if len(b.active) != 0 {
		t.Fatal("canceled waiter leaked capacity")
	}
}

func TestReadBoundaryQueueDeadlineAndGenerationNeverStartHandler(t *testing.T) {
	for _, reason := range []string{"deadline", "generation", "cancel"} {
		t.Run(reason, func(t *testing.T) {
			b := newReadBoundary()
			b.admit("")
			b.admit("")
			defer b.release("")
			ctx, cancel := context.WithCancel(context.Background())
			if reason == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
			}
			defer cancel()
			called := make(chan struct{}, 1)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				b.serve(w, httptest.NewRequest("POST", "http://localhost/api/render", nil).WithContext(ctx), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called <- struct{}{}
					w.Write([]byte(`{"markdown":"obsolete"}`))
				}), func(http.ResponseWriter, string) bool { return false })
				done <- w
			}()
			waitReadQueue(t, b, 1)
			if reason == "generation" {
				b.advance(2)
				b.release("")
			} else if reason == "cancel" {
				cancel()
			}
			if w := <-done; w.Code != 503 {
				t.Fatalf("invalid queued request succeeded: %d %s", w.Code, w.Body.String())
			}
			if reason != "generation" {
				b.release("")
			}
			waitReadQueue(t, b, 0)
			select {
			case <-called:
				t.Fatal("expired or superseded queued handler started")
			default:
			}
		})
	}
}

func TestReadBoundaryCancellationDuringQueueHandoffDoesNotLeak(t *testing.T) {
	for range 100 {
		b := newReadBoundary()
		b.admit("")
		b.admit("")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- b.acquire(ctx, "") }()
		waitReadQueue(t, b, 1)
		go cancel()
		b.release("")
		if err := <-done; err == nil {
			b.release("")
		} else if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		b.release("")
		b.mu.Lock()
		if len(b.active) != 0 || len(b.waiting) != 0 {
			t.Errorf("handoff leak: active=%v, waiting=%d", b.active, len(b.waiting))
		}
		b.mu.Unlock()
	}
}

func TestReadBoundaryQueueAndHandlerShareDeadline(t *testing.T) {
	b := newReadBoundary()
	b.admit("")
	b.admit("")
	defer b.release("")
	started := time.Now()
	deadline := make(chan time.Time, 1)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- boundaryRequest(b, "http://localhost/api/note?id=1", func(w http.ResponseWriter, r *http.Request) {
			limit, _ := r.Context().Deadline()
			deadline <- limit
			w.Write([]byte(`{"note":{"body":"fresh"}}`))
		})
	}()
	waitReadQueue(t, b, 1)
	queued := time.Now()
	time.Sleep(50 * time.Millisecond)
	b.release("")
	limit := <-deadline
	// The timer must have started before the waiter was observed, not after
	// release. There is no wall-time upper bound on scheduler execution here.
	if limit.Before(started.Add(readResponseBudget)) || limit.After(queued.Add(readResponseBudget)) {
		t.Fatalf("queue reset the response budget: start=%v queued=%v deadline=%v", started, queued, limit)
	}
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestReadBoundaryDispatchSkipsIneligibleQueuedOwner(t *testing.T) {
	b := newReadBoundary()
	for i := range readActiveLimit {
		b.admit(fmt.Sprint(i / readOwnerLimit))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	busy := make(chan error, 1)
	peer := make(chan error, 1)
	go func() { busy <- b.acquire(ctx, "0") }()
	waitReadQueue(t, b, 1)
	go func() { peer <- b.acquire(ctx, "peer") }()
	waitReadQueue(t, b, 2)
	b.release("1")
	if err := <-peer; err != nil {
		t.Fatal(err)
	}
	waitReadQueue(t, b, 1)
	b.release("peer")
	cancel()
	if err := <-busy; !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked owner unexpectedly admitted: %v", err)
	}
	for i := range readActiveLimit {
		if i == 2 { // released owner 1's first slot above
			continue
		}
		b.release(fmt.Sprint(i / readOwnerLimit))
	}
}
