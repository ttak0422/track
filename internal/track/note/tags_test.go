package note

import (
	"reflect"
	"testing"
)

func TestInlineTags(t *testing.T) {
	body := "# Title\n" + // 1: ATX heading ('#' + space) is not a tag
		"Working on #proj/track today.\n" + // 2
		"Also #golang and #proj/track again.\n" + // 3: duplicate dropped
		"```\n" +
		"#fenced/tag\n" +
		"```\n" +
		"docs show `#inline/code` in code spans\n" + // 7
		"#2026 is a year, # Heading is a heading, C# and foo#bar are not tags.\n" // 8

	got := InlineTags(body)
	want := []string{"proj/track", "golang"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InlineTags = %v, want %v", got, want)
	}
}

// A Markdown example shown as a longer fence wrapping a shorter one stays code (CommonMark closes a
// block only with a same-kind fence at least as long), so the tags inside it are sample syntax.
func TestInlineTagsSkipsNestedFence(t *testing.T) {
	body := "````markdown\n" +
		"```text\n" +
		"#nested/tag\n" +
		"```\n" +
		"````\n" +
		"#real/tag\n"

	got := InlineTags(body)
	want := []string{"real/tag"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InlineTags = %v, want %v", got, want)
	}
}

// A tag stops at any non-tag character: trailing punctuation and the end of a line stay out of the
// tag, and a multi-level path is kept whole.
func TestInlineTagsBoundaries(t *testing.T) {
	body := "(#a/b), [#c], #d/e/f. done\n#g_2/h-1\n#a/\n"
	got := InlineTags(body)
	want := []string{"a/b", "c", "d/e/f", "g_2/h-1", "a"} // "#a/" drops the empty segment
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InlineTags = %v, want %v", got, want)
	}
}

func TestCollectTagsMergesSidecarAndInline(t *testing.T) {
	meta := Metadata{Tags: []string{"sidecar", "shared"}}
	body := "body #inline #shared #sidecar\n"
	got := CollectTags(meta, body)
	// Sidecar order first, inline appended in body order, duplicates against sidecar dropped.
	want := []string{"sidecar", "shared", "inline"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectTags = %v, want %v", got, want)
	}
}
