package webui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Journal lookup never invokes journal.Open, a resolver, or a cache refresh.
func (s *Server) handleJournalLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), 405)
		return
	}
	title := r.URL.Query().Get("title")
	if !store.ValidJournalLookupTitle(title) {
		writeError(w, errors.New("title must be a real YYYYMM or YYYYMMDD journal title in years 0001 through 9998"), 400)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("vault"))
	v := s.active
	if name != "" && name != s.active.name {
		if _, known := s.active.cfg.Vaults[name]; !known {
			writeError(w, fmt.Errorf("vault %q is not registered", name), 400)
			return
		}
		var ok bool
		v, ok = s.cachedView(name)
		if !ok {
			writeError(w, store.ErrCalendarActivityUnavailable, 503)
			return
		}
	}
	generation, ready := s.calendarActivityReady(v)
	if !ready {
		writeError(w, store.ErrCalendarActivityUnavailable, 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	found, revision, err := v.store.LookupJournalContext(ctx, title)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if errors.Is(err, store.ErrJournalLookupAmbiguous) {
			writeError(w, err, 409)
		} else {
			calendarActivityError(w, err)
		}
		return
	}
	if found != nil {
		// The path comes only from the verified selected vault, fixed kind and positive indexed ID.
		// The shared reader checks bounded regular files, stable versions and metadata without repair.
		canonical, err := note.ReadFileContext(ctx, v.cfg.PathForKind("journal", found.NoteID), v.cfg)
		if err != nil || canonical.ID != found.NoteID || canonical.Kind != "journal" || canonical.Meta.Title != title {
			writeError(w, errors.New("journal source is unavailable or no longer matches the verified lookup"), 503)
			return
		}
	}
	current, ready := s.calendarActivityReady(v)
	if !ready || current != generation {
		writeError(w, store.ErrCalendarActivityUnavailable, 503)
		return
	}
	if err := v.store.CalendarActivityRevisionContext(ctx, revision); err != nil {
		calendarActivityError(w, err)
		return
	}
	if err := ctx.Err(); err != nil {
		calendarActivityError(w, err)
		return
	}
	writeJSON(w, struct {
		Vault string                   `json:"vault"`
		Title string                   `json:"title"`
		Found bool                     `json:"found"`
		Note  *store.JournalLookupNote `json:"note"`
	}{v.label, title, found != nil, found})
}
