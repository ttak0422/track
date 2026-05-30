// Package match implements track's auto-link detection: finding occurrences of known keywords (note titles and aliases) inside note text.
// The Go indexer uses it to build the link graph; the Lua frontend reimplements the same longest-match-substring rule for highlighting, so "what is a link" agrees on both sides.
package match

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Term is a keyword pointing at a note.
type Term struct {
	Text   string
	NoteID int64
}

type Occurrence struct {
	Term      Term
	Line      int
	StartByte int
	EndByte   int
}

// Matcher resolves keyword occurrences using longest-match, non-overlapping scanning.
// CJK has no word boundaries, so matching is pure substring matching by design.
type Matcher struct {
	terms []Term // unique text, sorted by byte length descending
}

// New builds a Matcher from terms.
// Duplicate texts keep the first NoteID.
func New(terms []Term) *Matcher {
	seen := make(map[string]bool, len(terms))
	uniq := make([]Term, 0, len(terms))
	for _, t := range terms {
		if t.Text == "" || seen[t.Text] {
			continue
		}
		seen[t.Text] = true
		uniq = append(uniq, t)
	}
	sort.SliceStable(uniq, func(i, j int) bool {
		return len(uniq[i].Text) > len(uniq[j].Text)
	})
	return &Matcher{terms: uniq}
}

// TargetIDs returns the deduplicated set of note ids whose keywords appear in text, skipping fenced code blocks.
// Order is the first-seen order.
func (m *Matcher) TargetIDs(text string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)
	for _, occ := range m.Occurrences(text) {
		if !seen[occ.Term.NoteID] {
			seen[occ.Term.NoteID] = true
			ids = append(ids, occ.Term.NoteID)
		}
	}
	return ids
}

func (m *Matcher) Occurrences(text string) []Occurrence {
	var out []Occurrence
	for _, line := range scannableLines(text) {
		i := 0
		for i < len(line.Text) {
			if t, ok := m.matchAt(line.Text, i); ok {
				out = append(out, Occurrence{
					Term:      t,
					Line:      line.Number,
					StartByte: i,
					EndByte:   i + len(t.Text),
				})
				i += len(t.Text)
				continue
			}
			_, size := utf8.DecodeRuneInString(line.Text[i:])
			if size == 0 {
				size = 1
			}
			i += size
		}
	}
	return out
}

func (m *Matcher) matchAt(line string, i int) (Term, bool) {
	rest := line[i:]
	for _, t := range m.terms {
		if strings.HasPrefix(rest, t.Text) {
			return t, true
		}
	}
	return Term{}, false
}

type scannableLine struct {
	Number int
	Text   string
}

// scannableLines returns the lines of text that are eligible for keyword matching, dropping lines inside fenced code blocks.
func scannableLines(text string) []scannableLine {
	lines := strings.Split(text, "\n")
	out := make([]scannableLine, 0, len(lines))
	inFence := false
	for i, line := range lines {
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, scannableLine{Number: i, Text: line})
	}
	return out
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}
