package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

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
