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

// handleSearch searches every vault the workspace serves, so one search box reaches a whole set of
// vaults rather than only the one track web was launched in. ?vault=<name> narrows it back to a
// single vault. Each hit is labelled with the vault it came from, and vaults that could not be
// opened are listed under "unavailable" instead of quietly shrinking the result set.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	if name := strings.TrimSpace(r.URL.Query().Get("vault")); name != "" {
		v, err := s.viewByName(name)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		results, err := s.searchOne(v, query, limit)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"results": results, "unavailable": []vaultInfo{}})
		return
	}

	views, unavailable := s.servedViews()
	if unavailable == nil {
		unavailable = []vaultInfo{}
	}
	for _, v := range views {
		s.refresh(v)
	}
	// One vault needs no federated connection, and this is also the single-vault workspace's path.
	if len(views) == 1 {
		results, err := s.searchOne(views[0], query, limit)
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"results": results, "unavailable": unavailable})
		return
	}

	results, err := s.searchAcross(views, query, limit)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"results": results, "unavailable": unavailable})
}

// searchOne is the single-vault search: an empty query lists the most recently updated notes, the
// same listing the workspace opens with.
func (s *Server) searchOne(v *vaultView, query string, limit int) ([]store.SearchResult, error) {
	s.refresh(v)
	var (
		results []store.SearchResult
		err     error
	)
	if query == "" {
		results, err = v.store.SearchRefs()
		sortRefs(results)
		if len(results) > limit {
			results = results[:limit]
		}
	} else {
		results, err = v.store.SearchScoped(query, limit, store.SearchAll)
	}
	if err != nil {
		return nil, err
	}
	addSearchPaths(v, results)
	if results == nil {
		results = []store.SearchResult{}
	}
	return results, nil
}

// searchAcross searches every served vault and merges the pages into one ranked list. It is the
// single-vault search run once per vault: searchOne already resolves each hit's path, icon, and wire
// label against the vault it came from — all vault-local, so no shared query could fill them anyway —
// and each vault's page is its own top-k under the order every vault shares, so merging them yields
// the same list a single query over all of them would (store.MergeSearchResults). A vault that could
// not be opened never reaches here; servedViews already reported it as a gap.
func (s *Server) searchAcross(views []*vaultView, query string, limit int) ([]store.SearchResult, error) {
	pages := make([][]store.SearchResult, 0, len(views))
	for _, v := range views {
		page, err := s.searchOne(v, query, limit)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	if query != "" {
		return store.MergeSearchResults(pages, limit), nil
	}
	// An empty query is the recent-notes listing, not a search: it carries no rank and every listing
	// surface breaks an mtime tie by ascending id (sortRefs), the opposite of the search order.
	results := []store.SearchResult{}
	for _, page := range pages {
		results = append(results, page...)
	}
	sortRefs(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Server) handleNotes(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
	results, err := v.store.SearchRefs()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	// Activity days ride along so the calendar can derive per-day note lists from this one listing,
	// the same way the static export's notes.json carries them.
	days, err := v.store.AllNoteDays()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range results {
		results[i].Days = days[results[i].NoteID]
	}
	sortRefs(results)
	addSearchPaths(v, results)
	writeJSON(w, map[string]any{"notes": results})
}

// handleActivity returns the per-day note activity within a [since, until] window (inclusive), counted
// from note_days so it reflects notes worked on, not journal opens. The window is generic: since/until
// are YYYY-MM-DD. until defaults to today and since to four weeks before until.
func (s *Server) handleActivity(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
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
	counts, err := v.store.NoteActivityRange(sinceStr, untilStr)
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
func (s *Server) handleAgenda(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if date == "" {
		date = localDate(time.Now()).Format("2006-01-02")
	}
	notes, err := v.store.NotesOnDay(date)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if notes == nil {
		notes = []store.NoteRef{}
	}
	addRefPaths(v, notes)
	writeJSON(w, map[string]any{"date": date, "notes": notes})
}

// handleJournal opens or creates the journal for a day and returns its note id, letting the activity
// heatmap navigate to that day's journal. The day defaults to today; date is YYYY-MM-DD. Web-created
// journals start empty (their date is the note's title); the CLI applies its template engine.
func (s *Server) handleJournal(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
	res, err := journal.Open(v.cfg, day, journal.Options{
		CreateBody: func(name string, id int64, d time.Time) (string, error) {
			spec, err := tmpl.DefaultSpec(v.cfg, config.KindJournal)
			if err != nil {
				return "", err
			}
			if spec == "" {
				return "", nil
			}
			return tmpl.Render(v.cfg, spec, name, id, config.KindJournal, "", d)
		},
	})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	ix := index.New(v.cfg, v.store)
	for _, p := range res.Reindex {
		if err := ix.One(p); err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{"vault": v.label, "note_id": res.NoteID, "created": res.Created})
}

func (s *Server) handleResolve(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if term == "" {
		writeError(w, errors.New("term is required"), http.StatusBadRequest)
		return
	}
	ref, found, err := v.store.ResolveTerm(term)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if found {
		ref.Vault = v.label
		ref.Path = v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	}
	writeJSON(w, map[string]any{"found": found, "note": ref})
}
