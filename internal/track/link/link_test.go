package link

import (
	"reflect"
	"testing"
)

func TestRefsSingle(t *testing.T) {
	got := Refs("see [[リンク]] here")
	want := []Ref{
		{Line: 0, StartByte: 6, EndByte: 15, OpenByte: 4, CloseByte: 17, Text: "リンク", Display: "リンク"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRefsMultiplePerLineAndLines(t *testing.T) {
	got := Refs("[[a]] and [[b]]\nthen [[c]]")
	want := []Ref{
		{Line: 0, StartByte: 2, EndByte: 3, OpenByte: 0, CloseByte: 5, Text: "a", Display: "a"},
		{Line: 0, StartByte: 12, EndByte: 13, OpenByte: 10, CloseByte: 15, Text: "b", Display: "b"},
		{Line: 1, StartByte: 7, EndByte: 8, OpenByte: 5, CloseByte: 10, Text: "c", Display: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRefsSkipsFencedCode(t *testing.T) {
	got := Refs("[[a]]\n```\n[[b]]\n```\n[[c]]")
	want := []Ref{
		{Line: 0, StartByte: 2, EndByte: 3, OpenByte: 0, CloseByte: 5, Text: "a", Display: "a"},
		{Line: 4, StartByte: 2, EndByte: 3, OpenByte: 0, CloseByte: 5, Text: "c", Display: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRefsTrimsInnerWhitespace(t *testing.T) {
	got := Refs("[[  リンク  ]]")
	if len(got) != 1 || got[0].Text != "リンク" {
		t.Fatalf("expected trimmed key リンク, got %+v", got)
	}
	// The highlight range covers the inner text including padding.
	if got[0].StartByte != 2 || got[0].EndByte != 15 {
		t.Fatalf("unexpected inner range: %+v", got[0])
	}
}

func TestRefsDisplayAlias(t *testing.T) {
	got := Refs("see [[Go|ゴー言語]] here")
	if len(got) != 1 {
		t.Fatalf("expected 1 ref, got %+v", got)
	}
	r := got[0]
	if r.Text != "Go" || r.Display != "ゴー言語" {
		t.Fatalf("target/display: %+v", r)
	}
	// The range still spans the whole inner text, pipe included: "see " is 4 bytes, "[[" at 4-5.
	if r.StartByte != 6 {
		t.Fatalf("unexpected start byte: %+v", r)
	}
}

func TestRefsDisplayEdgeCases(t *testing.T) {
	// Empty display falls back to the target.
	if g := Refs("[[Go|]]"); len(g) != 1 || g[0].Text != "Go" || g[0].Display != "Go" {
		t.Fatalf("empty display: %+v", g)
	}
	// Empty target is not a link.
	if g := Refs("[[|disp]]"); len(g) != 0 {
		t.Fatalf("empty target should be invalid: %+v", g)
	}
	// Only the first pipe splits; the rest stays in the display.
	if g := Refs("[[a|b|c]]"); len(g) != 1 || g[0].Text != "a" || g[0].Display != "b|c" {
		t.Fatalf("multi pipe: %+v", g)
	}
	// Whitespace around both sides is trimmed.
	if g := Refs("[[ Go | ゴー ]]"); len(g) != 1 || g[0].Text != "Go" || g[0].Display != "ゴー" {
		t.Fatalf("trim: %+v", g)
	}
}

func TestRefsHeadingAnchor(t *testing.T) {
	// Single "#" is an h1 anchor; the note key excludes the anchor.
	if g := Refs("[[test#foo]]"); len(g) != 1 || g[0].Text != "test" || g[0].Heading != "foo" || g[0].HeadingLevel != 1 {
		t.Fatalf("h1 anchor: %+v", g)
	}
	// Double "##" is an h2 anchor.
	if g := Refs("[[test##bar]]"); len(g) != 1 || g[0].Text != "test" || g[0].Heading != "bar" || g[0].HeadingLevel != 2 {
		t.Fatalf("h2 anchor: %+v", g)
	}
	// Whitespace around the key and heading is trimmed.
	if g := Refs("[[ test # foo ]]"); len(g) != 1 || g[0].Text != "test" || g[0].Heading != "foo" || g[0].HeadingLevel != 1 {
		t.Fatalf("trimmed anchor: %+v", g)
	}
	// An anchor composes with a display alias: target#heading|display.
	if g := Refs("[[test#foo|ふー]]"); len(g) != 1 || g[0].Text != "test" || g[0].Heading != "foo" || g[0].Display != "ふー" {
		t.Fatalf("anchor with display: %+v", g)
	}
	// A trailing "#" with no heading text stays part of the note key.
	if g := Refs("[[C#]]"); len(g) != 1 || g[0].Text != "C#" || g[0].Heading != "" || g[0].HeadingLevel != 0 {
		t.Fatalf("trailing hash key: %+v", g)
	}
	// An empty note key (anchor only) is not a link.
	if g := Refs("[[#foo]]"); len(g) != 0 {
		t.Fatalf("anchor-only should be invalid: %+v", g)
	}
}

func TestHeadingsAndFind(t *testing.T) {
	body := "# Title\n\n## foo\nbody\n## foo\n```\n## fenced\n```\n### bar ###\n"
	got := Headings(body)
	want := []Heading{
		{Level: 1, Text: "Title", Line: 0},
		{Level: 2, Text: "foo", Line: 2},
		{Level: 2, Text: "foo", Line: 4},
		{Level: 3, Text: "bar", Line: 8},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// First matching heading wins.
	if line, ok := FindHeading(body, 2, "foo"); !ok || line != 2 {
		t.Fatalf("FindHeading h2 foo: line=%d ok=%v", line, ok)
	}
	if line, ok := FindHeading(body, 3, "bar"); !ok || line != 8 {
		t.Fatalf("FindHeading h3 bar: line=%d ok=%v", line, ok)
	}
	// Level must match: an h1 "foo" does not exist.
	if _, ok := FindHeading(body, 1, "foo"); ok {
		t.Fatalf("FindHeading should not match wrong level")
	}
}

func TestHeadingsLevelRange(t *testing.T) {
	// h1 through h6 are valid ATX heading levels; a run of seven "#" is not a heading.
	body := "# a\n## b\n### c\n#### d\n##### e\n###### f\n####### g\n"
	got := Headings(body)
	want := []Heading{
		{Level: 1, Text: "a", Line: 0},
		{Level: 2, Text: "b", Line: 1},
		{Level: 3, Text: "c", Line: 2},
		{Level: 4, Text: "d", Line: 3},
		{Level: 5, Text: "e", Line: 4},
		{Level: 6, Text: "f", Line: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// h5/h6 resolve; a level-7 anchor has no heading to match (no clamp to h6).
	if line, ok := FindHeading(body, 5, "e"); !ok || line != 4 {
		t.Fatalf("FindHeading h5: line=%d ok=%v", line, ok)
	}
	if line, ok := FindHeading(body, 6, "f"); !ok || line != 5 {
		t.Fatalf("FindHeading h6: line=%d ok=%v", line, ok)
	}
	if _, ok := FindHeading(body, 7, "g"); ok {
		t.Fatalf("FindHeading should not resolve a level-7 anchor")
	}
}

func TestRefsIgnoresMalformed(t *testing.T) {
	for _, in := range []string{
		"[[unterminated",
		"plain [single] brackets",
		"[[]]",     // empty
		"[[ ]]",    // blank after trim
		"[[a]b]]",  // bracket inside
		"[[a]\n]]", // spans lines
	} {
		if got := Refs(in); len(got) != 0 {
			t.Fatalf("input %q: expected no refs, got %+v", in, got)
		}
	}
}

func TestReplaceRefKey(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		oldKey string
		newKey string
		want   string
		count  int
	}{
		{
			name:   "single reference",
			in:     "see [[Old]]",
			oldKey: "Old",
			newKey: "New",
			want:   "see [[New]]",
			count:  1,
		},
		{
			name:   "multiple references across lines",
			in:     "[[Old]] and [[Old]]\nthen [[Other]] and [[Old]]",
			oldKey: "Old",
			newKey: "New",
			want:   "[[New]] and [[New]]\nthen [[Other]] and [[New]]",
			count:  3,
		},
		{
			name:   "heading and display are preserved",
			in:     "[[Old#sec]] [[Old|表示]] [[Old##deep|深い]]",
			oldKey: "Old",
			newKey: "New",
			want:   "[[New#sec]] [[New|表示]] [[New##deep|深い]]",
			count:  3,
		},
		{
			name:   "no match",
			in:     "[[Other]]",
			oldKey: "Old",
			newKey: "New",
			want:   "[[Other]]",
			count:  0,
		},
		{
			name:   "empty old key",
			in:     "[[Old]]",
			oldKey: "",
			newKey: "New",
			want:   "[[Old]]",
			count:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, count := ReplaceRefKey(tt.in, tt.oldKey, tt.newKey)
			if got != tt.want || count != tt.count {
				t.Fatalf("ReplaceRefKey() = %q, %d; want %q, %d", got, count, tt.want, tt.count)
			}
		})
	}
}
