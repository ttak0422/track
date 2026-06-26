package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, "":
		s.getNote(w, r)
	case http.MethodPut:
		s.putNote(w, r)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *Server) getNote(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	ref, err := s.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	path := s.cfg.PathForKind(ref.FileKind, ref.NoteID)
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	backlinks, err := s.store.Backlinks(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if backlinks == nil {
		backlinks = []store.NoteRef{}
	}
	for i := range backlinks {
		backlinks[i].Path = s.cfg.PathForKind(backlinks[i].FileKind, backlinks[i].NoteID)
	}
	writeJSON(w, map[string]any{
		"note": map[string]any{
			"note_id":   ref.NoteID,
			"file_kind": ref.FileKind,
			"path":      path,
			"copy_path": s.cfg.DisplayPathForKind(ref.FileKind, ref.NoteID),
			"title":     ref.Title,
			"tags":      ref.Tags,
			"body":      body,
			// etag is a content hash of the file as read; clients echo it back on PUT so a save can be
			// rejected when the file changed underneath (e.g. an OneDrive sync) since this read.
			"etag": etagFor(raw),
		},
		"backlinks": backlinks,
	})
}

// putNote saves the body of an existing note. The request JSON carries the new body and the etag the
// client last read; if the file changed on disk since then the save is refused with 409 so a cloud-sync
// update is not silently overwritten. Titles stay sidecar-authoritative, so only the body is touched.
//
// TODO(track): the web frontend has no editor UI yet (textarea/keymap/save affordance) and PUT cannot
// create new notes. Both are deferred follow-ups; this is the save+conflict-detection backend slice only.
func (s *Server) putNote(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	ref, err := s.noteByID(id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	var req struct {
		Body string `json:"body"`
		ETag string `json:"etag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}

	path := s.cfg.PathForKind(ref.FileKind, ref.NoteID)
	current, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if req.ETag == "" {
		writeError(w, errors.New("etag is required to detect conflicts"), http.StatusBadRequest)
		return
	}
	if req.ETag != etagFor(current) {
		writeError(w, errors.New("note changed on disk since it was loaded; reload before saving"), http.StatusConflict)
		return
	}

	out := []byte(ensureTrailingNewline(req.Body))
	if err := os.WriteFile(path, out, 0o644); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if err := index.New(s.cfg, s.store).One(path); err != nil {
		writeError(w, fmt.Errorf("reindex: %w", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"note_id": ref.NoteID, "etag": etagFor(out), "saved": true})
}

// handleRender sanitizes a raw note body into the Markdown the frontend renders: track action links
// (editor-only, not web-navigable) are flattened to plain text while wiki links, code, and ordinary
// Markdown pass through. Keeping this on the server makes the engine the single source of truth for
// track-specific Markdown semantics, and lets the editor preview the live (unsaved) body by posting it
// here rather than re-implementing the rules in the frontend.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	res, err := export.Export(&note.Note{Body: req.Body}, export.NewWebRenderer(), export.Options{})
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"markdown": res.Markdown})
}

// etagFor returns a short content hash used as an optimistic-concurrency token for note bodies.
func etagFor(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// ensureTrailingNewline mirrors the CLI's write behavior so saved bodies end with exactly one newline.
func ensureTrailingNewline(body string) string {
	if body == "" {
		return ""
	}
	if strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}
