// Package webui serves track's local interactive workspace.
package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	mux   *http.ServeMux
}

func New(cfg *config.Config, s *store.Store) *Server {
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux()}
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
	s.mux.HandleFunc("/api/resolve", s.handleResolve)
	s.mux.HandleFunc("/api/note", s.handleNote)
	s.mux.HandleFunc("/api/graph/local", s.handleLocalGraph)
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
	_, _ = w.Write([]byte(indexHTML))
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
			"title":           ref.Title,
			"tags":            ref.Tags,
			"generated_by_ai": ref.GeneratedByAI,
			"body":            body,
		},
		"backlinks": backlinks,
	})
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
