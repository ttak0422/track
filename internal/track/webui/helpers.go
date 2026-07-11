package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/dashboard"
	"github.com/ttak0422/track/internal/track/store"
)

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
		// The store fills Icon with the per-note sidecar override; resolve it against the config
		// tag/kind mapping here so an empty override falls back to the mapping (config.NoteIcon).
		results[i].Icon = cfg.NoteIcon(results[i].FileKind, results[i].Tags, results[i].Icon)
	}
}

// sortRefs applies the one ordering every note-list surface shares — most recently updated first, id
// ascending on ties. The store's agenda/backlink queries and the static bundle's listing sort the same
// way, so a calendar cell, the day page it opens, and a note's backlinks always read in the same order.
func sortRefs(results []store.SearchResult) {
	slices.SortFunc(results, func(a, b store.SearchResult) int {
		if a.Mtime != b.Mtime {
			return desc(a.Mtime, b.Mtime)
		}
		return desc(b.NoteID, a.NoteID)
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

// dashboardData gathers the vault values a ```dashboard block renders from: note titles in the shared
// recently-updated-first order for the recent widget, and today's journal name for the journal shortcut.
// Errors are non-fatal — a widget just renders empty — so a dashboard note never fails to load.
func (s *Server) dashboardData() dashboard.Data {
	refs, err := s.store.SearchRefs()
	if err != nil {
		return dashboard.Data{}
	}
	sortRefs(refs)
	titles := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.FileKind == config.KindJournal {
			continue // the recent widget lists real notes, not the auto-created daily journals
		}
		if r.Title != "" {
			titles = append(titles, r.Title)
		}
	}
	return dashboard.Data{
		RecentTitles: titles,
		JournalTitle: localDate(time.Now()).Format(s.cfg.JournalDateFormat),
	}
}

// homeNoteID resolves the configured web.home (a note title or numeric id) to a note id, or 0 when unset
// or unresolvable. It lets the workspace open a landing note instead of the search hero.
func (s *Server) homeNoteID() int64 {
	home := strings.TrimSpace(s.cfg.WebHome)
	if home == "" {
		return 0
	}
	if ref, found, err := s.store.ResolveTerm(home); err == nil && found {
		return ref.NoteID
	}
	if id, err := strconv.ParseInt(home, 10, 64); err == nil {
		return id
	}
	return 0
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
