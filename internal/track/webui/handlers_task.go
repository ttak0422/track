package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
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
	// Without an id this is the vault-wide listing. By default it is what the calendar and day pages
	// read: every task carrying a date, so a planned day is visible without opening the note that
	// planned it. ?open=1 asks the other question — everything still to do, dated or not — which is
	// most of a project note's checklist and is invisible under the dated filter.
	if strings.TrimSpace(r.URL.Query().Get("id")) == "" {
		open := r.URL.Query().Get("open") == "1"
		rows, err := v.store.Tasks(store.TaskFilter{Dated: !open, Open: open, ByPriority: open})
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		if rows == nil {
			rows = []store.TaskRow{}
		}
		// The vault label qualifies each row's note id client-side, as the journal and search
		// responses do — two vaults hold different notes under the same number.
		writeJSON(w, map[string]any{"vault": v.label, "tasks": rows})
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
		ETag  string `json:"etag"`
		// The state the client believes the line is in. The board, the rendered task rows, and the
		// table's date cells all know it, so they send it and a stale view is refused (409) instead
		// of writing over whatever the line became — a date patch on its own included.
		Expect string `json:"expect"`
		// Date patches. Pointers so "set it to empty" (clear the token) is distinguishable from
		// "leave it alone"; State may be empty when only a date is being written.
		Sched *string `json:"sched"`
		Due   *string `json:"due"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ETag) == "" {
		writeError(w, errors.New("etag is required"), http.StatusBadRequest)
		return
	}

	path := v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	var tr task.Transition
	var updatedRaw []byte
	if err := v.write(func() error {
		var err error
		tr, err = note.ApplyTaskPatch(v.cfg, path, note.TaskPatch{
			Line: req.Line, State: req.State, Expect: req.Expect,
			Sched: req.Sched, Due: req.Due, ETag: req.ETag,
		}, time.Now())
		if err != nil {
			return err
		}
		if err := index.New(v.cfg, v.store).One(path); err != nil {
			return err
		}
		updatedRaw, err = os.ReadFile(path)
		return err
	}); err != nil {
		// A refused assertion is a conflict, not a bad request — the same shape the note save's
		// etag mismatch returns, one level down.
		if errors.Is(err, note.ErrStateMismatch) || errors.Is(err, note.ErrETagMismatch) {
			writeError(w, err, http.StatusConflict)
			return
		}
		writeError(w, err, http.StatusBadRequest)
		return
	}
	set, err := v.noteTasks(ref.FileKind, ref.NoteID)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"tasks": set, "transition": tr, "etag": note.ContentETag(updatedRaw)})
}

// noteTasks reads a note file and parses its task lines with the configured state set.
func (v *vaultView) noteTasks(fileKind string, id int64) (task.Set, error) {
	raw, err := os.ReadFile(v.cfg.PathForKind(fileKind, id))
	if err != nil {
		return task.Set{}, err
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	return task.NewSet(body), nil
}
