package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/timeline"
)

func agentTimelineError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, timeline.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, timeline.ErrConflict), errors.Is(err, timeline.ErrDuplicate):
		status = http.StatusConflict
	case errors.Is(err, timeline.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, timeline.ErrLimit):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusServiceUnavailable
	}
	writeError(w, err, status)
}

func (s *Server) handleTimelineProjects(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	projects, err := timeline.NewStore(v.cfg.VaultDir).List(ctx)
	if err != nil {
		agentTimelineError(w, err)
		return
	}
	if projects == nil {
		projects = []timeline.Summary{}
	}
	writeJSON(w, map[string]any{"vault": v.label, "projects": projects})
}

func (s *Server) handleTimelineProject(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, errors.New("project id is required"), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	store := timeline.NewStore(v.cfg.VaultDir)
	if r.Method == http.MethodGet {
		snapshot, err := store.Read(ctx, id)
		if err != nil {
			agentTimelineError(w, err)
			return
		}
		writeJSON(w, struct {
			timeline.Snapshot
			Vault string `json:"vault"`
		}{snapshot, v.label})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, errors.New("invalid or oversized timeline request"), http.StatusBadRequest)
		return
	}
	var request timeline.Request
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, fmt.Errorf("decode timeline request: %w", err), http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, errors.New("request must be one JSON object"), http.StatusBadRequest)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	revision, found := fields["expected_revision"]
	if !found || bytes.Equal(bytes.TrimSpace(revision), []byte("null")) {
		writeError(w, errors.New("expected_revision is required"), http.StatusBadRequest)
		return
	}
	var snapshot timeline.Snapshot
	err = v.write(func() error {
		var applyErr error
		snapshot, applyErr = store.Apply(ctx, id, request)
		return applyErr
	})
	if err != nil {
		agentTimelineError(w, err)
		return
	}
	s.events.broadcastChange()
	writeJSON(w, struct {
		timeline.Snapshot
		Vault string `json:"vault"`
	}{snapshot, v.label})
}
