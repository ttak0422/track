package note

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

var markers = config.FootmatterMarkers{Open: "<!--track", Close: "-->"}

func TestSplitFootmatter(t *testing.T) {
	raw := strings.Join([]string{
		"# Title",
		"",
		"本文の リンク について。",
		"",
		"<!--track",
		"title: リンク",
		"aliases:",
		"    - link",
		"    - TEST",
		"tags:",
		"    - zettel",
		"created: 2026-05-24",
		"-->",
		"",
	}, "\n")

	body, f, found, err := SplitFootmatter(raw, markers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected footmatter to be found")
	}
	wantBody := "# Title\n\n本文の リンク について。"
	if body != wantBody {
		t.Fatalf("body mismatch:\n got %q\nwant %q", body, wantBody)
	}
	want := Footmatter{
		Title:   "リンク",
		Aliases: []string{"link", "TEST"},
		Tags:    []string{"zettel"},
		Created: "2026-05-24",
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("footmatter mismatch:\n got %+v\nwant %+v", f, want)
	}
}

func TestSplitFootmatterAbsent(t *testing.T) {
	raw := "# Just a body\n\nno metadata here\n"
	body, f, found, err := SplitFootmatter(raw, markers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("did not expect footmatter")
	}
	if body != "# Just a body\n\nno metadata here" {
		t.Fatalf("body mismatch: %q", body)
	}
	if !reflect.DeepEqual(f, Footmatter{}) {
		t.Fatalf("expected zero footmatter, got %+v", f)
	}
}

func TestUpsertFootmatterRoundTrip(t *testing.T) {
	in := Footmatter{
		Title:   "リンク",
		Aliases: []string{"link", "TEST"},
		Tags:    []string{"zettel"},
		Created: "2026-05-24",
	}
	out, err := UpsertFootmatter("# Note\n\nbody text", in, markers)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	body, got, found, err := SplitFootmatter(out, markers)
	if err != nil {
		t.Fatalf("re-split: %v", err)
	}
	if !found {
		t.Fatal("expected footmatter after upsert")
	}
	if body != "# Note\n\nbody text" {
		t.Fatalf("body mismatch: %q", body)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestUpsertFootmatterReplacesExisting(t *testing.T) {
	first, err := UpsertFootmatter("body", Footmatter{Title: "old"}, markers)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := UpsertFootmatter(first, Footmatter{Title: "new"}, markers)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if strings.Count(second, markers.Open) != 1 {
		t.Fatalf("expected exactly one footmatter block, got:\n%s", second)
	}
	_, got, _, err := SplitFootmatter(second, markers)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if got.Title != "new" {
		t.Fatalf("expected title 'new', got %q", got.Title)
	}
}
