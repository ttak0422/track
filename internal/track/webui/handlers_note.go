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
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/rename"
	"github.com/ttak0422/track/internal/track/render"
	"github.com/ttak0422/track/internal/track/store"
)

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, "":
		s.getNote(w, r)
	case http.MethodPut:
		s.putNote(w, r)
	case http.MethodDelete:
		s.deleteNote(w, r)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

// deleteNote removes a note: its Markdown file, its sidecar metadata, and its index row (tags and links
// cascade). Other notes' bodies keep their now-dangling [[links]]; the link rows pointing here are
// dropped with the note, so the graph and backlinks stay consistent. The destructive confirmation
// (typing the title) is enforced in the web UI; this endpoint deletes by id.
func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
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
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeError(w, fmt.Errorf("remove note file: %w", err), http.StatusInternalServerError)
		return
	}
	if err := os.Remove(s.cfg.MetadataPath(id)); err != nil && !os.IsNotExist(err) {
		writeError(w, fmt.Errorf("remove note metadata: %w", err), http.StatusInternalServerError)
		return
	}
	if err := s.store.DeleteNote(id); err != nil {
		writeError(w, fmt.Errorf("delete from index: %w", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"note_id": ref.NoteID, "deleted": true})
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
	// Properties come from the index (refreshed above), which flattens sidecar props and inline
	// "key:: value" fields through the same engine path everything else uses.
	props, err := s.store.NoteProps(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if props == nil {
		props = []note.Prop{}
	}
	writeJSON(w, map[string]any{
		"note": map[string]any{
			"note_id":   ref.NoteID,
			"file_kind": ref.FileKind,
			"path":      path,
			"copy_path": s.cfg.DisplayPathForKind(ref.FileKind, ref.NoteID),
			"title":     ref.Title,
			"tags":      ref.Tags,
			"props":     props,
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

// handleNoteMeta reads or edits a note's editable sidecar metadata — title, tags, description,
// cover image, and typed props — as structured fields. GET seeds the dialog's typed controls
// (props as a free-form YAML "key: value" block); POST takes those fields back, composes a
// document, and applies it through the same validated engine path as `track meta --edit`
// (note.ApplyMetaDocValue; a changed title through rename.Do). Every rule — tag normalization, an
// existing vault asset in a raster format, props typed against the configured schema, title
// uniqueness — lives in the engine, so the frontend never assembles YAML: a violation is a 400
// whose message the editor shows inline, and a rejected edit changes nothing.
func (s *Server) handleNoteMeta(w http.ResponseWriter, r *http.Request) {
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
	switch r.Method {
	case http.MethodGet, "":
		meta, _, err := note.ReadMetadata(s.cfg.MetadataPath(id))
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeMetaFields(w, meta)
	case http.MethodPost:
		var req struct {
			Title       string   `json:"title"`
			Tags        []string `json:"tags"`
			Description string   `json:"description"`
			Image       string   `json:"image"`
			Props       string   `json:"props"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
			return
		}
		props, err := note.ParsePropsText(req.Props)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		doc := note.MetaDoc{
			Title:       req.Title,
			Tags:        req.Tags,
			Description: req.Description,
			Image:       req.Image,
			Props:       props,
		}
		// Pre-validate a title change so a conflicting title rejects the whole edit before any
		// write; an empty title means "leave the title unchanged".
		newTitle := strings.TrimSpace(doc.Title)
		if newTitle != "" {
			if other, ok, err := s.store.ResolveTerm(newTitle); err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			} else if ok && other.NoteID != id {
				writeError(w, fmt.Errorf("title %q already in use by note %d", newTitle, other.NoteID), http.StatusBadRequest)
				return
			}
		}
		meta, err := note.ApplyMetaDocValue(s.cfg, id, doc)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		if newTitle != "" && newTitle != meta.Title {
			// A title change is a rename: backlink rewrite, history, full reindex — the same engine
			// path as `track rename`.
			if _, err := rename.Do(s.cfg, s.store, id, newTitle); err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			meta.Title = newTitle
		} else if err := index.New(s.cfg, s.store).One(s.cfg.PathForKind(ref.FileKind, ref.NoteID)); err != nil {
			writeError(w, fmt.Errorf("reindex: %w", err), http.StatusInternalServerError)
			return
		}
		writeMetaFields(w, meta)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

// writeMetaFields serializes a note's editable metadata as the dialog's typed fields, rendering the
// props map back to the free-form YAML block the props textarea seeds from.
func writeMetaFields(w http.ResponseWriter, meta note.Metadata) {
	propsText, err := note.PropsText(meta.Props)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	tags := meta.Tags
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, map[string]any{
		"title":       meta.Title,
		"tags":        tags,
		"description": meta.Description,
		"image":       meta.Image,
		"props":       propsText,
	})
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
	// Includes resolve against the rendered markdown (what the frontend draws), so their line
	// numbers align with the text the client splices them into; target bodies render through the
	// same web renderer so embedded content arrives as sanitized as the note's own.
	s.refreshIfStale()
	writeJSON(w, map[string]any{
		"markdown": res.Markdown,
		"includes": link.ResolveIncludes(res.Markdown, s.loadRenderedNote),
	})
}

// loadRenderedNote resolves a link key to a note and returns its web-rendered body, for include
// resolution. Any failure (unknown key, unreadable file, render error) reads as "not found" — the
// include renders as unresolved rather than surfacing a partial embed.
func (s *Server) loadRenderedNote(key string) (int64, string, string, bool) {
	ref, found, err := s.store.ResolveTerm(key)
	if err != nil || !found {
		return 0, "", "", false
	}
	raw, err := os.ReadFile(s.cfg.PathForKind(ref.FileKind, ref.NoteID))
	if err != nil {
		return 0, "", "", false
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	res, err := export.Export(&note.Note{Body: body}, export.NewWebRenderer(), export.Options{})
	if err != nil {
		return 0, "", "", false
	}
	return ref.NoteID, ref.FileKind, res.Markdown, true
}

// handleViewSpec resolves a fenced ```viewspec block (a View Spec JSON) to its ECharts option JSON,
// which the frontend hands to its own ECharts instance — the engine stays the single source of truth
// for chart semantics while the embedded chart is interactive. data.source references resolve inside
// the vault's data/ directory (render.EChartsOptionFromSpecDir confines them there). A bad spec is a
// client error: the frontend shows the message at the block position instead of a chart.
func (s *Server) handleViewSpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Spec string `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	opt, err := render.EChartsOptionFromSpecDir([]byte(req.Spec), s.cfg.DataDir())
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"echarts": json.RawMessage(opt)})
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
