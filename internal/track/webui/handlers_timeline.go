package webui

import (
	"fmt"
	"net/http"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/task"
)

// handleTimeline exposes the same authoritative snapshot as `track timeline`.
func (s *Server) handleTimeline(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	s.refresh(v)
	ref, err := v.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	n, err := note.ReadFile(v.cfg.PathForKind(ref.FileKind, ref.NoteID), v.cfg)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	snapshot := task.NewTimeline(n.ID, n.Meta.Title, n.Body, n.Meta.TaskLog)
	writeReadJSON(w, v, map[string]any{
		"vault": v.label, "schema_version": snapshot.SchemaVersion, "note_id": snapshot.NoteID, "title": snapshot.Title,
		"items": snapshot.Items, "progress": snapshot.Progress, "history": snapshot.History,
	})
}
