package note

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeFlags(t *testing.T) {
	// Normalization: trimmed and uppercased, deduplicated, sorted into the registry's canonical
	// order — the ADR's [DEPRECATED, CONFIDENTIAL], not lexicographic order.
	got, err := NormalizeFlags([]string{"  confidential ", "DEPRECATED", "deprecated", "CONFIDENTIAL"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if want := []Flag{FlagDeprecated, FlagConfidential}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flags = %v, want %v", got, want)
	}

	// A single flag normalizes alone.
	if got, err := NormalizeFlags([]string{" deprecated "}); err != nil || !reflect.DeepEqual(got, []Flag{FlagDeprecated}) {
		t.Fatalf("single flag = %v, %v", got, err)
	}

	// Empty input returns nil so the sidecar field stays omitted (omitempty).
	if got, err := NormalizeFlags(nil); err != nil || got != nil {
		t.Fatalf("nil input = %v, %v; want nil, nil", got, err)
	}
	if got, err := NormalizeFlags([]string{}); err != nil || got != nil {
		t.Fatalf("empty input = %v, %v; want nil, nil", got, err)
	}

	// The closed set is fixed and known in canonical order.
	if want := []Flag{FlagDeprecated, FlagConfidential}; !reflect.DeepEqual(KnownFlags(), want) {
		t.Fatalf("KnownFlags() = %v, want %v", KnownFlags(), want)
	}
}

func TestNormalizeFlagsRejectsUnknown(t *testing.T) {
	for _, in := range [][]string{
		{"SECRET"},
		{"deprecated", "foo"},
		{""}, // an empty value is not in the closed set
	} {
		if _, err := NormalizeFlags(in); err == nil {
			t.Errorf("NormalizeFlags(%v) should reject a value outside the closed set", in)
		} else if !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("error should name the offending value: %v", err)
		}
	}
}

func TestApplyMetaEditAddsAndRemovesFlags(t *testing.T) {
	cfg := metaDocConfig(t)
	meta, err := ApplyMetaEdit(cfg, 100, MetaEdit{FlagAdd: []string{"deprecated", "DEPRECATED", "CONFIDENTIAL"}})
	if err != nil {
		t.Fatalf("add flags: %v", err)
	}
	if want := []string{"DEPRECATED", "CONFIDENTIAL"}; !reflect.DeepEqual(meta.Flags, want) {
		t.Fatalf("flags after add = %v, want %v", meta.Flags, want)
	}
	// The version bump is applied on disk by WriteMetadata (ApplyMetaEdit returns the in-memory
	// metadata it loaded), so read the sidecar back to see it.
	stored, _, err := ReadMetadata(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version < MetadataVersionV10 {
		t.Fatalf("flags should bump the sidecar to at least v%d, got %d", MetadataVersionV10, stored.Version)
	}

	// Removing one flag keeps the other; case does not matter for the removal.
	meta, err = ApplyMetaEdit(cfg, 100, MetaEdit{FlagUnset: []string{"confidential"}})
	if err != nil {
		t.Fatalf("remove flag: %v", err)
	}
	if want := []string{"DEPRECATED"}; !reflect.DeepEqual(meta.Flags, want) {
		t.Fatalf("flags after unset = %v, want %v", meta.Flags, want)
	}

	// Removing the last flag clears the field entirely (nil, so YAML omits it).
	meta, err = ApplyMetaEdit(cfg, 100, MetaEdit{FlagUnset: []string{"DEPRECATED"}})
	if err != nil {
		t.Fatalf("clear flags: %v", err)
	}
	if meta.Flags != nil {
		t.Fatalf("flags after clearing = %v, want nil", meta.Flags)
	}
}

func TestApplyMetaEditRejectsUnknownFlagAtomically(t *testing.T) {
	cfg := metaDocConfig(t)
	seed, err := ApplyMetaEdit(cfg, 100, MetaEdit{FlagAdd: []string{"DEPRECATED"}})
	if err != nil {
		t.Fatalf("seed flag: %v", err)
	}
	before, err := os.ReadFile(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMetaEdit(cfg, 100, MetaEdit{FlagAdd: []string{"TOP_SECRET"}}); err == nil {
		t.Fatal("an unknown flag should be rejected")
	}
	if _, err := ApplyMetaEdit(cfg, 100, MetaEdit{FlagUnset: []string{"TOP_SECRET"}}); err == nil {
		t.Fatal("an unknown --unflag target should be rejected")
	}
	after, err := os.ReadFile(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("rejected edits must change nothing; sidecar diff:\n%s\n---\n%s", before, after)
	}
	if !reflect.DeepEqual(seed.Flags, []string{"DEPRECATED"}) {
		t.Fatalf("seed flags = %v", seed.Flags)
	}
}

func TestApplyMetaDocValueValidatesAndNormalizesFlags(t *testing.T) {
	cfg := metaDocConfig(t)
	if _, err := ApplyMetaDoc(cfg, 100, []byte("flags:\n  - deprecated\n  - CONFIDENTIAL\n  - deprecated\n")); err != nil {
		t.Fatalf("apply flags doc: %v", err)
	}
	meta, _, err := ReadMetadata(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"DEPRECATED", "CONFIDENTIAL"}; !reflect.DeepEqual(meta.Flags, want) {
		t.Fatalf("normalized flags = %v, want %v", meta.Flags, want)
	}
	if meta.Version < MetadataVersionV10 {
		t.Fatalf("a flags sidecar is at least v%d, got %d", MetadataVersionV10, meta.Version)
	}

	// A document carrying an unknown flag is rejected atomically.
	before, err := os.ReadFile(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMetaDoc(cfg, 100, []byte("flags:\n  - TOP_SECRET\n")); err == nil {
		t.Fatal("an unknown flag in a metadata document should be rejected")
	}
	after, err := os.ReadFile(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("rejected document must change nothing; sidecar diff:\n%s\n---\n%s", before, after)
	}

	// The document is whole-state: omitting flags clears them.
	if _, err := ApplyMetaDoc(cfg, 100, []byte("tags:\n  - go\n")); err != nil {
		t.Fatalf("apply doc without flags: %v", err)
	}
	meta, _, _ = ReadMetadata(cfg.MetadataPath(100))
	if meta.Flags != nil {
		t.Fatalf("flags should clear when the document omits them, got %v", meta.Flags)
	}
}

func TestMetaDocYAMLRoundTripsFlags(t *testing.T) {
	meta := Metadata{Title: "Alpha", Flags: []string{"DEPRECATED", "CONFIDENTIAL"}}
	doc, err := MetaDocYAML(meta)
	if err != nil {
		t.Fatalf("render doc: %v", err)
	}
	if !strings.Contains(doc, "flags:") || !strings.Contains(doc, "- DEPRECATED") || !strings.Contains(doc, "- CONFIDENTIAL") {
		t.Fatalf("doc missing flags:\n%s", doc)
	}
	parsed, err := ParseMetaDoc([]byte(doc))
	if err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	if want := []string{"DEPRECATED", "CONFIDENTIAL"}; !reflect.DeepEqual(parsed.Flags, want) {
		t.Fatalf("parsed flags = %v, want %v", parsed.Flags, want)
	}

	// An empty flag set renders as a bare "flags:" line, not the flow "[]" form.
	empty, err := MetaDocYAML(Metadata{Title: "Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, "\nflags:\n") || strings.Contains(empty, "flags: []") {
		t.Fatalf("empty flags must render bare:\n%s", empty)
	}
}
