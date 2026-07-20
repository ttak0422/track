package note

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/ttak0422/track/internal/track/config"
)

// MetaEdit is one metadata change: a nil field is left untouched, a pointer to "" clears the
// field. It is the single write path for description/image/icon and note properties, shared by the
// CLI meta command and the web editor, so validation lives here once. Set assigns properties (value
// text parsed by ParsePropValue and checked against the configured schema); Unset removes keys.
type MetaEdit struct {
	Description *string
	Image       *string
	Icon        *string
	Set         map[string]string
	Unset       []string
}

// imageExtensions lists the cover-image formats OGP consumers actually render; anything else (an
// SVG, a PDF) errors at set time instead of publishing a broken og:image.
var imageExtensions = []string{".png", ".jpg", ".jpeg", ".webp", ".gif"}

// ApplyMetaEdit validates an edit against the vault and writes it into the note's sidecar, returning
// the resulting metadata. The image must be an existing vault asset addressed as "assets/<file>" —
// the same reference form note bodies use — with no absolute path or traversal, mirroring the
// data-source rules elsewhere. The description is stored trimmed; renderers flatten it further.
func ApplyMetaEdit(cfg *config.Config, noteID int64, edit MetaEdit) (Metadata, error) {
	metaPath := cfg.MetadataPath(noteID)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		meta = Metadata{Created: time.Now().Format(cfg.DateFormat)}
	}
	if edit.Description != nil {
		meta.Description = strings.TrimSpace(*edit.Description)
	}
	if edit.Image != nil {
		img := strings.TrimSpace(*edit.Image)
		if img != "" {
			if err := ValidateImageRef(cfg, img); err != nil {
				return Metadata{}, err
			}
		}
		meta.Image = img
	}
	if edit.Icon != nil {
		icon, err := cleanIcon(*edit.Icon)
		if err != nil {
			return Metadata{}, err
		}
		meta.Icon = icon
	}
	if len(edit.Set) > 0 && meta.Props == nil {
		meta.Props = map[string]any{}
	}
	for key, raw := range edit.Set {
		if !ValidPropKey(key) {
			return Metadata{}, fmt.Errorf("invalid property key %q (want letter, then letters/digits/_/-)", key)
		}
		value := ParsePropValue(raw)
		if violations := CheckProps(flattenValue(key, value), cfg.Properties); len(violations) > 0 {
			return Metadata{}, fmt.Errorf("property %s: %s", key, violations[0].Message)
		}
		meta.Props[key] = value
	}
	for _, key := range edit.Unset {
		delete(meta.Props, key)
	}
	if len(meta.Props) == 0 {
		meta.Props = nil
	}
	if err := WriteMetadata(metaPath, meta); err != nil {
		return Metadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return meta, nil
}

// MetaDoc is the canonical user-editable slice of a note's sidecar metadata as one YAML document:
// title, tags, page description/image, icon, and typed props. Both frontends (the web meta dialog and
// the Neovim popup) show this document verbatim and apply it through ApplyMetaDoc, so parsing and
// validation live here once. The title travels in the document but is applied by the callers
// through the engine rename path (rename.Do — backlink rewrite, uniqueness, history), which needs
// the index store this package cannot depend on.
type MetaDoc struct {
	Title       string         `yaml:"title"`
	Tags        []string       `yaml:"tags"`
	Description string         `yaml:"description"`
	Image       string         `yaml:"image"`
	Icon        string         `yaml:"icon"`
	Props       map[string]any `yaml:"props"`
}

// MetaDocYAML renders a note's editable metadata document. Empty fields stay present so an editor
// seeded from it always shows every editable key — as bare "key:" lines rather than the flow-style
// "[]" / "{}" / '""', which are hostile to hand-editing (ParseMetaDoc reads both forms).
func MetaDocYAML(meta Metadata) (string, error) {
	doc := MetaDoc{Title: meta.Title, Tags: meta.Tags, Description: meta.Description, Image: meta.Image, Icon: meta.Icon, Props: meta.Props}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	s := "\n" + string(out)
	for _, key := range []string{"title", "tags", "description", "image", "icon", "props"} {
		for _, empty := range []string{` ""`, " []", " {}"} {
			s = strings.Replace(s, "\n"+key+":"+empty+"\n", "\n"+key+":\n", 1)
		}
	}
	if clean, err := cleanIcon(meta.Icon); err == nil && clean == meta.Icon && meta.Icon != "" {
		// yaml.Marshal escapes non-ASCII scalars ("\U0001F4DA") — unreadable in a document meant for
		// hand editing. A cleanIcon-valid value is safe verbatim inside plain double quotes, so
		// re-render its line that way. A stored icon that would not pass cleanIcon today (written
		// before the gate existed, or a hand-edited sidecar) keeps the marshalled escaped form —
		// ugly but parseable, where a verbatim splice would corrupt the document.
		quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(meta.Icon) + `"`
		if i := strings.Index(s, "\nicon: "); i >= 0 {
			end := i + 1 + strings.IndexByte(s[i+1:], '\n')
			s = s[:i] + "\nicon: " + quoted + s[end:]
		}
	}
	return s[1:], nil
}

// cleanIcon normalizes a per-note icon value: trimmed, with no control characters or Unicode line
// separators. The sidecar and the editable document both render the icon as a one-line "icon:"
// entry — and MetaDocYAML re-quotes the value verbatim — so any such character would corrupt the
// document (YAML treats U+0085/U+2028/U+2029 as breaks and forbids raw control characters). It is
// the shared gate for both write paths (MetaEdit and MetaDoc).
func cleanIcon(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}) {
		return "", fmt.Errorf("icon must be a single line without control characters")
	}
	return s, nil
}

// ParseMetaDoc parses an editable metadata document strictly: unknown top-level keys (created,
// days, blocks — the non-editable sidecar fields) are rejected rather than silently dropped, so a
// typo never loses data. An empty document is valid whole-state: it clears every editable field
// (an empty title means "leave the title unchanged" — see ApplyMetaDoc).
func ParseMetaDoc(docYAML []byte) (MetaDoc, error) {
	var doc MetaDoc
	dec := yaml.NewDecoder(bytes.NewReader(docYAML))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return MetaDoc{}, fmt.Errorf("parse metadata document: %w", err)
	}
	// One document only. A second "---"-separated document would otherwise be dropped silently,
	// smuggling in fields (e.g. a forbidden title) that never reach validation. Require EOF here.
	if err := dec.Decode(new(MetaDoc)); !errors.Is(err, io.EOF) {
		if err == nil {
			return MetaDoc{}, fmt.Errorf("parse metadata document: expected a single YAML document")
		}
		return MetaDoc{}, fmt.Errorf("parse metadata document: %w", err)
	}
	return doc, nil
}

// ApplyMetaDoc replaces a note's editable sidecar fields (tags, description, image, icon, props) with the
// given YAML document, validating everything before the single sidecar write — a rejected document
// changes nothing on disk. The image must pass the same vault-asset check as ApplyMetaEdit, tags
// are trimmed and de-duplicated like every CLI tag flag, and props are typed against the configured
// schema exactly like `track meta --set`.
//
// The document's title is deliberately NOT applied here: a title change is a rename (backlink
// rewrite, uniqueness against the index), so the callers pre-validate it and route it through
// rename.Do after this write. Non-editable sidecar fields (created, days, blocks) carry over.
func ApplyMetaDoc(cfg *config.Config, noteID int64, docYAML []byte) (Metadata, error) {
	doc, err := ParseMetaDoc(docYAML)
	if err != nil {
		return Metadata{}, err
	}
	return ApplyMetaDocValue(cfg, noteID, doc)
}

// ApplyMetaDocValue is ApplyMetaDoc for an already-parsed document. It is the single validated
// apply shared by the YAML-document path (CLI --edit, Neovim popup) and the web editor's structured
// form (which composes a MetaDoc from its typed fields and a free-form props block). The title is
// deliberately not applied here — see ApplyMetaDoc — so both callers route a title change through
// rename.Do afterward.
func ApplyMetaDocValue(cfg *config.Config, noteID int64, doc MetaDoc) (Metadata, error) {
	doc.Description = strings.TrimSpace(doc.Description)
	doc.Image = strings.TrimSpace(doc.Image)
	if doc.Image != "" {
		if err := ValidateImageRef(cfg, doc.Image); err != nil {
			return Metadata{}, err
		}
	}
	if len(doc.Props) == 0 {
		doc.Props = nil
	}
	for key, value := range doc.Props {
		if !ValidPropKey(key) {
			return Metadata{}, fmt.Errorf("invalid property key %q (want letter, then letters/digits/_/-)", key)
		}
		if !scalarOrScalarList(value) {
			return Metadata{}, fmt.Errorf("property %s: value must be a scalar or a list of scalars", key)
		}
		if violations := CheckProps(flattenValue(key, value), cfg.Properties); len(violations) > 0 {
			return Metadata{}, fmt.Errorf("property %s: %s", key, violations[0].Message)
		}
	}

	metaPath := cfg.MetadataPath(noteID)
	meta, found, err := ReadMetadata(metaPath)
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		meta = Metadata{Created: time.Now().Format(cfg.DateFormat)}
	}
	icon, err := cleanIcon(doc.Icon)
	if err != nil {
		return Metadata{}, err
	}
	meta.Tags = DedupTags(doc.Tags)
	meta.Description = doc.Description
	meta.Image = doc.Image
	meta.Icon = icon
	meta.Props = doc.Props
	if err := WriteMetadata(metaPath, meta); err != nil {
		return Metadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return meta, nil
}

// scalarOrScalarList reports whether a props value has an editable shape: a YAML scalar or a
// (possibly nested) list of scalars — the shapes flattenValue indexes meaningfully. A map anywhere
// would flatten to junk in the property index, so it is rejected at apply time.
func scalarOrScalarList(v any) bool {
	switch val := v.(type) {
	case map[string]any:
		return false
	case []any:
		for _, item := range val {
			if !scalarOrScalarList(item) {
				return false
			}
		}
	}
	return true
}

// DedupTags trims and de-duplicates tags, preserving first-seen order. It returns nil for an empty
// set. This is the single tag-normalization rule, shared by the CLI tag flags and ApplyMetaDoc.
func DedupTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tags))
	var out []string
	for _, tg := range tags {
		tg = strings.TrimSpace(tg)
		if tg == "" || seen[tg] {
			continue
		}
		seen[tg] = true
		out = append(out, tg)
	}
	return out
}

// ParsePropsText parses the web editor's free-form props block — a YAML map, or "key: value" lines —
// into a property map. Empty text yields no props. A document that is not a map (a bare scalar or a
// list) is an error, surfaced to the dialog; per-key/per-value typing happens later in ApplyMetaDocValue.
func ParsePropsText(text string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var props map[string]any
	if err := yaml.Unmarshal([]byte(text), &props); err != nil {
		return nil, fmt.Errorf("parse props: %w", err)
	}
	return props, nil
}

// PropsText renders a property map back to the web editor's props block (a YAML "key: value" block),
// so the dialog seeds its free-form textarea from the stored props. No props renders as empty text.
func PropsText(props map[string]any) (string, error) {
	if len(props) == 0 {
		return "", nil
	}
	out, err := yaml.Marshal(props)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ValidateImageRef checks a cover-image reference: assets/-relative, no escape from the assets
// directory, a renderable raster format, and the file actually present. It is the one gate for a
// vault-legal cover image, shared by the metadata apply and the web asset-upload endpoint.
func ValidateImageRef(cfg *config.Config, ref string) error {
	if filepath.IsAbs(ref) || strings.Contains(ref, "..") {
		return fmt.Errorf("image %q must be a plain assets/<file> reference", ref)
	}
	rel, ok := strings.CutPrefix(filepath.ToSlash(ref), config.AssetsDirName+"/")
	if !ok || rel == "" {
		return fmt.Errorf("image %q must live under %s/ (import it with `track asset import`)", ref, config.AssetsDirName)
	}
	if !slices.Contains(imageExtensions, strings.ToLower(filepath.Ext(rel))) {
		return fmt.Errorf("image %q must be one of %s (OGP consumers do not render other formats)", ref, strings.Join(imageExtensions, " "))
	}
	if _, err := os.Stat(filepath.Join(cfg.AssetsDir(), filepath.FromSlash(rel))); err != nil {
		return fmt.Errorf("image %q not found in the vault assets: %w", ref, err)
	}
	return nil
}
