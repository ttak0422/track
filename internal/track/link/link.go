// Package link implements track's explicit note links: extracting [[...]] references from note text.
// Extraction is deliberately store-free so it stays cheap and unit-testable; resolving a reference's
// text to a note id is the caller's job (index builds the link graph, the LSP server answers navigation),
// which keeps detection at O(line length) per line and resolution at O(1) per reference.
package link

import (
	"regexp"
	"strings"
)

// Ref is one [[...]] occurrence in note text.
// Byte offsets are within the occurrence's line; Line is 0-based.
type Ref struct {
	Line      int    // 0-based line number
	StartByte int    // start of the inner text (just after "[[")
	EndByte   int    // end of the inner text (just before "]]")
	OpenByte  int    // start of "[[", for replace ranges and unresolved highlighting
	CloseByte int    // end of "]]"
	Text      string // resolution key: the target before "|", whitespace trimmed
	Display   string // display text: the part after "|", or Text when no "|" is present
}

// wikiLink matches a single-line [[...]] with no brackets inside, so [[a]b]] and [[]] do not match.
var wikiLink = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// Refs extracts every [[...]] occurrence in text, skipping fenced code blocks.
// Links are single-line; references whose inner text is blank after trimming are ignored.
func Refs(text string) []Ref {
	var out []Ref
	for _, line := range scannableLines(text) {
		for _, m := range wikiLink.FindAllStringSubmatchIndex(line.Text, -1) {
			inner := line.Text[m[2]:m[3]]
			target, display := splitDisplay(inner)
			if target == "" {
				continue
			}
			out = append(out, Ref{
				Line:      line.Number,
				StartByte: m[2],
				EndByte:   m[3],
				OpenByte:  m[0],
				CloseByte: m[1],
				Text:      target,
				Display:   display,
			})
		}
	}
	return out
}

// splitDisplay parses [[target|display]] inner text into its resolution key and display text.
// The first "|" separates them; later "|" stay in the display. Without a "|", display equals the
// trimmed key, and an empty display falls back to the key. A blank target yields an empty target,
// which the caller treats as not a link.
func splitDisplay(inner string) (target, display string) {
	if i := strings.IndexByte(inner, '|'); i >= 0 {
		target = strings.TrimSpace(inner[:i])
		display = strings.TrimSpace(inner[i+1:])
		if display == "" {
			display = target
		}
		return target, display
	}
	target = strings.TrimSpace(inner)
	return target, target
}

type scannableLine struct {
	Number int
	Text   string
}

// scannableLines returns the lines eligible for link extraction, dropping lines inside fenced code blocks.
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
