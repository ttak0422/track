// Package webui serves track's local interactive workspace.
package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/journal"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	tmpl "github.com/ttak0422/track/internal/track/template"
)

type Server struct {
	cfg       *config.Config
	store     *store.Store
	mux       *http.ServeMux
	colorCSS  string
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
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux(), events: newEventHub()}
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
	s.mux.HandleFunc("/api/render", s.handleRender)
	s.mux.HandleFunc("/api/asset", s.handleAsset)
	s.mux.HandleFunc("/api/ogp", s.handleOGP)
	s.mux.HandleFunc("/api/graph/local", s.handleLocalGraph)
	s.mux.HandleFunc("/api/graph", s.handleGraph)
	s.mux.HandleFunc("/api/follow", s.handleFollow)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	// Everything that is not an API route is served from the embedded frontend build.
	s.mux.HandleFunc("/", s.handleApp)
}

// handleApp serves the embedded React frontend: a request that maps to a real built file (the hashed
// JS/CSS bundles, icons, etc.) returns that file, and anything else falls back to index.html so the
// client-side router can handle deep links like /notes/123.
func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}

	upath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if upath == "" || upath == "." || upath == "index.html" {
		s.serveIndex(w, r)
		return
	}

	f, err := webRoot.Open(upath)
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		s.serveIndex(w, r)
		return
	}

	if ct := mime.TypeByExtension(path.Ext(upath)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// Vite emits content-hashed filenames under assets/, so those are safe to cache indefinitely; any
	// other static file (icons, etc.) is left uncached so swaps are picked up immediately.
	if strings.HasPrefix(upath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, upath, stat.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(webRoot, "index.html")
	if err != nil {
		writeError(w, fmt.Errorf("read index: %w", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	// Inject the configured default theme. Config.WebTheme is normalized to system/light/dark, so this
	// is never arbitrary text from the user's config.
	theme := s.cfg.WebTheme
	if theme == "" {
		theme = "system"
	}
	html := strings.Replace(string(raw), "__TRACK_DEFAULT_THEME__", theme, 1)
	overrides := ""
	if s.colorCSS != "" {
		overrides = "<style id=\"track-colors\">\n" + s.colorCSS + "</style>"
	}
	html = strings.Replace(html, "__TRACK_COLOR_OVERRIDES__", overrides, 1)
	_, _ = w.Write([]byte(html))
}

// handleAsset serves a note's media/attachments from the vault's per-kind assets directory
// (note/assets, journal/assets). Notes reference an attachment with the relative path "assets/<file>";
// the frontend rewrites that to /api/asset?kind=<kind>&name=<file> so the file is served from the vault
// instead of being resolved against the /notes/<id> route and swallowed by the SPA index fallback (an
// embedded image/PDF would otherwise render the app inside itself). name is constrained to the assets
// directory so a note cannot read arbitrary files via "../" traversal.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, errors.New("name is required"), http.StatusBadRequest)
		return
	}
	dir := s.cfg.AssetsDirForKind(r.URL.Query().Get("kind"))
	// Clean the slash path, drop any leading separator, then confirm the result stays inside the assets
	// directory before touching the filesystem.
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	full := filepath.Join(dir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, errors.New("invalid asset path"), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeError(w, errors.New("asset not found"), http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		writeError(w, errors.New("asset not found"), http.StatusNotFound)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(full)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// Vault assets can change underneath us (an edit or a cloud sync), so they are not cached.
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	var (
		results []store.SearchResult
		err     error
	)
	if query == "" {
		results, err = s.store.SearchRefs()
		sortRefs(results)
		if len(results) > limit {
			results = results[:limit]
		}
	} else {
		results, err = s.store.SearchScoped(query, limit, store.SearchAll)
	}
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	addSearchPaths(s.cfg, results)
	writeJSON(w, map[string]any{"results": results})
}

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	results, err := s.store.SearchRefs()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	sortRefs(results)
	addSearchPaths(s.cfg, results)
	writeJSON(w, map[string]any{"notes": results})
}

// handleActivity returns the per-day note activity within a [since, until] window (inclusive), counted
// from note_days so it reflects notes worked on, not journal opens. The window is generic: since/until
// are YYYY-MM-DD. until defaults to today and since to four weeks before until.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	today := localDate(time.Now())
	until := today
	if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
		if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
			until = t
		}
	}
	since := until.AddDate(0, 0, -27)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
			since = t
		}
	}
	sinceStr := since.Format("2006-01-02")
	untilStr := until.Format("2006-01-02")
	counts, err := s.store.NoteActivityRange(sinceStr, untilStr)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	total := 0
	for _, day := range counts {
		total += day.Count
	}
	writeJSON(w, map[string]any{
		"activity": map[string]any{
			"since":  sinceStr,
			"until":  untilStr,
			"total":  total,
			"counts": counts,
		},
	})
}

// handleAgenda lists the notes active (created or updated) on a calendar day, so a journal view can show
// which notes were worked on that day. The date defaults to today; the format is YYYY-MM-DD.
func (s *Server) handleAgenda(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if date == "" {
		date = localDate(time.Now()).Format("2006-01-02")
	}
	notes, err := s.store.NotesOnDay(date)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if notes == nil {
		notes = []store.NoteRef{}
	}
	for i := range notes {
		notes[i].Path = s.cfg.PathForKind(notes[i].FileKind, notes[i].NoteID)
	}
	writeJSON(w, map[string]any{"date": date, "notes": notes})
}

// handleJournal opens or creates the journal for a day and returns its note id, letting the activity
// heatmap navigate to that day's journal. The day defaults to today; date is YYYY-MM-DD. Web-created
// journals start empty (their date is the note's title); the CLI applies its template engine.
func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	day := localDate(time.Now())
	if raw := strings.TrimSpace(r.URL.Query().Get("date")); raw != "" {
		t, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			writeError(w, fmt.Errorf("invalid date %q", raw), http.StatusBadRequest)
			return
		}
		day = t
	}
	res, err := journal.Open(s.cfg, day, journal.Options{
		CreateBody: func(name string, id int64, d time.Time) (string, error) {
			spec, err := tmpl.DefaultSpec(s.cfg, config.KindJournal)
			if err != nil {
				return "", err
			}
			if spec == "" {
				return "", nil
			}
			return tmpl.Render(s.cfg, spec, name, id, config.KindJournal, "", d)
		},
	})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	ix := index.New(s.cfg, s.store)
	for _, p := range res.Reindex {
		if err := ix.One(p); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{"note_id": res.NoteID, "created": res.Created})
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		writeError(w, errors.New("term is required"), http.StatusBadRequest)
		return
	}
	ref, found, err := s.store.ResolveTerm(term)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if found {
		ref.Path = s.cfg.PathForKind(ref.FileKind, ref.NoteID)
	}
	writeJSON(w, map[string]any{"found": found, "note": ref})
}

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, "":
		s.getNote(w, r)
	case http.MethodPut:
		s.putNote(w, r)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *Server) getNote(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	ref, err := s.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	path := s.cfg.PathForKind(ref.FileKind, ref.NoteID)
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	backlinks, err := s.store.Backlinks(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if backlinks == nil {
		backlinks = []store.NoteRef{}
	}
	for i := range backlinks {
		backlinks[i].Path = s.cfg.PathForKind(backlinks[i].FileKind, backlinks[i].NoteID)
	}
	writeJSON(w, map[string]any{
		"note": map[string]any{
			"note_id":   ref.NoteID,
			"file_kind": ref.FileKind,
			"path":      path,
			"copy_path": s.cfg.DisplayPathForKind(ref.FileKind, ref.NoteID),
			"title":     ref.Title,
			"tags":      ref.Tags,
			"body":      body,
			// etag is a content hash of the file as read; clients echo it back on PUT so a save can be
			// rejected when the file changed underneath (e.g. an OneDrive sync) since this read.
			"etag": etagFor(raw),
		},
		"backlinks": backlinks,
	})
}

// putNote saves the body of an existing note. The request JSON carries the new body and the etag the
// client last read; if the file changed on disk since then the save is refused with 409 so a cloud-sync
// update is not silently overwritten. Titles stay sidecar-authoritative, so only the body is touched.
//
// TODO(track): the web frontend has no editor UI yet (textarea/keymap/save affordance) and PUT cannot
// create new notes. Both are deferred follow-ups; this is the save+conflict-detection backend slice only.
func (s *Server) putNote(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	ref, err := s.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	var req struct {
		Body string `json:"body"`
		ETag string `json:"etag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}

	path := s.cfg.PathForKind(ref.FileKind, ref.NoteID)
	current, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if req.ETag == "" {
		writeError(w, errors.New("etag is required to detect conflicts"), http.StatusBadRequest)
		return
	}
	if req.ETag != etagFor(current) {
		writeError(w, errors.New("note changed on disk since it was loaded; reload before saving"), http.StatusConflict)
		return
	}

	out := []byte(ensureTrailingNewline(req.Body))
	if err := os.WriteFile(path, out, 0o644); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if err := index.New(s.cfg, s.store).One(path); err != nil {
		writeError(w, fmt.Errorf("reindex: %w", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"note_id": ref.NoteID, "etag": etagFor(out), "saved": true})
}

// handleRender sanitizes a raw note body into the Markdown the frontend renders: track action links
// (editor-only, not web-navigable) are flattened to plain text while wiki links, code, and ordinary
// Markdown pass through. Keeping this on the server makes the engine the single source of truth for
// track-specific Markdown semantics, and lets the editor preview the live (unsaved) body by posting it
// here rather than re-implementing the rules in the frontend.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	res, err := export.Export(&note.Note{Body: req.Body}, export.NewWebRenderer(), export.Options{})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"markdown": res.Markdown})
}

// etagFor returns a short content hash used as an optimistic-concurrency token for note bodies.
func etagFor(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// ensureTrailingNewline mirrors the CLI's write behavior so saved bodies end with exactly one newline.
func ensureTrailingNewline(body string) string {
	if body == "" {
		return ""
	}
	if strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}

func (s *Server) handleLocalGraph(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	graph, err := s.store.LocalGraph(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Path = s.cfg.PathForKind(graph.Nodes[i].FileKind, graph.Nodes[i].NoteID)
	}
	writeJSON(w, map[string]any{"graph": graph})
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	graph, err := s.store.FullGraph()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Path = s.cfg.PathForKind(graph.Nodes[i].FileKind, graph.Nodes[i].NoteID)
	}
	writeJSON(w, map[string]any{"graph": graph})
}

func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.followMu.Lock()
		state := s.follow
		s.followMu.Unlock()
		if state == nil {
			writeJSON(w, map[string]any{"active": false})
			return
		}
		writeJSON(w, map[string]any{"active": true, "state": state})
	case http.MethodPost:
		var state followState
		if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
			writeError(w, fmt.Errorf("decode follow state: %w", err), http.StatusBadRequest)
			return
		}
		if state.NoteID <= 0 {
			writeError(w, errors.New("note_id is required"), http.StatusBadRequest)
			return
		}
		if state.FileKind != config.KindNote && state.FileKind != config.KindJournal {
			writeError(w, errors.New("file_kind must be note or journal"), http.StatusBadRequest)
			return
		}
		if state.Line < 1 {
			state.Line = 1
		}
		if state.TopLine < 1 {
			state.TopLine = state.Line
		}
		if state.LineCount < 1 {
			state.LineCount = state.TopLine
		}
		state.Path = s.cfg.PathForKind(state.FileKind, state.NoteID)
		state.UpdatedAt = time.Now().Format(time.RFC3339Nano)
		s.followMu.Lock()
		s.follow = &state
		s.followMu.Unlock()
		s.events.broadcastFollow(state)
		writeJSON(w, map[string]any{"active": true, "state": state})
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *Server) noteByID(id int64) (store.SearchResult, error) {
	notes, err := s.store.SearchRefs()
	if err != nil {
		return store.SearchResult{}, err
	}
	for _, n := range notes {
		if n.NoteID == id {
			return n, nil
		}
	}
	return store.SearchResult{}, fmt.Errorf("note %d is not indexed", id)
}

func addSearchPaths(cfg *config.Config, results []store.SearchResult) {
	for i := range results {
		results[i].Path = cfg.PathForKind(results[i].FileKind, results[i].NoteID)
	}
}

func sortRefs(results []store.SearchResult) {
	slices.SortFunc(results, func(a, b store.SearchResult) int {
		if a.Mtime != b.Mtime {
			return desc(a.Mtime, b.Mtime)
		}
		return desc(a.NoteID, b.NoteID)
	})
}

func desc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func parseID(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	if raw == "" {
		return 0, errors.New("id is required")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}
	return id, nil
}

func parseLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > 500 {
		return 500
	}
	return n
}

func localDate(t time.Time) time.Time {
	y, m, d := t.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
