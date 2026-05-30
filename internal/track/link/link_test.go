package link

import (
	"reflect"
	"testing"
)

func TestRefsSingle(t *testing.T) {
	got := Refs("see [[リンク]] here")
	want := []Ref{
		{Line: 0, StartByte: 6, EndByte: 15, OpenByte: 4, CloseByte: 17, Text: "リンク"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRefsMultiplePerLineAndLines(t *testing.T) {
	got := Refs("[[a]] and [[b]]\nthen [[c]]")
	want := []Ref{
		{Line: 0, StartByte: 2, EndByte: 3, OpenByte: 0, CloseByte: 5, Text: "a"},
		{Line: 0, StartByte: 12, EndByte: 13, OpenByte: 10, CloseByte: 15, Text: "b"},
		{Line: 1, StartByte: 7, EndByte: 8, OpenByte: 5, CloseByte: 10, Text: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRefsSkipsFencedCode(t *testing.T) {
	got := Refs("[[a]]\n```\n[[b]]\n```\n[[c]]")
	want := []Ref{
		{Line: 0, StartByte: 2, EndByte: 3, OpenByte: 0, CloseByte: 5, Text: "a"},
		{Line: 4, StartByte: 2, EndByte: 3, OpenByte: 0, CloseByte: 5, Text: "c"},
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
