package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/rename"
)

// handleNoteMeta reads or edits a note's editable sidecar metadata — title, tags, description,
// cover image, icon, and typed props — as structured fields. GET seeds the dialog's typed controls
// (props as a free-form YAML "key: value" block); POST takes those fields back, composes a
// document, and applies it through the same validated engine path as `track meta --edit`
// (note.ApplyMetaDocValue; a changed title through rename.Do). Every rule — tag normalization, an
// existing vault asset in a raster format, props typed against the configured schema, title
// uniqueness — lives in the engine, so the frontend never assembles YAML: a violation is a 400
// whose message the editor shows inline, and a rejected edit changes nothing.
func (s *Server) handleNoteMeta(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
	switch r.Method {
	case http.MethodGet, "":
		v.reindexMu.Lock()
		defer v.reindexMu.Unlock()
		meta, _, err := note.ReadMetadata(v.cfg.MetadataPath(id))
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeMetaFields(w, meta, ref.FileKind)
	case http.MethodPost:
		var req struct {
			ETag        *string  `json:"etag"`
			Title       string   `json:"title"`
			Tags        []string `json:"tags"`
			Description string   `json:"description"`
			Image       string   `json:"image"`
			Icon        string   `json:"icon"`
			Props       string   `json:"props"`
			Flags       []string `json:"flags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
			return
		}
		// Serialize the snapshot check, edit and reindex with server writes.
		// External CLI/cloud writers do not take this lock.
		v.reindexMu.Lock()
		defer v.reindexMu.Unlock()
		if req.ETag != nil {
			current, _, err := note.ReadMetadata(v.cfg.MetadataPath(id))
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			etag, err := metaFieldsETag(current)
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			if *req.ETag != etag {
				writeError(w, errors.New("metadata changed since it was loaded; reload before saving"), http.StatusConflict)
				return
			}
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
			Icon:        req.Icon,
			Props:       props,
			Flags:       req.Flags,
		}
		// Pre-validate a title change so a conflicting title rejects the whole edit before any
		// write; an empty title means "leave the title unchanged".
		newTitle := strings.TrimSpace(doc.Title)
		if newTitle != "" {
			if err := config.ValidateLinkTitle(v.cfg.LinkFormat, newTitle); err != nil {
				writeError(w, err, http.StatusBadRequest)
				return
			}
			if other, ok, err := v.store.ResolveTerm(newTitle); err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			} else if ok && other.NoteID != id {
				writeError(w, fmt.Errorf("title %q already in use by note %d", newTitle, other.NoteID), http.StatusBadRequest)
				return
			}
		}
		if ref.FileKind == config.KindJournal && newTitle != "" {
			current, _, err := note.ReadMetadata(v.cfg.MetadataPath(id))
			if err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			if newTitle != current.Title {
				writeError(w, errors.New("a journal title is derived from its date and cannot be renamed"), http.StatusBadRequest)
				return
			}
		}
		meta, err := note.ApplyMetaDocValue(v.cfg, id, doc)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		if newTitle != "" && newTitle != meta.Title {
			// A title change is a rename: backlink rewrite, history, full reindex — the same engine
			// path as `track rename`.
			if _, err := rename.Do(v.cfg, v.store, id, newTitle); err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			}
			meta.Title = newTitle
		} else if err := index.New(v.cfg, v.store).One(v.cfg.PathForKind(ref.FileKind, ref.NoteID)); err != nil {
			writeError(w, fmt.Errorf("reindex: %w", err), http.StatusInternalServerError)
			return
		}
		writeMetaFields(w, meta, ref.FileKind)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

// writeMetaFields serializes a note's editable metadata as the dialog's typed fields, rendering the
// props map back to the free-form YAML block the props textarea seeds from. kind is the note's file
// kind (config.KindNote / KindJournal); the dialog disables title editing for journals, whose titles
// are derived from their date.
func writeMetaFields(w http.ResponseWriter, meta note.Metadata, kind string) {
	propsText, err := note.PropsText(meta.Props)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	tags := meta.Tags
	if tags == nil {
		tags = []string{}
	}
	flags := meta.Flags
	if flags == nil {
		flags = []string{}
	}
	etag, err := metaFieldsETag(meta)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"etag":        etag,
		"title":       meta.Title,
		"kind":        kind,
		"tags":        tags,
		"description": meta.Description,
		"image":       meta.Image,
		"icon":        meta.Icon,
		"flags":       flags,
		"props":       propsText,
	})
}

// Hash only editable fields: reading milestones and activity can change while
// a dialog is open and are preserved by ApplyMetaDocValue.
func metaFieldsETag(meta note.Metadata) (string, error) {
	doc, err := note.MetaDocYAML(meta)
	if err != nil {
		return "", err
	}
	return note.ContentETag([]byte(doc)), nil
}
