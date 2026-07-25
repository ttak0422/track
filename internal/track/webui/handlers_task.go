package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/task"
)

// handleTasks returns a note's parsed task lines plus the vault's state set — the data the board view
// draws its columns and cards from. Line numbers are 1-based over the note file, the same coordinates
// POST /api/task and the CLI use.
func (s *Server) handleTasks(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != "" {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	s.refresh(v)
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	ref, err := v.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	set, err := v.noteTasks(ref.FileKind, ref.NoteID)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"tasks": set})
}

// handleTaskSet moves one task line into a named state through the same engine write path as the CLI
// (note.ApplyTaskState): completion stamp, sidecar transition log, cookie recompute. It responds with
// the note's refreshed tasks so the board can redraw without a second request.
func (s *Server) handleTaskSet(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	ref, err := v.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	var req struct {
		Line  int    `json:"line"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}

	path := v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	tr, err := note.ApplyTaskState(v.cfg, path, req.Line, req.State, time.Now())
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if err := index.New(v.cfg, v.store).One(path); err != nil {
		writeError(w, fmt.Errorf("reindex: %w", err), http.StatusInternalServerError)
		return
	}
	set, err := v.noteTasks(ref.FileKind, ref.NoteID)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"tasks": set, "transition": tr})
}

// noteTasks reads a note file and parses its task lines with the configured state set.
func (v *vaultView) noteTasks(fileKind string, id int64) (task.Set, error) {
	raw, err := os.ReadFile(v.cfg.PathForKind(fileKind, id))
	if err != nil {
		return task.Set{}, err
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	return task.NewSet(body, v.cfg.TaskStates), nil
}
