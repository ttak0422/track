package webui

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ttak0422/track/internal/track/request"
)

// handleRequests reads archived history. Creation and execution have been retired.
func (s *Server) handleRequests(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("agent requests have been retired; history is read-only"), http.StatusMethodNotAllowed)
		return
	}
	reqs, next, err := request.New(v.cfg).List(parseLimit(r.URL.Query().Get("limit"), 20), strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"requests": reqs, "next_cursor": next})
}

// handleRequest returns one stored request with its attempts and result.
func (s *Server) handleRequest(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != "" {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, errors.New("request id is required"), http.StatusBadRequest)
		return
	}
	req, err := request.New(v.cfg).Get(id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, request.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, err, status)
		return
	}
	writeJSON(w, req)
}

// Stale clients get an explicit retirement response instead of the frontend HTML fallback.
func retiredAgentRequests(w http.ResponseWriter, r *http.Request) {
	writeError(w, errors.New("agent requests have been retired"), http.StatusGone)
}
