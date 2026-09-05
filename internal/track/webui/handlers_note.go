package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
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
	tmpl "github.com/ttak0422/track/internal/track/template"
	"github.com/ttak0422/track/internal/track/vaultref"
)

func (s *Server) handleNote(v *vaultView, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, "":
		s.getNote(v, w, r)
	case http.MethodPost:
		s.postNote(v, w, r)
	case http.MethodPut:
		s.putNote(v, w, r)
	case http.MethodDelete:
		s.deleteNote(v, w, r)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

// postNote creates a note titled req.Title with the default template and
// indexes it, mirroring the CLI's `new`. Titles are link keywords, so a title
// that already resolves is refused with 409 rather than minting an ambiguous
// duplicate.
func (s *Server) postNote(v *vaultView, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, errors.New("title is required"), http.StatusBadRequest)
		return
	}
	if _, found, err := v.store.ResolveTerm(title); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	} else if found {
		writeError(w, fmt.Errorf("note already exists for title %q", title), http.StatusConflict)
		return
	}
	noteID, err := note.NewID(v.cfg, time.Now())
	if err != nil {
		writeError(w, fmt.Errorf("allocate note id: %w", err), http.StatusInternalServerError)
		return
	}
	path := v.cfg.NotePath(noteID)
	if _, err := os.Stat(path); err == nil {
		writeError(w, fmt.Errorf("note already exists: %s", path), http.StatusConflict)
		return
	}
	spec, err := tmpl.DefaultSpec(v.cfg, config.KindNote)
	if err != nil {
		writeError(w, fmt.Errorf("resolve default template: %w", err), http.StatusInternalServerError)
		return
	}
	rendered, err := tmpl.Render(v.cfg, spec, title, noteID, config.KindNote, "", time.Now())
	if err != nil {
		writeError(w, fmt.Errorf("render template: %w", err), http.StatusInternalServerError)
		return
	}
	body := ensureTrailingNewline(rendered)
	if err := v.write(func() error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create note dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("write note: %w", err)
		}
		if err := note.WriteMetadata(
			v.cfg.MetadataPath(noteID),
			note.Metadata{Title: title, Created: time.Now().Format(v.cfg.DateFormat)},
		); err != nil {
			return fmt.Errorf("write metadata: %w", err)
		}
		return index.New(v.cfg, v.store).One(path)
	}); err != nil {
		writeError(w, fmt.Errorf("create note: %w", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"vault": v.label, "note_id": noteID, "title": title, "created": true})
}

// deleteNote removes a note: its Markdown file, its sidecar metadata, and its index row (tags and links
// cascade). Other notes' bodies keep their now-dangling [[links]]; the link rows pointing here are
// dropped with the note, so the graph and backlinks stay consistent. The destructive confirmation
// (typing the title) is enforced in the web UI; this endpoint deletes by id.
func (s *Server) deleteNote(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
	path := v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	if err := v.write(func() error {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove note file: %w", err)
		}
		if err := os.Remove(v.cfg.MetadataPath(id)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove note metadata: %w", err)
		}
		if err := v.store.DeleteNote(id); err != nil {
			return fmt.Errorf("delete from index: %w", err)
		}
		return nil
	}); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"vault": v.label, "note_id": ref.NoteID, "deleted": true})
}

func (s *Server) getNote(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
	path := v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	backlinks, err := v.store.Backlinks(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if backlinks == nil {
		backlinks = []store.NoteRef{}
	}
	addRefPaths(v, backlinks)
	// Properties come from the index (refreshed above), which flattens sidecar props and inline
	// "key:: value" fields through the same engine path everything else uses.
	props, err := v.store.NoteProps(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if props == nil {
		props = []note.Prop{}
	}
	// Hierarchy navigation from the "up" relation property: the ancestor trail (root first) and the
	// notes whose "up" points here.
	trail, err := v.store.Trail(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	children, err := v.store.ChildNotes(id)
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
	addRefPaths(v, trail)
	addRefPaths(v, children)
	noteJSON := map[string]any{
		"vault":     v.label,
		"note_id":   ref.NoteID,
		"file_kind": ref.FileKind,
		"path":      path,
		"copy_path": v.cfg.DisplayPathForKind(ref.FileKind, ref.NoteID),
		"title":     ref.Title,
		"tags":      ref.Tags,
		"props":     props,
		"body":      body,
		// etag is a content hash of the file as read; clients echo it back on PUT so a save can be
		// rejected when the file changed underneath (e.g. an OneDrive sync) since this read.
		"etag": note.ContentETag(raw),
	}
	// Timestamps: created is the sidecar string verbatim (its format is config.DateFormat, and
	// `track export --frontmatter` emits the same string — reformatting here would let the two
	// diverge), updated is the file mtime the index already carries. The reading milestones ride
	// along as unix seconds so a freshly opened browser adopts what other devices already recorded.
	// Each is omitted when absent, including when the sidecar cannot be read: a note stays readable
	// without its dates.
	if meta, _, err := note.ReadMetadata(v.cfg.MetadataPath(id)); err == nil {
		if meta.Created != "" {
			noteJSON["created"] = meta.Created
		}
		if sec := note.StampUnix(meta.SeenAt); sec != 0 {
			noteJSON["seen_at"] = sec
		}
		if sec := note.StampUnix(meta.ReadAt); sec != 0 {
			noteJSON["read_at"] = sec
		}
		// Flags ride along as the normalized slice (empty, not null) so the stamp and badges draw
		// from vault metadata; when the sidecar cannot be read the field stays absent like the dates.
		flags := meta.Flags
		if flags == nil {
			flags = []string{}
		}
		noteJSON["flags"] = flags
	}
	if ref.Mtime != 0 {
		noteJSON["updated"] = ref.Mtime
	}
	// Task lines ride along so a ```taskboard fence renders without a second request, mirroring the
	// static bundle's note JSON.
	if set := task.NewSet(body); len(set.Items) > 0 {
		noteJSON["tasks"] = set
	}
	out := map[string]any{
		"note":      noteJSON,
		"backlinks": backlinks,
		"trail":     trail,
		"children":  children,
	}
	// Inbound references can also live in other vaults, written [[name:title]] and stored as string
	// edges keyed by this note's title (ADR 0053). They are in those vaults' indexes, so they are a
	// separate lookup; a vault that cannot be consulted is reported, because a missing backlink must
	// stay distinguishable from a missing vault. The CLI's `track backlinks` answers the same way.
	if external, unavailable, ok := s.externalBacklinks(v, ref.Title); ok {
		out["external"] = external
		out["unavailable"] = unavailable
	}
	writeJSON(w, out)
}

// externalBacklinks lists the [[vault:title]] references other vaults make to a title, or reports
// ok=false when the workspace serves no registry and the question cannot arise. It reads through the
// server's own views rather than opening each vault per request the way the one-shot CLI does: this
// runs on every note open, and the handles are already here.
func (s *Server) externalBacklinks(v *vaultView, title string) ([]vaultref.ExternalRef, []vaultInfo, bool) {
	if len(v.cfg.Vaults) == 0 || title == "" {
		return nil, nil, false
	}
	// References name this vault by its registry name, which the view already carries: for the launch
	// vault it is what New labelled it with, and for every other view it is the name it was opened by.
	external := []vaultref.ExternalRef{}
	if v.name == "" {
		// Unregistered: no other vault has a name to refer to this one by. Returning here also keeps a
		// plain single-vault note open off servedViews, which stats every registered vault.
		return external, []vaultInfo{}, true
	}
	views, unavailable := s.servedViews()
	if unavailable == nil {
		unavailable = []vaultInfo{}
	}
	for _, other := range views {
		if other == v {
			continue // its own references are the ordinary backlinks
		}
		// Only the launch vault is watched, so a read is the only thing that ever notices another
		// vault's edits (vaults.go). Without this the reference a sibling vault just wrote would stay
		// invisible here for as long as the workspace runs. refresh throttles per vault and takes that
		// vault's reindex lock, so the extra call costs nothing on a warm index.
		s.refresh(other)
		backs, err := other.store.ExtBacklinks([]string{v.name}, title)
		if err != nil {
			unavailable = append(unavailable, vaultInfo{Name: other.name, Path: other.cfg.VaultDirDisplay, Error: err.Error()})
			continue
		}
		for _, b := range backs {
			external = append(external, vaultref.ExternalRef{
				Vault:    other.name,
				NoteID:   b.NoteID,
				FileKind: b.FileKind,
				Title:    b.Title,
				Path:     other.cfg.PathForKind(b.FileKind, b.NoteID),
			})
		}
	}
	return external, unavailable, true
}

// putNote saves the body of an existing note. The request JSON carries the new body and the etag the
// client last read; if the file changed on disk since then the save is refused with 409 so a cloud-sync
// update is not silently overwritten. Titles stay sidecar-authoritative, so only the body is touched.
//
// TODO(track): the web frontend has no editor UI yet (textarea/keymap/save affordance) and PUT cannot
// create new notes. Both are deferred follow-ups; this is the save+conflict-detection backend slice only.
func (s *Server) putNote(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
		Body string `json:"body"`
		ETag string `json:"etag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}

	path := v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	current, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if req.ETag == "" {
		writeError(w, errors.New("etag is required to detect conflicts"), http.StatusBadRequest)
		return
	}
	if req.ETag != note.ContentETag(current) {
		writeError(w, errors.New("note changed on disk since it was loaded; reload before saving"), http.StatusConflict)
		return
	}

	out := []byte(ensureTrailingNewline(req.Body))
	if err := v.write(func() error {
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
		return index.New(v.cfg, v.store).One(path)
	}); err != nil {
		writeError(w, fmt.Errorf("save note: %w", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"vault": v.label, "note_id": ref.NoteID, "etag": note.ContentETag(out), "saved": true})
}

// handleNoteRead records a shared reading milestone on a note's sidecar: "seen" when the workspace
// opened the note on any device, "read" when viewing time there crossed its read threshold. Both are
// monotonic firsts (note.ApplyReadEvent), so whichever device reaches a milestone first wins and a
// repeat report is a no-op that skips both the sidecar write and the reindex. This is what moves the
// NEW/read badges from per-browser localStorage onto vault metadata that syncs across devices
// (ADR 0072); no etag is required because the fields only ever move forward.
func (s *Server) handleNoteRead(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
		Event string `json:"event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
		return
	}
	if req.Event != "seen" && req.Event != "read" {
		writeError(w, fmt.Errorf("event must be \"seen\" or \"read\", got %q", req.Event), http.StatusBadRequest)
		return
	}

	metaPath := v.cfg.MetadataPath(id)
	path := v.cfg.PathForKind(ref.FileKind, ref.NoteID)
	if err := v.write(func() error {
		meta, _, err := note.ReadMetadata(metaPath)
		if err != nil {
			return fmt.Errorf("read sidecar: %w", err)
		}
		if !meta.ApplyReadEvent(req.Event, time.Now()) {
			return nil
		}
		if err := note.WriteMetadata(metaPath, meta); err != nil {
			return fmt.Errorf("write sidecar: %w", err)
		}
		return index.New(v.cfg, v.store).One(path)
	}); err != nil {
		writeError(w, fmt.Errorf("record read state: %w", err), http.StatusInternalServerError)
		return
	}
	meta, _, _ := note.ReadMetadata(metaPath)
	out := map[string]any{"vault": v.label, "note_id": ref.NoteID}
	if sec := note.StampUnix(meta.SeenAt); sec != 0 {
		out["seen_at"] = sec
	}
	if sec := note.StampUnix(meta.ReadAt); sec != 0 {
		out["read_at"] = sec
	}
	writeJSON(w, out)
}

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
		meta, _, err := note.ReadMetadata(v.cfg.MetadataPath(id))
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
			Flags       []string `json:"flags"`
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
			Flags:       req.Flags,
		}
		// Pre-validate a title change so a conflicting title rejects the whole edit before any
		// write; an empty title means "leave the title unchanged".
		newTitle := strings.TrimSpace(doc.Title)
		if newTitle != "" {
			if other, ok, err := v.store.ResolveTerm(newTitle); err != nil {
				writeError(w, err, http.StatusInternalServerError)
				return
			} else if ok && other.NoteID != id {
				writeError(w, fmt.Errorf("title %q already in use by note %d", newTitle, other.NoteID), http.StatusBadRequest)
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
	writeJSON(w, map[string]any{
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

// handleRender sanitizes a raw note body into the Markdown the frontend renders: track action links
// (editor-only, not web-navigable) are flattened to plain text while wiki links, code, and ordinary
// Markdown pass through. Keeping this on the server makes the engine the single source of truth for
// track-specific Markdown semantics, and lets the editor preview the live (unsaved) body by posting it
// here rather than re-implementing the rules in the frontend.
func (s *Server) handleRender(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
	s.refresh(v)
	body := req.Body
	if strings.Contains(body, "```"+dashboard.Lang) {
		body = dashboard.Resolve(body, v.dashboardData())
	}
	markdown, err := export.WebBody(body)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	// Embedded ```track-query fences resolve here into Markdown result tables over the freshly
	// reconciled index, so the workspace draws them with its ordinary table rendering — the same
	// expansion the static export bakes in at build time. A row-load failure leaves the fences as
	// source rather than failing the whole render. Like the dashboard block above, the row scan is
	// skipped unless the body carries a query fence: the editor re-renders per keystroke, and
	// almost no note has one. The test is on the rendered markdown, not the request body, because
	// a dashboard fence can expand into query fences.
	if strings.Contains(markdown, "```"+query.FenceLang) {
		if rows, err := query.RowsFromStore(v.store); err == nil {
			kinds := make(map[int64]string, len(rows))
			for _, r := range rows {
				kinds[r.ID] = r.Kind
			}
			// Gallery covers come from the sidecar metadata, read lazily per matched note; the value is
			// the note-relative "assets/<file>" the frontend already maps to /api/asset. The icon is the
			// cover's stand-in on cards without one, resolved by the one resolver every surface uses.
			markdown = query.ExpandBlocks(markdown, v.cfg.Queries, rows, func(id int64) (string, string) {
				meta, _, _ := note.ReadMetadata(v.cfg.MetadataPath(id))
				return meta.Image, v.cfg.NoteIcon(kinds[id], meta.Tags, meta.Icon)
			})
		}
	}
	// Includes resolve against the rendered markdown (what the frontend draws), so their line
	// numbers align with the text the client splices them into; target bodies render through the
	// same web renderer so embedded content arrives as sanitized as the note's own.
	writeJSON(w, map[string]any{
		"markdown": markdown,
		"includes": link.ResolveIncludes(markdown, v.loadRenderedNote),
	})
}

// loadRenderedNote resolves a link key to a note and returns its web-rendered body, for include
// resolution. Any failure (unknown key, unreadable file, render error) reads as "not found" — the
// include renders as unresolved rather than surfacing a partial embed.
func (v *vaultView) loadRenderedNote(key string) (int64, string, string, string, bool) {
	ref, found, err := v.store.ResolveTerm(key)
	if err != nil || !found {
		return 0, "", "", "", false
	}
	raw, err := os.ReadFile(v.cfg.PathForKind(ref.FileKind, ref.NoteID))
	if err != nil {
		return 0, "", "", "", false
	}
	body, _, _ := note.SplitLegacyFootmatter(string(raw))
	markdown, err := export.WebBody(body)
	if err != nil {
		return 0, "", "", "", false
	}
	return ref.NoteID, ref.FileKind, markdown, note.ContentETag(raw), true
}

// handleViewSpec resolves a fenced ```viewspec block (a View Spec JSON) to its ECharts option JSON,
// which the frontend hands to its own ECharts instance — the engine stays the single source of truth
// for chart semantics while the embedded chart is interactive. data.source references resolve inside
// the vault's data/ directory (render.EChartsOptionFromSpecDir confines them there). A bad spec is a
// client error: the frontend shows the message at the block position instead of a chart.
func (s *Server) handleViewSpec(v *vaultView, w http.ResponseWriter, r *http.Request) {
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
	opt, err := render.EChartsOptionFromSpecDir([]byte(req.Spec), v.cfg.DataDir())
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"echarts": json.RawMessage(opt)})
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
