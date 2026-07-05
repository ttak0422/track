package webui

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/journal"
	"github.com/ttak0422/track/internal/track/store"
	tmpl "github.com/ttak0422/track/internal/track/template"
)

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
	// Activity days ride along so the calendar can derive per-day note lists from this one listing,
	// the same way the static export's notes.json carries them.
	days, err := s.store.AllNoteDays()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range results {
		results[i].Days = days[results[i].NoteID]
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
