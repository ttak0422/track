package note

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func metaDocConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{
		VaultDir:   t.TempDir(),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
		Properties: map[string]config.PropSpec{
			"status": {Values: []string{"draft", "done"}},
			"rating": {Type: TypeNumber},
		},
	}
	if err := os.MkdirAll(cfg.AssetsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.AssetsDir(), "cover.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestMetaDocRoundTrip drives the full editor path: GET (MetaDocYAML) produces a document that,
// applied unchanged, is a no-op; an edited document lands validated in the sidecar with the
// non-editable fields (title, created) carried over.
func TestMetaDocRoundTrip(t *testing.T) {
	cfg := metaDocConfig(t)
	if err := WriteMetadata(cfg.MetadataPath(100), Metadata{Title: "Alpha", Created: "2026-07-01"}); err != nil {
		t.Fatal(err)
	}

	meta, _, err := ReadMetadata(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := MetaDocYAML(meta)
	if err != nil {
		t.Fatalf("render doc: %v", err)
	}
	for _, key := range []string{"title: Alpha", "tags:", "description:", "image:", "icon:", "props:"} {
		if !strings.Contains(doc, key) {
			t.Fatalf("doc missing %q:\n%s", key, doc)
		}
	}
	// Empty fields render as bare "key:" lines, not the hand-editing-hostile "[]"/"{}"/`""`.
	for _, flow := range []string{"[]", "{}", `""`} {
		if strings.Contains(doc, flow) {
			t.Fatalf("empty fields must render bare, got %q in:\n%s", flow, doc)
		}
	}

	// Applying the seed document unchanged keeps the sidecar equivalent.
	if _, err := ApplyMetaDoc(cfg, 100, []byte(doc)); err != nil {
		t.Fatalf("apply unchanged doc: %v", err)
	}

	// A different title in the document is not applied by ApplyMetaDoc — a title change is a
	// rename (index uniqueness, backlink rewrite), routed through rename.Do by the callers.
	edited := "title: Renamed\n" +
		"tags:\n  - go\n  - go\n  - \" lua \"\n" +
		"description: '  a summary  '\n" +
		"image: assets/cover.png\n" +
		"icon: \" 📚 \"\n" +
		"props:\n  status: draft\n  rating: 8\n  authors: [\"[[Ada]]\", \"[[Alan]]\"]\n"
	got, err := ApplyMetaDoc(cfg, 100, []byte(edited))
	if err != nil {
		t.Fatalf("apply edited doc: %v", err)
	}
	if !reflect.DeepEqual(got.Tags, []string{"go", "lua"}) {
		t.Fatalf("tags = %v (want deduped, trimmed)", got.Tags)
	}
	if got.Description != "a summary" || got.Image != "assets/cover.png" {
		t.Fatalf("description/image = %q / %q", got.Description, got.Image)
	}
	if got.Icon != "📚" {
		t.Fatalf("icon = %q (want trimmed)", got.Icon)
	}
	if got.Props["status"] != "draft" || got.Props["rating"] != 8 {
		t.Fatalf("props = %#v", got.Props)
	}
	if got.Title != "Alpha" || got.Created != "2026-07-01" {
		t.Fatalf("title must stay rename-owned and created preserved: title=%q created=%q", got.Title, got.Created)
	}
	if parsed, err := ParseMetaDoc([]byte(edited)); err != nil || parsed.Title != "Renamed" {
		t.Fatalf("ParseMetaDoc must surface the title for the rename leg: %+v err=%v", parsed, err)
	}

	stored, found, err := ReadMetadata(cfg.MetadataPath(100))
	if err != nil || !found {
		t.Fatalf("read sidecar: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(stored.Tags, got.Tags) || stored.Description != got.Description {
		t.Fatalf("sidecar diverges from returned metadata: %#v", stored)
	}

	// Round-trip: the stored metadata renders back to a document carrying the same values.
	doc2, err := MetaDocYAML(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- go", "- lua", "a summary", "assets/cover.png", "icon: \"📚\"", "status: draft", "rating: 8"} {
		if !strings.Contains(doc2, want) {
			t.Fatalf("round-tripped doc missing %q:\n%s", want, doc2)
		}
	}
}

// TestApplyMetaDocRejectsInvalid checks every validation gate and that a rejected document is
// atomic: the sidecar on disk stays byte-identical.
func TestApplyMetaDocRejectsInvalid(t *testing.T) {
	cfg := metaDocConfig(t)
	seed := "tags:\n  - keep\nprops:\n  status: draft\n"
	if _, err := ApplyMetaDoc(cfg, 100, []byte(seed)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := os.ReadFile(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"schema enum violation":   "props:\n  status: waiting\n",
		"schema type violation":   "props:\n  rating: high\n",
		"invalid property key":    "props:\n  \"bad key\": x\n",
		"nested map property":     "props:\n  status:\n    nested: draft\n",
		"missing image asset":     "image: assets/nope.png\n",
		"non-raster image":        "image: assets/cover.svg\n",
		"unknown top-level field": "created: 2020-01-01\n",
		"malformed yaml":          "tags: [unclosed\n",
		// Control characters (and U+0085/U+2028/U+2029 line separators) inside an icon would corrupt
		// the one-line "icon:" entry MetaDocYAML re-quotes verbatim. See cleanIcon.
		"control character in icon": "icon: \"a\\ab\"\n",
		"NEL in icon":               "icon: \"a\\Nb\"\n",
		// A second "---" document must be rejected, not dropped — otherwise a forbidden title (or
		// any field) rides along past validation. See ParseMetaDoc.
		"trailing second document": "tags:\n  - keep\n---\ntitle: forbidden\n",
	}
	for name, doc := range cases {
		if _, err := ApplyMetaDoc(cfg, 100, []byte(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	after, err := os.ReadFile(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("rejected documents must change nothing; sidecar diff:\n%s\n---\n%s", before, after)
	}

	// An empty document is a valid full state: it clears every editable field.
	got, err := ApplyMetaDoc(cfg, 100, nil)
	if err != nil {
		t.Fatalf("apply empty doc: %v", err)
	}
	if got.Tags != nil || got.Props != nil || got.Description != "" || got.Image != "" || got.Icon != "" {
		t.Fatalf("empty doc should clear editable fields: %#v", got)
	}
}

// TestMetaDocYAMLLegacyIcon: a stored icon that would not pass cleanIcon today (written before the
// gate, or a hand-edited sidecar) must still render to a document ParseMetaDoc accepts — the
// readable verbatim re-quoting is skipped, not spliced into a corrupt line — so the note is never
// locked out of the editors.
func TestMetaDocYAMLLegacyIcon(t *testing.T) {
	for name, icon := range map[string]string{
		"embedded newline":  "a\nb",
		"control character": "a\x1bb",
	} {
		doc, err := MetaDocYAML(Metadata{Title: "Alpha", Icon: icon})
		if err != nil {
			t.Fatalf("%s: render: %v", name, err)
		}
		parsed, err := ParseMetaDoc([]byte(doc))
		if err != nil {
			t.Fatalf("%s: rendered doc must stay parseable, got %v:\n%s", name, err, doc)
		}
		if parsed.Icon != icon {
			t.Fatalf("%s: icon = %q (want %q)", name, parsed.Icon, icon)
		}
	}
}
