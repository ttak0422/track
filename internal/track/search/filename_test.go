package search

import "testing"

// A coding agent hands over whatever shape it read the vault in, so every shape of one file name has
// to reach the same note — and nothing that is not a whole file name may become a hit.
func TestNoteIDFromFileName(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int64
	}{
		{"note/1785024006000.md", 1785024006000},
		{"1785024006000.md", 1785024006000},
		{"1785024006000", 1785024006000},
		{"  note/1785024006000.md  ", 1785024006000},
		{`C:\vault\note\1785024006000.md`, 1785024006000},
		{"journal/20260811.md", 20260811},
	} {
		got, ok := NoteIDFromFileName(tc.query)
		if !ok || got != tc.want {
			t.Errorf("NoteIDFromFileName(%q) = (%d, %v), want (%d, true)", tc.query, got, ok, tc.want)
		}
	}

	for _, query := range []string{
		"",
		"   ",
		"design",              // an ordinary word
		"track 1785024006000", // a query that merely contains an id is a text search
		"1785024006000.txt",   // not a note file
		"note/",
		"v2",
		"178-502",
		"99999999999999999999999", // more digits than an id can hold
	} {
		if id, ok := NoteIDFromFileName(query); ok {
			t.Errorf("NoteIDFromFileName(%q) = (%d, true), want no match", query, id)
		}
	}
}
