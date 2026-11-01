package webui

import (
	"github.com/ttak0422/track/internal/track/config"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type retirementStream struct {
	header http.Header
	once   sync.Once
	ready  chan struct{}
}

func (w *retirementStream) Header() http.Header           { return w.header }
func (w *retirementStream) WriteHeader(int)               {}
func (w *retirementStream) Write(raw []byte) (int, error) { return len(raw), nil }
func (w *retirementStream) Flush()                        { w.once.Do(func() { close(w.ready) }) }

func TestRetiredBackendEndsEventStreamWithoutClosingWorkspace(t *testing.T) {
	srv := newServer(&config.Config{}, nil, true)
	r := httptest.NewRequest("GET", "http://localhost/api/events", nil)
	w := &retirementStream{header: http.Header{}, ready: make(chan struct{})}
	finished := make(chan struct{})
	go func() { srv.handleEvents(w, r); close(finished) }()
	<-w.ready
	srv.closeViews()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("event stream remained attached to retired backend")
	}
	if r.Context().Err() != nil {
		t.Fatal("fixture canceled the whole workspace instead of retiring its backend")
	}
}
