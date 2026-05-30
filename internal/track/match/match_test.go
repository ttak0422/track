package match

import (
	"reflect"
	"testing"
)

func TestTargetIDsLongestMatch(t *testing.T) {
	m := New([]Term{
		{Text: "Go", NoteID: 1},
		{Text: "Golang", NoteID: 2},
		{Text: "リンク", NoteID: 3},
	})

	// "Golang" must win over "Go" (longest match); リンク matches mid-sentence.
	got := m.TargetIDs("今日は Golang と リンク について。Go も。")
	want := []int64{2, 3, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTargetIDsDedup(t *testing.T) {
	m := New([]Term{{Text: "Go", NoteID: 1}})
	got := m.TargetIDs("Go Go Go")
	if !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("got %v, want [1]", got)
	}
}

func TestTargetIDsSkipsFencedCode(t *testing.T) {
	m := New([]Term{{Text: "Go", NoteID: 1}})
	text := "intro\n```\nGo inside code\n```\nGo outside"
	got := m.TargetIDs(text)
	if !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("got %v, want [1]", got)
	}

	codeOnly := "```\nGo only here\n```"
	if got := m.TargetIDs(codeOnly); got != nil {
		t.Fatalf("expected no matches inside code, got %v", got)
	}
}

func TestNewDeduplicatesTerms(t *testing.T) {
	m := New([]Term{
		{Text: "Go", NoteID: 1},
		{Text: "Go", NoteID: 2},
		{Text: "", NoteID: 3},
	})
	if len(m.terms) != 1 || m.terms[0].NoteID != 1 {
		t.Fatalf("expected single term keeping first id, got %+v", m.terms)
	}
}

func TestOccurrences(t *testing.T) {
	m := New([]Term{
		{Text: "Go", NoteID: 1},
		{Text: "Golang", NoteID: 2},
	})
	got := m.Occurrences("Golang\n```\nGo\n```\nGo")
	want := []Occurrence{
		{Term: Term{Text: "Golang", NoteID: 2}, Line: 0, StartByte: 0, EndByte: 6},
		{Term: Term{Text: "Go", NoteID: 1}, Line: 4, StartByte: 0, EndByte: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
