package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
