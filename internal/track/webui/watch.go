package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ttak0422/track/internal/track/index"
)

type serverEvent struct {
	name string
	data []byte
}

// eventHub fans out Server-Sent Events to connected clients.
type eventHub struct {
	mu   sync.Mutex
	subs map[chan serverEvent]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[chan serverEvent]struct{})}
}

func (h *eventHub) subscribe() chan serverEvent {
	ch := make(chan serverEvent, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan serverEvent) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// broadcast wakes every subscriber. The channels are buffered size 1, so pending
// signals are coalesced rather than blocking.
func (h *eventHub) broadcast(ev serverEvent) {
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *eventHub) broadcastChange() {
	h.broadcast(serverEvent{name: "change", data: []byte("{}")})
}

// broadcastData signals that a file under the vault's data/ directory changed. Charts rendered from
// data.source / overlays[].source depend on those files without the note body changing, so this is a
// separate event from "change": no reindex happens and the frontend only refreshes rendered charts.
func (h *eventHub) broadcastData() {
	h.broadcast(serverEvent{name: "data", data: []byte("{}")})
}

func (h *eventHub) broadcastFollow(state followState) {
	data, err := json.Marshal(state)
	if err != nil {
		data = []byte("{}")
	}
	h.broadcast(serverEvent{name: "follow", data: data})
}

// handleEvents streams Server-Sent Events. Clients receive a `change` event
// whenever the vault is reindexed after a filesystem change, a `data` event
// whenever a file in the vault's data/ directory changes, and a `follow`
// event whenever Neovim publishes its active track note.
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
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data)
			flusher.Flush()
		}
	}
}

// startWatch watches the note, journal, and data directories and, on any
// change, notifies connected clients (reindexing first for note changes). It
// runs for the life of the process; failures are logged and degrade to no
// live updates.
func (s *Server) startWatch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "track web: file watch disabled: %v\n", err)
		return
	}

	watched := 0
	for _, dir := range []string{s.cfg.NoteDir(), s.cfg.JournalDir(), s.cfg.DataDir()} {
		// Ensure the directory exists so it is watched even before its first file
		// (e.g. journal/ is created lazily on the first daily note, data/ on the
		// first JSONL ingest).
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

// reconcileAfterChange reindexes the vault after a watched filesystem change and notifies clients.
// It must go through RefreshIfStale, not a bare Full(): RefreshIfStale stamps each changed note's
// activity day into its sidecar (recordActivity) before rebuilding, which is what surfaces an edited
// note under "on this day" for the day it was edited. A bare Full() would only sync mtimes, silently
// swallowing the staleness so the read-time refresh later sees nothing left to stamp.
func (s *Server) reconcileAfterChange() {
	s.reindexMu.Lock()
	defer s.reindexMu.Unlock()
	if _, err := index.New(s.cfg, s.store).RefreshIfStale(); err != nil {
		fmt.Fprintf(os.Stderr, "track web: reindex after change failed: %v\n", err)
		return
	}
	s.events.broadcastChange()
}

func (s *Server) watchLoop(watcher *fsnotify.Watcher) {
	defer watcher.Close()

	const debounce = 300 * time.Millisecond
	dataDir := s.cfg.DataDir()
	var noteTimer, dataTimer *time.Timer
	// Each timer coalesces a burst of events into a single broadcast (with a
	// reconcile first for note changes; data files are not indexed).
	reindex := func() { s.reconcileAfterChange() }
	notifyData := func() { s.events.broadcastData() }

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
			// The watch is non-recursive, so events name direct children of a
			// watched directory; a parent of dataDir means a data file changed.
			if filepath.Dir(ev.Name) == dataDir {
				if dataTimer != nil {
					dataTimer.Stop()
				}
				dataTimer = time.AfterFunc(debounce, notifyData)
				continue
			}
			if noteTimer != nil {
				noteTimer.Stop()
			}
			noteTimer = time.AfterFunc(debounce, reindex)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "track web: watch error: %v\n", err)
		}
	}
}
