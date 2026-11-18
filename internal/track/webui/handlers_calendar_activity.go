package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/calendarevent"
	"github.com/ttak0422/track/internal/track/store"
)

// Calendar activity is an index snapshot, unlike the canonical-file event projection.
// This endpoint never opens a store, starts a refresh, or warms an unavailable peer.
func (s *Server) handleCalendarActivityRange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), 405)
		return
	}
	query := r.URL.Query()
	since, until, zone := query.Get("since"), query.Get("until"), query.Get("time_zone")
	first, e1 := time.Parse(time.DateOnly, since)
	last, e2 := time.Parse(time.DateOnly, until)
	if e1 != nil || e2 != nil || first.Year() < 1 || last.Year() > 9998 || since != first.Format(time.DateOnly) || until != last.Format(time.DateOnly) || last.Before(first) || !last.Before(first.AddDate(0, 0, 42)) {
		writeError(w, errors.New("range must contain 1 through 42 inclusive real civil dates in years 0001 through 9998"), 400)
		return
	}
	loc, err := calendarevent.DisplayLocation(zone)
	if err != nil {
		eventError(w, err)
		return
	}
	name := strings.TrimSpace(query.Get("vault"))
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
	days, revision, err := v.store.CalendarActivityRangeContext(ctx, since, until)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		calendarActivityError(w, err)
		return
	}
	next, ready := s.calendarActivityReady(v)
	if !ready || next != generation {
		writeError(w, store.ErrCalendarActivityUnavailable, 503)
		return
	}
	if err := v.store.CalendarActivityRevisionContext(ctx, revision); err != nil {
		calendarActivityError(w, err)
		return
	}
	raw, err := json.Marshal(struct {
		Vault    string                      `json:"vault"`
		Since    string                      `json:"since"`
		Until    string                      `json:"until"`
		TimeZone string                      `json:"time_zone"`
		Today    string                      `json:"today"`
		Days     []store.CalendarActivityDay `json:"days"`
	}{v.label, since, until, zone, time.Now().In(loc).Format(time.DateOnly), days})
	if err != nil {
		calendarActivityError(w, err)
		return
	}
	if len(raw)+1 > 512<<10 {
		writeError(w, errors.New("calendar activity response exceeds byte budget"), 503)
		return
	}
	if err := ctx.Err(); err != nil {
		calendarActivityError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(append(raw, '\n'))
}

func calendarActivityError(w http.ResponseWriter, err error) {
	status := 500
	if errors.Is(err, store.ErrCalendarActivityUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = 503
	}
	writeError(w, err, status)
}

func (s *Server) calendarActivityReady(v *vaultView) (uint64, bool) {
	s.viewsMu.Lock()
	current := !s.closed && (v == s.active || s.views[v.name] == v)
	s.viewsMu.Unlock()
	if !current || v.cfg.Provisional || v.store == nil || v.mutationGate().Busy() {
		return 0, false
	}
	if v.async {
		p := &v.progress
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.generation, p.verified && !p.state.Partial && !p.state.CacheUsed && p.state.CheckedAt > 0
	}
	if !v.reindexMu.TryLock() {
		return 0, false
	}
	defer v.reindexMu.Unlock()
	return 0, v.refreshErr == nil
}
