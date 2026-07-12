// Package webui serves track's local interactive workspace.
package webui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
)

type Server struct {
	cfg      *config.Config
	store    *store.Store
	mux      *http.ServeMux
	webRoot  fs.FS
	colorCSS string
	// session is a token unique to this server process, injected into index.html so the frontend can
	// tell a fresh launch (new token → discard restored tab strip) from a reload (same token → keep it).
	session   string
	events    *eventHub
	reindexMu sync.Mutex
	lastStale time.Time
	ogpMu     sync.Mutex
	ogpCache  map[string]ogpCacheEntry
	followMu  sync.Mutex
	follow    *followState
}

type followState struct {
	NoteID    int64  `json:"note_id"`
	FileKind  string `json:"file_kind"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line"`
	TopLine   int    `json:"top_line"`
	LineCount int    `json:"line_count"`
	UpdatedAt string `json:"updated_at"`
}

// staleCheckInterval throttles the read-time freshness scan. The fsnotify watcher already reindexes on
// local changes; this scan is the safety net for changes it misses (another process, or a cloud sync
// that raises no event). Throttling keeps a burst of requests from each rescanning the vault.
const staleCheckInterval = 250 * time.Millisecond

// followStateTTL keeps the web Follow toggle from jumping to an old Neovim position when the user turns
// it on after leaving the web server running. Fresh states still let an already-open Neovim buffer sync
// immediately instead of waiting for the next cursor event.
const followStateTTL = 10 * time.Second

// refreshIfStale reconciles the index with the notes on disk before a read, so the web workspace
// reflects edits made by another process or an external/cloud sync even when no filesystem event
// arrived. It shares reindexMu with the watcher so the two never reindex concurrently, and notifies
// connected clients when a reconcile actually changed something.
func (s *Server) refreshIfStale() {
	s.reindexMu.Lock()
	defer s.reindexMu.Unlock()
	if time.Since(s.lastStale) < staleCheckInterval {
		return
	}
	s.lastStale = time.Now()
	changed, err := index.New(s.cfg, s.store).RefreshIfStale()
	if err != nil {
		fmt.Fprintf(os.Stderr, "track web: refresh-if-stale failed: %v\n", err)
		return
	}
	if changed {
		s.events.broadcastChange()
	}
}

// The frontend ships an ES-module web worker (pdf.js) as a .mjs asset. Browsers only run a module
// worker when it is served with a JavaScript MIME type, but Go's mime table lacks a .mjs entry on some
// platforms (e.g. a Linux host with no /etc/mime.types), where it would default to no Content-Type and
// the worker would fail to start. Register it here so the type is deterministic across hosts.
func init() {
	_ = mime.AddExtensionType(".mjs", "text/javascript; charset=utf-8")
}

func New(cfg *config.Config, s *store.Store) *Server {
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux(), webRoot: embeddedWebRoot, session: newSessionToken(), events: newEventHub()}
	// A palette is a best-effort cosmetic override; a bad file must not take the workspace down, so we
	// warn and fall back to the built-in colors rather than failing to start.
	if css, err := LoadPalette(cfg.WebColorsPath); err != nil {
		fmt.Fprintf(os.Stderr, "track web: ignoring palette: %v\n", err)
	} else {
		srv.colorCSS = css
	}
	srv.routes()
	return srv
}

// newSessionToken returns a random per-process token. A crypto/rand read failure is not fatal: an empty
// token just means the frontend keeps its restored tabs (the pre-existing behavior).
func newSessionToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func Serve(cfg *config.Config, st *store.Store, addr string) error {
	srv := New(cfg, st)
	srv.startWatch()
	return http.ListenAndServe(addr, srv.Handler())
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/search", s.handleSearch)
	s.mux.HandleFunc("/api/notes", s.handleNotes)
	s.mux.HandleFunc("/api/activity", s.handleActivity)
	s.mux.HandleFunc("/api/agenda", s.handleAgenda)
	s.mux.HandleFunc("/api/journal", s.handleJournal)
	s.mux.HandleFunc("/api/resolve", s.handleResolve)
	s.mux.HandleFunc("/api/note", s.handleNote)
	s.mux.HandleFunc("/api/note/meta", s.handleNoteMeta)
	s.mux.HandleFunc("/api/render", s.handleRender)
	s.mux.HandleFunc("/api/viewspec", s.handleViewSpec)
	s.mux.HandleFunc("/api/asset", s.handleAsset)
	s.mux.HandleFunc("/api/ogp", s.handleOGP)
	s.mux.HandleFunc("/api/graph/local", s.handleLocalGraph)
	s.mux.HandleFunc("/api/graph", s.handleGraph)
	s.mux.HandleFunc("/api/follow", s.handleFollow)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	// Everything that is not an API route is served from the embedded frontend build.
	s.mux.HandleFunc("/", s.handleApp)
}
