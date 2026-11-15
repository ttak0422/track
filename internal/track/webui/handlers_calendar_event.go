package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ttak0422/track/internal/track/config"
	"io"
	"net/http"
	"strings"

	"github.com/ttak0422/track/internal/track/calendarevent"
)

func eventError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, calendarevent.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, calendarevent.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, calendarevent.ErrNotFound):
		status = http.StatusNotFound
	}
	writeError(w, err, status)
}

func (s *Server) handleCalendarEvents(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	deleted := r.URL.Query().Get("deleted")
	if deleted != "" && deleted != "0" && deleted != "1" {
		writeError(w, errors.New("deleted must be 0 or 1"), http.StatusBadRequest)
		return
	}
	events, err := calendarevent.List(v.cfg, deleted == "1")
	if err != nil {
		eventError(w, err)
		return
	}
	writeJSON(w, map[string]any{"vault": v.label, "events": events})
}

func (s *Server) handleCalendarEvent(v *vaultView, w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	switch r.Method {
	case http.MethodGet:
		event, err := calendarevent.Get(v.cfg, id)
		if err != nil {
			eventError(w, err)
			return
		}
		writeJSON(w, map[string]any{"vault": v.label, "event": event})
	case http.MethodPut:
		var req calendarevent.WriteRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, calendarevent.MaxRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeError(w, fmt.Errorf("decode calendar event: %w", err), http.StatusBadRequest)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, errors.New("request must be one JSON object"), http.StatusBadRequest)
			return
		}
		var event calendarevent.Event
		err := v.write(func() error {
			var err error
			event, err = calendarevent.Save(v.cfg, id, req)
			return err
		})
		if err != nil {
			eventError(w, err)
			return
		}
		s.events.broadcastChange()
		writeJSON(w, map[string]any{"vault": v.label, "event": event})
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCalendarEventsDay(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	if query.Has("deleted") {
		writeError(w, errors.New("deleted is not supported for day projection"), http.StatusBadRequest)
		return
	}
	day, err := calendarevent.DayContext(r.Context(), v.cfg, query.Get("date"), query.Get("time_zone"))
	if err != nil {
		eventError(w, err)
		return
	}
	writeJSON(w, struct {
		Vault string `json:"vault"`
		calendarevent.DayProjection
	}{Vault: v.label, DayProjection: day})
}

func (s *Server) handleCalendarEventsRange(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	if query.Has("deleted") {
		writeError(w, errors.New("deleted is not supported for range projection"), http.StatusBadRequest)
		return
	}
	projection, err := calendarevent.RangeContext(r.Context(), v.cfg, query.Get("since"), query.Get("until"), query.Get("time_zone"))
	if err != nil {
		eventError(w, err)
		return
	}
	writeJSON(w, struct {
		Vault string `json:"vault"`
		calendarevent.RangeProjection
	}{Vault: v.label, RangeProjection: projection})
}

func calendarProjectionPath(path string) bool {
	return path == "/api/calendar-events/day" || path == "/api/calendar-events/range" || path == "/api/calendar-activity/range"
}

// Range reads canonical event files only: resolving a vault must not open or refresh an index.
func (s *Server) handleCalendarEventsRangeRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("vault"))
	v := s.active
	if name != "" && name != s.active.name {
		path, known := s.active.cfg.Vaults[name]
		if !known {
			writeError(w, fmt.Errorf("vault %q is not registered", name), http.StatusBadRequest)
			return
		}
		if cached, ok := s.cachedView(name); ok && !cached.cfg.Provisional {
			v = cached
		} else {
			cfg, err := config.LoadBounded(r.Context(), path, true)
			if err != nil {
				writeError(w, err, http.StatusBadRequest)
				return
			}
			v = &vaultView{name: name, label: name, cfg: cfg}
		}
	}
	if v.cfg.Provisional {
		writeError(w, errors.New("storage is not ready for calendar projection"), http.StatusServiceUnavailable)
		return
	}
	if err := v.cfg.ValidateLinkFormat(); err != nil {
		writeError(w, err, http.StatusConflict)
		return
	}
	s.handleCalendarEventsRange(v, w, r)
}
