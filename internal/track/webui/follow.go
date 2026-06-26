package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, active := s.currentFollowState()
		if !active {
			writeJSON(w, map[string]any{"active": false})
			return
		}
		writeJSON(w, map[string]any{"active": true, "state": state})
	case http.MethodPost:
		var state followState
		if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
			writeError(w, fmt.Errorf("decode follow state: %w", err), http.StatusBadRequest)
			return
		}
		if state.NoteID <= 0 {
			writeError(w, errors.New("note_id is required"), http.StatusBadRequest)
			return
		}
		if state.FileKind != config.KindNote && state.FileKind != config.KindJournal {
			writeError(w, errors.New("file_kind must be note or journal"), http.StatusBadRequest)
			return
		}
		ref, err := s.noteByID(state.NoteID)
		if err != nil {
			writeError(w, err, http.StatusNotFound)
			return
		}
		if ref.FileKind != state.FileKind {
			writeError(w, fmt.Errorf("note %d is indexed as %s, not %s", state.NoteID, ref.FileKind, state.FileKind), http.StatusBadRequest)
			return
		}
		if state.Line < 1 {
			state.Line = 1
		}
		if state.TopLine < 1 {
			state.TopLine = state.Line
		}
		if state.LineCount < 1 {
			state.LineCount = state.TopLine
		}
		state.Path = s.cfg.PathForKind(state.FileKind, state.NoteID)
		state.UpdatedAt = time.Now().Format(time.RFC3339Nano)
		s.followMu.Lock()
		s.follow = &state
		s.followMu.Unlock()
		s.events.broadcastFollow(state)
		writeJSON(w, map[string]any{"active": true, "state": state})
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *Server) currentFollowState() (*followState, bool) {
	s.followMu.Lock()
	defer s.followMu.Unlock()
	if s.follow == nil {
		return nil, false
	}
	if followStateExpired(*s.follow) {
		s.follow = nil
		return nil, false
	}
	state := *s.follow
	return &state, true
}

func followStateExpired(state followState) bool {
	updated, err := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	if err != nil {
		return true
	}
	return time.Since(updated) > followStateTTL
}
