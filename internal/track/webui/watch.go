package webui

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ttak0422/track/internal/track/index"
)

// eventHub fans out "vault changed" signals to connected SSE clients.
type eventHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[chan struct{}]struct{})}
}

func (h *eventHub) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// broadcast wakes every subscriber. The channels are buffered size 1, so a
// pending signal is coalesced rather than blocking.
func (h *eventHub) broadcast() {
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

// handleEvents streams Server-Sent Events. Clients receive a `change` event
// whenever the vault is reindexed after a filesystem change.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("streaming unsupported"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// Periodic comments keep the connection alive through idle proxies.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case _, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprint(w, "event: change\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

// startWatch watches the note and journal directories and, on any change,
// reindexes the vault and notifies connected clients. It runs for the life of
// the process; failures are logged and degrade to no live updates.
func (s *Server) startWatch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "track web: file watch disabled: %v\n", err)
		return
	}

	watched := 0
	for _, dir := range []string{s.cfg.NoteDir(), s.cfg.JournalDir()} {
		// Ensure the directory exists so it is watched even before its first note
		// (e.g. journal/ is created lazily on the first daily note).
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "track web: not watching %s: %v\n", dir, err)
			continue
		}
		if err := watcher.Add(dir); err != nil {
			fmt.Fprintf(os.Stderr, "track web: not watching %s: %v\n", dir, err)
			continue
		}
		watched++
	}
	if watched == 0 {
		_ = watcher.Close()
		return
	}

	go s.watchLoop(watcher)
}

func (s *Server) watchLoop(watcher *fsnotify.Watcher) {
	defer watcher.Close()

	const debounce = 300 * time.Millisecond
	var timer *time.Timer
	// reindex coalesces a burst of events into a single Full() + broadcast.
	reindex := func() {
		s.reindexMu.Lock()
		defer s.reindexMu.Unlock()
		if _, err := index.New(s.cfg, s.store).Full(); err != nil {
			fmt.Fprintf(os.Stderr, "track web: reindex after change failed: %v\n", err)
			return
		}
		s.events.broadcast()
	}

	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Ignore chmod-only events; react to content/structure changes.
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, reindex)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "track web: watch error: %v\n", err)
		}
	}
}
