// Package webui serves track's local interactive workspace.
package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

type Server struct {
	cfg      *config.Config
	store    *store.Store
	mux      *http.ServeMux
	colorCSS string
}

func New(cfg *config.Config, s *store.Store) *Server {
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux()}
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
	return http.ListenAndServe(addr, New(cfg, st).Handler())
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/app.js", serveText("text/javascript; charset=utf-8", appJS))
	s.mux.HandleFunc("/style.css", serveText("text/css; charset=utf-8", styleCSS))
	s.mux.HandleFunc("/api/search", s.handleSearch)
	s.mux.HandleFunc("/api/notes", s.handleNotes)
	s.mux.HandleFunc("/api/activity", s.handleActivity)
	s.mux.HandleFunc("/api/resolve", s.handleResolve)
	s.mux.HandleFunc("/api/note", s.handleNote)
	s.mux.HandleFunc("/api/graph/local", s.handleLocalGraph)
	s.mux.HandleFunc("/api/graph", s.handleGraph)
}

func serveText(contentType string, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(body))
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Inject the configured default theme. Config.WebTheme is normalized to system/light/dark, so this
	// is never arbitrary text from the user's config.
	theme := s.cfg.WebTheme
	if theme == "" {
		theme = "system"
	}
	html := strings.Replace(indexHTML, "__TRACK_DEFAULT_THEME__", theme, 1)
	overrides := ""
	if s.colorCSS != "" {
		overrides = "<style id=\"track-colors\">\n" + s.colorCSS + "</style>"
	}
	html = strings.Replace(html, "__TRACK_COLOR_OVERRIDES__", overrides, 1)
	_, _ = w.Write([]byte(html))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
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
	addSearchPaths(s.cfg, results)
	writeJSON(w, map[string]any{"results": results})
}

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	results, err := s.store.SearchRefs()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	sortRefs(results)
	addSearchPaths(s.cfg, results)
	writeJSON(w, map[string]any{"notes": results})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	days := parseDays(r.URL.Query().Get("days"), 7)
	today := localDate(time.Now())
	start := today.AddDate(0, 0, -(days - 1))
	counts, err := s.store.ActivitySince(start)
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
			"start_date": start.Format("2006-01-02"),
			"days":       days,
			"total":      total,
			"counts":     counts,
		},
	})
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
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
			"note_id":         ref.NoteID,
			"file_kind":       ref.FileKind,
			"path":            path,
			"copy_path":       s.cfg.DisplayPathForKind(ref.FileKind, ref.NoteID),
			"title":           ref.Title,
			"tags":            ref.Tags,
			"generated_by_ai": ref.GeneratedByAI,
			"body":            body,
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
		if a.GeneratedByAI != b.GeneratedByAI {
			if a.GeneratedByAI {
				return 1
			}
			return -1
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

func parseDays(raw string, fallback int) int {
	days := parseLimit(raw, fallback)
	if days < 7 {
		return 7
	}
	if days > 3650 {
		return 3650
	}
	return days
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
