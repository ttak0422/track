package link

import (
	"reflect"
	"strings"
	"testing"
)

func TestRefsBlockAnchor(t *testing.T) {
	got := Refs("see [[Note#^intro-1]] here")
	if len(got) != 1 {
		t.Fatalf("expected 1 ref, got %+v", got)
	}
	r := got[0]
	if r.Text != "Note" || r.BlockID != "intro-1" || r.Heading != "" || r.HeadingLevel != 0 {
		t.Fatalf("block anchor parsed wrong: %+v", r)
	}
}

func TestRefsBlockAnchorInvalidIDStaysHeading(t *testing.T) {
	// "^" followed by something outside the id grammar is not a block anchor; it parses as a
	// heading anchor like any other "#..." text.
	got := Refs("[[Note#^not an id]]")
	if len(got) != 1 || got[0].BlockID != "" || got[0].Heading != "^not an id" {
		t.Fatalf("invalid block id should fall back to heading anchor: %+v", got)
	}
}

func TestBlocksFindsTrailingMarkers(t *testing.T) {
	text := strings.Join([]string{
		"A paragraph line. ^para",
		"",
		"- item one ^item",
		"",
		"```",
		"code ^fenced",
		"```",
		"not a marker: foo^glued",
		"^alone",
	}, "\n")
	got := Blocks(text)
	want := []Block{{ID: "para", Line: 0}, {ID: "item", Line: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocks = %+v, want %+v", got, want)
	}
}

func TestFindBlockParagraph(t *testing.T) {
	text := strings.Join([]string{
		"# Title",
		"",
		"first line",
		"second line ^blk",
		"third line",
		"",
		"after",
	}, "\n")
	from, to, ok := FindBlock(text, "blk")
	if !ok || from != 2 || to != 5 {
		t.Fatalf("FindBlock = (%d, %d, %v), want (2, 5, true)", from, to, ok)
	}
	if _, _, ok := FindBlock(text, "missing"); ok {
		t.Fatal("missing id must not match")
	}
}

func TestFindBlockListItem(t *testing.T) {
	text := strings.Join([]string{
		"- item one ^one",
		"  continuation",
		"  - nested child",
		"- item two",
	}, "\n")
	from, to, ok := FindBlock(text, "one")
	if !ok || from != 0 || to != 3 {
		t.Fatalf("FindBlock = (%d, %d, %v), want (0, 3, true)", from, to, ok)
	}
}

func TestStripBlockMarker(t *testing.T) {
	if got := StripBlockMarker("text ^id  "); got != "text" {
		t.Fatalf("StripBlockMarker = %q, want %q", got, "text")
	}
	if got := StripBlockMarker("no marker here"); got != "no marker here" {
		t.Fatalf("unmarked line changed: %q", got)
	}
	if got := StripBlockMarker("glued^id"); got != "glued^id" {
		t.Fatalf("glued caret is prose, got %q", got)
	}
}

func TestExtractBlockInclude(t *testing.T) {
	body := strings.Join([]string{
		"# Title",
		"",
		"An intro paragraph",
		"spanning two lines. ^intro",
		"",
		"- a list item ^li",
		"  with a child",
		"- another item",
	}, "\n")

	incs := Includes("![[Note#^intro]]\n![[Note#^li]]\n![[Note#^nope]]")
	if len(incs) != 3 {
		t.Fatalf("want 3 includes, got %+v", incs)
	}

	lines, ok := Extract(body, incs[0])
	if !ok || !reflect.DeepEqual(lines, []string{"An intro paragraph", "spanning two lines."}) {
		t.Fatalf("paragraph block extract = (%v, %v)", lines, ok)
	}

	lines, ok = Extract(body, incs[1])
	if !ok || !reflect.DeepEqual(lines, []string{"- a list item", "  with a child"}) {
		t.Fatalf("list block extract = (%v, %v)", lines, ok)
	}

	if _, ok := Extract(body, incs[2]); ok {
		t.Fatal("missing block must not fall back to the whole note")
	}
}

func TestResolveIncludesReportsMissingBlock(t *testing.T) {
	res := ResolveIncludes("![[Note#^nope]]", func(string) (int64, string, string, bool) {
		return 1, "note", "some body", true
	})
	if len(res) != 1 || res[0].Error != "block not found: ^nope" {
		t.Fatalf("unexpected resolution: %+v", res)
	}
}
