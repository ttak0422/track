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

	"github.com/ttak0422/track/internal/track/dashboard"
	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/query"
	"github.com/ttak0422/track/internal/track/rename"
	"github.com/ttak0422/track/internal/track/render"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/task"
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
	// Hierarchy navigation from the "up" relation property: the ancestor trail (root first) and the
	// notes whose "up" points here.
	trail, err := s.store.Trail(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	children, err := s.store.ChildNotes(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if trail == nil {
		trail = []store.NoteRef{}
	}
	if children == nil {
		children = []store.NoteRef{}
	}
	for _, refs := range [][]store.NoteRef{trail, children} {
		for i := range refs {
			refs[i].Path = s.cfg.PathForKind(refs[i].FileKind, refs[i].NoteID)
		}
	}
	noteJSON := map[string]any{
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
	}
	// Task lines ride along so a ```taskboard fence renders without a second request, mirroring the
	// static bundle's note JSON.
	if set := task.NewSet(body, s.cfg.TaskStates); len(set.Items) > 0 {
		noteJSON["tasks"] = set
	}
	writeJSON(w, map[string]any{
		"note":      noteJSON,
		"backlinks": backlinks,
		"trail":     trail,
		"children":  children,
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
// cover image, icon, and typed props — as structured fields. GET seeds the dialog's typed controls
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
		writeMetaFields(w, meta, ref.FileKind)
	case http.MethodPost:
		var req struct {
			Title       string   `json:"title"`
			Tags        []string `json:"tags"`
			Description string   `json:"description"`
			Image       string   `json:"image"`
			Icon        string   `json:"icon"`
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
			Icon:        req.Icon,
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
	writeJSON(w, map[string]any{
		"title":       meta.Title,
		"kind":        kind,
		"tags":        tags,
		"description": meta.Description,
		"image":       meta.Image,
		"icon":        meta.Icon,
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
	// Resolve any ```dashboard widget blocks to Markdown before sanitizing, so a home/dashboard note's
	// recent-notes, journal, and pinned widgets render live. The static export resolves the same blocks
	// at build time (see site.writeBundle), keeping the two deployments identical. The store scan for
	// widget data is skipped unless the body actually carries a dashboard fence (the common case).
	s.refreshIfStale()
	body := req.Body
	if strings.Contains(body, "```"+dashboard.Lang) {
		body = dashboard.Resolve(body, s.dashboardData())
	}
	markdown, err := export.WebBody(body)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	// Embedded ```track-query fences resolve here into Markdown result tables over the freshly
	// reconciled index, so the workspace draws them with its ordinary table rendering — the same
	// expansion the static export bakes in at build time. A row-load failure leaves the fences as
	// source rather than failing the whole render.
	if rows, err := query.RowsFromStore(s.store); err == nil {
		kinds := make(map[int64]string, len(rows))
		for _, r := range rows {
			kinds[r.ID] = r.Kind
		}
		// Gallery covers come from the sidecar metadata, read lazily per matched note; the value is
		// the note-relative "assets/<file>" the frontend already maps to /api/asset. The icon is the
		// cover's stand-in on cards without one, resolved by the one resolver every surface uses.
		markdown = query.ExpandBlocks(markdown, s.cfg.Queries, rows, func(id int64) (string, string) {
			meta, _, _ := note.ReadMetadata(s.cfg.MetadataPath(id))
			return meta.Image, s.cfg.NoteIcon(kinds[id], meta.Tags, meta.Icon)
		})
	}
	// Includes resolve against the rendered markdown (what the frontend draws), so their line
	// numbers align with the text the client splices them into; target bodies render through the
	// same web renderer so embedded content arrives as sanitized as the note's own.
	writeJSON(w, map[string]any{
		"markdown": markdown,
		"includes": link.ResolveIncludes(markdown, s.loadRenderedNote),
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
	markdown, err := export.WebBody(body)
	if err != nil {
		return 0, "", "", false
	}
	return ref.NoteID, ref.FileKind, markdown, true
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
