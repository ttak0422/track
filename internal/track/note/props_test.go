package note

import (
	"reflect"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func TestValueType(t *testing.T) {
	cases := map[string]string{
		"true":          TypeBoolean,
		"false":         TypeBoolean,
		"42":            TypeNumber,
		"-3.5":          TypeNumber,
		"2026-07-10":    TypeDate,
		"2026-13-40":    TypeString, // not a real calendar day
		"[[Go]]":        TypeLink,
		"hello":         TypeString,
		"1.2.3":         TypeString,
		"inf":           TypeString,
		"[[]]":          TypeString,
		"true business": TypeString,
	}
	for in, want := range cases {
		if got := ValueType(in); got != want {
			t.Errorf("ValueType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInlineFields(t *testing.T) {
	body := "# Heading\n" + // 1
		"status:: draft\n" + // 2
		"- rating:: 8\n" + // 3
		"2. due:: 2026-08-01\n" + // 4
		"Met with [owner:: [[Ada Lovelace]]] about the plan.\n" + // 5
		"languages:: go, lua\n" + // 6
		"```\n" +
		"fenced:: not a field\n" +
		"```\n" +
		"std::vector is not a field\n" +
		"plain prose with key:: mid-sentence stays one line field? no marker\n" +
		"docs may show `[example:: value]` in inline code without it becoming data\n"

	got := InlineFields(body)
	want := []Prop{
		{Key: "status", Value: "draft", Type: TypeString, Line: 2},
		{Key: "rating", Value: "8", Type: TypeNumber, Line: 3},
		{Key: "due", Value: "2026-08-01", Type: TypeDate, Line: 4},
		{Key: "owner", Value: "Ada Lovelace", Type: TypeLink, Line: 5},
		{Key: "languages", Value: "go", Type: TypeString, Line: 6},
		{Key: "languages", Value: "lua", Type: TypeString, Line: 6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InlineFields = %+v\nwant %+v", got, want)
	}
}

func TestSplitListKeepsLinkCommas(t *testing.T) {
	got := splitList("[[Ada, Countess]], [[Go]], plain")
	want := []string{"[[Ada, Countess]]", "[[Go]]", "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitList = %v, want %v", got, want)
	}
}

func TestSidecarPropsTypesYAMLValues(t *testing.T) {
	meta := Metadata{Props: map[string]any{
		"rating":  8,
		"done":    true,
		"due":     "2026-08-01",
		"owner":   "[[Ada Lovelace|Ada]]",
		"aliases": []any{"one", 2},
	}}
	got := SidecarProps(meta)
	want := []Prop{
		{Key: "aliases", Value: "one", Type: TypeString},
		{Key: "aliases", Value: "2", Type: TypeNumber},
		{Key: "done", Value: "true", Type: TypeBoolean},
		{Key: "due", Value: "2026-08-01", Type: TypeDate},
		{Key: "owner", Value: "Ada Lovelace", Type: TypeLink},
		{Key: "rating", Value: "8", Type: TypeNumber},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SidecarProps = %+v\nwant %+v", got, want)
	}
}

func TestParsePropValue(t *testing.T) {
	if got := ParsePropValue("8"); got != int64(8) {
		t.Fatalf("ParsePropValue(8) = %#v", got)
	}
	if got := ParsePropValue("true"); got != true {
		t.Fatalf("ParsePropValue(true) = %#v", got)
	}
	if got := ParsePropValue("2026-08-01"); got != "2026-08-01" {
		t.Fatalf("ParsePropValue(date) = %#v", got)
	}
	got := ParsePropValue("go, 2")
	want := []any{"go", int64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePropValue(list) = %#v, want %#v", got, want)
	}
}

func TestCheckProps(t *testing.T) {
	schema := map[string]config.PropSpec{
		"status": {Type: TypeString, Values: []string{"draft", "done"}},
		"rating": {Type: TypeNumber},
		"free":   {},
	}
	props := []Prop{
		{Key: "status", Value: "draft", Type: TypeString, Line: 2},
		{Key: "status", Value: "waiting", Type: TypeString, Line: 3},
		{Key: "rating", Value: "high", Type: TypeString, Line: 4},
		{Key: "free", Value: "anything", Type: TypeString},
		{Key: "unknown", Value: "ignored", Type: TypeString},
	}
	got := CheckProps(props, schema)
	if len(got) != 2 {
		t.Fatalf("CheckProps = %+v, want 2 violations", got)
	}
	if got[0].Key != "status" || got[0].Line != 3 {
		t.Fatalf("first violation = %+v", got[0])
	}
	if got[1].Key != "rating" || got[1].Line != 4 {
		t.Fatalf("second violation = %+v", got[1])
	}
}

func TestApplyMetaEditSetUnsetProps(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:   vault,
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
		Properties: map[string]config.PropSpec{
			"status": {Values: []string{"draft", "done"}},
		},
	}

	meta, err := ApplyMetaEdit(cfg, 100, MetaEdit{Set: map[string]string{"status": "draft", "rating": "8"}})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if meta.Props["status"] != "draft" || meta.Props["rating"] != int64(8) {
		t.Fatalf("props = %#v", meta.Props)
	}

	if _, err := ApplyMetaEdit(cfg, 100, MetaEdit{Set: map[string]string{"status": "waiting"}}); err == nil {
		t.Fatal("expected schema violation for status=waiting")
	}
	if _, err := ApplyMetaEdit(cfg, 100, MetaEdit{Set: map[string]string{"bad key": "x"}}); err == nil {
		t.Fatal("expected invalid key error")
	}

	meta, err = ApplyMetaEdit(cfg, 100, MetaEdit{Unset: []string{"status", "rating"}})
	if err != nil {
		t.Fatalf("unset: %v", err)
	}
	if len(meta.Props) != 0 {
		t.Fatalf("props after unset = %#v", meta.Props)
	}

	// The sidecar round-trips: version was bumped for props and reads back cleanly.
	stored, found, err := ReadMetadata(cfg.MetadataPath(100))
	if err != nil || !found {
		t.Fatalf("read sidecar: found=%v err=%v", found, err)
	}
	if stored.Props != nil {
		t.Fatalf("stored props = %#v", stored.Props)
	}
}
