// Package webui serves track's local interactive workspace.
package webui

import (
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

type Server struct {
	// active is the vault track web was launched in: the default target of every request and the
	// only vault the file watcher follows. cfg and store are its config and index, kept as fields
	// because the shell, palette, and watcher are properties of the launch vault, not of a request.
	active *vaultView
	cfg    *config.Config
	store  *store.Store
	// views caches the vaults this server opened, by the registry name that reaches them. The
	// registry gives a vault exactly one name, so one entry is one vault. The workspace reads and
	// writes across them, so a request names its vault and never inherits the active one by accident.
	viewsMu  sync.Mutex
	views    map[string]*vaultView
	mux      *http.ServeMux
	webRoot  fs.FS
	colorCSS string
	// bindHost is the non-loopback host the server was asked to listen on (empty for the default
	// loopback bind); guard admits it alongside the loopback names.
	bindHost string
	events   *eventHub
	ogpMu    sync.Mutex
	ogpCache map[string]ogpCacheEntry
	followMu sync.Mutex
	follow   *followState
}

type followState struct {
	// Vault names the vault the editor's cursor is in, so the workspace scrolls the note the editor
	// actually has open. It is the registry name on the way out; the editor knows its buffer by
	// directory, so VaultPath is what it sends in and the server maps it to a name.
	Vault     string `json:"vault,omitempty"`
	VaultPath string `json:"vault_path,omitempty"`
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

// The frontend ships an ES-module web worker (pdf.js) as a .mjs asset. Browsers only run a module
// worker when it is served with a JavaScript MIME type, but Go's mime table lacks a .mjs entry on some
// platforms (e.g. a Linux host with no /etc/mime.types), where it would default to no Content-Type and
// the worker would fail to start. Register it here so the type is deterministic across hosts.
func init() {
	_ = mime.AddExtensionType(".mjs", "text/javascript; charset=utf-8")
}

func New(cfg *config.Config, s *store.Store) *Server {
	// The launch vault carries no wire label: unqualified means "the vault you are in".
	active := &vaultView{name: activeName(cfg), cfg: cfg, store: s}
	srv := &Server{
		active:  active,
		cfg:     cfg,
		store:   s,
		views:   map[string]*vaultView{},
		mux:     http.NewServeMux(),
		webRoot: embeddedWebRoot,
		events:  newEventHub(),
	}
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

func (s *Server) Handler() http.Handler {
	return s.guard(s.mux)
}

// guard rejects the requests a browser could aim at this local server from a foreign page: any Host
// that is not this server (DNS rebinding would otherwise expose every read API), and mutating
// requests bearing a foreign Origin (CSRF against the write APIs — a cross-site fetch POST is a
// "simple request", so no preflight protects them). Non-browser clients (curl, the Neovim plugin)
// send no Origin header and are unaffected.
// ponytail: a non-loopback --addr allowlists that exact bind host only; binding 0.0.0.0 still
// admits loopback names alone — make the allowlist configurable if remote use ever matters.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host != "127.0.0.1" && host != "localhost" && host != "::1" && (s.bindHost == "" || host != s.bindHost) {
			writeError(w, fmt.Errorf("host %q not served", r.Host), http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					writeError(w, fmt.Errorf("cross-origin write from %q refused", o), http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func Serve(cfg *config.Config, st *store.Store, addr string) error {
	srv := New(cfg, st)
	defer srv.closeViews()
	if h, _, err := net.SplitHostPort(addr); err == nil {
		srv.bindHost = h
	}
	srv.startWatch()
	return http.ListenAndServe(addr, srv.Handler())
}

// stopGrace and stopPoll bound `track web stop`'s wait for a graceful exit before it escalates to
// SIGKILL. They are vars so tests can shorten the wait instead of sleeping out a full grace period.
var (
	stopGrace = 3 * time.Second
	stopPoll  = 50 * time.Millisecond
)

// cacheDirFor returns the directory that holds a vault's derived caches. The index DB lives at
// <cacheDir>/<vaultKey>/index.db (config.go resolves cacheDir the same way, then keys the DB by the
// vault path), so the cache dir is the DB path's parent's parent. The PID files for track web live
// here too, so both `track web` and `track web stop` agree on where to look.
func cacheDirFor(cfg *config.Config) string {
	return filepath.Dir(filepath.Dir(cfg.DBPath))
}

// webPIDPath returns the PID file path for a web server listening on addr. The name is keyed by host
// and port so two addrs never collide (e.g. web-127.0.0.1-8765.pid).
func webPIDPath(cfg *config.Config, addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// A bare host without a port: key on the whole string rather than guessing.
		host, port = addr, ""
	}
	safe := strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(strings.Trim(host, "[]"))
	if port == "" {
		return filepath.Join(cacheDirFor(cfg), "web-"+safe+".pid")
	}
	return filepath.Join(cacheDirFor(cfg), fmt.Sprintf("web-%s-%s.pid", safe, port))
}

// processAlive reports whether a pid names a running process. Sending signal 0 does not deliver a
// signal; it only probes whether the process exists and is signalable.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// processStartTime returns a process's start time as an opaque identity string. It is compared
// verbatim, never parsed: any difference means "not the process that wrote the PID file".
func processStartTime(pid int) (string, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return "", err
	}
	started := strings.TrimSpace(string(out))
	if started == "" {
		return "", fmt.Errorf("no such process %d", pid)
	}
	return started, nil
}

// pidFileIdentity reads a PID file written as "pid\nstart-time\n" and reports whether it names the
// still-running process that wrote it. A PID alone is not identity: the OS recycles PID numbers, so
// a stale file can point at an unrelated browser renderer or shell. Signalling that process would
// kill the wrong program, so a missing or mismatched start time reads as "not ours".
func pidFileIdentity(path string) (pid int, ours bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 0 {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		return 0, false
	}
	started, err := processStartTime(pid)
	if err != nil || started != strings.TrimSpace(lines[1]) {
		return 0, false
	}
	return pid, processAlive(pid)
}

// AcquireWebPID claims the web server's PID file for addr, refusing to start when the file points at a
// still-running process. That is the early port-conflict check: `nix run` launching a second track web
// fails here with a clear message instead of getting a bind error after the server is already half up.
// A stale file (the recorded pid is dead, or its start time no longer matches because the OS recycled
// the PID number) is overwritten. The returned release removes the file again, so a normally-exiting
// server cleans up after itself. The file holds "pid\nstart-time\n": the start time is what keeps a
// recycled PID from ever reading as a live server.
func AcquireWebPID(cfg *config.Config, addr string) (release func(), err error) {
	path := webPIDPath(cfg, addr)
	if pid, ours := pidFileIdentity(path); ours {
		return nil, fmt.Errorf("track web already running (pid %d) on %s; stop it with 'track web stop --addr %s'", pid, addr, addr)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create web pid dir: %w", err)
	}
	started, err := processStartTime(os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("read own start time: %w", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"+started+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write web pid file: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}

// StopWeb terminates the web server on addr: SIGTERM, a short graceful-exit wait, then SIGKILL for a
// straggler, and finally removes the PID file. It reports whether a live process was actually stopped.
// A missing or stale PID file is not an error — the server is simply not running (stopped=false).
// Crucially, a PID file pointing at a recycled PID number never passes: the start-time check fails,
// the file is swept, and nothing is signalled.
func StopWeb(cfg *config.Config, addr string) (stopped bool, err error) {
	path := webPIDPath(cfg, addr)
	pid, ours := pidFileIdentity(path)
	if !ours {
		// Missing, unreadable, stale, or recycled: sweep whatever is there and report not-running.
		_ = os.Remove(path)
		return false, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("signal web server: %w", err)
	}
	deadline := time.Now().Add(stopGrace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(path)
			return true, nil
		}
		time.Sleep(stopPoll)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return false, fmt.Errorf("kill web server: %w", err)
	}
	_ = os.Remove(path)
	return true, nil
}

// routes registers the API. Every endpoint that names a note — by id, by date, or by term — goes
// through withVault, so the vault it acts on is resolved once at that seam instead of each handler
// defaulting to the vault the server was launched in. Note ids are vault-local and journal ids
// collide across vaults outright, so that default would silently read (and write) the wrong file.
func (s *Server) routes() {
	s.mux.HandleFunc("/api/search", s.handleSearch)
	s.mux.HandleFunc("/api/notes", s.withVault(s.handleNotes))
	s.mux.HandleFunc("/api/activity", s.withVault(s.handleActivity))
	s.mux.HandleFunc("/api/agenda", s.withVault(s.handleAgenda))
	s.mux.HandleFunc("/api/journal", s.withVault(s.handleJournal))
	s.mux.HandleFunc("/api/resolve", s.withVault(s.handleResolve))
	s.mux.HandleFunc("/api/note", s.withVault(s.handleNote))
	s.mux.HandleFunc("/api/note/meta", s.withVault(s.handleNoteMeta))
	s.mux.HandleFunc("/api/note/read", s.withVault(s.handleNoteRead))
	s.mux.HandleFunc("/api/tasks", s.withVault(s.handleTasks))
	s.mux.HandleFunc("/api/task", s.withVault(s.handleTaskSet))
	s.mux.HandleFunc("/api/render", s.withVault(s.handleRender))
	s.mux.HandleFunc("/api/viewspec", s.withVault(s.handleViewSpec))
	s.mux.HandleFunc("/api/asset", s.withVault(s.handleAsset))
	s.mux.HandleFunc("/api/ogp", s.handleOGP)
	s.mux.HandleFunc("/api/hierarchy", s.withVault(s.handleHierarchy))
	s.mux.HandleFunc("/api/graph/local", s.withVault(s.handleLocalGraph))
	s.mux.HandleFunc("/api/graph", s.withVault(s.handleGraph))
	s.mux.HandleFunc("/api/follow", s.handleFollow)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	// Everything that is not an API route is served from the embedded frontend build.
	s.mux.HandleFunc("/", s.handleApp)
}
