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
	Line         int    // 0-based line number
	StartByte    int    // start of the inner text (just after "[[")
	EndByte      int    // end of the inner text (just before "]]")
	OpenByte     int    // start of "[[", for replace ranges and unresolved highlighting
	CloseByte    int    // end of "]]"
	Text         string // resolution key: the note key (before any "#" anchor or "|"), whitespace trimmed
	Display      string // display text: the part after "|", or the whole target when no "|" is present
	Heading      string // heading anchor text after the "#" run, or "" when the link targets the whole note
	HeadingLevel int    // number of "#" in the anchor (1 == h1, 2 == h2, ...), or 0 when there is no anchor
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
			key, heading, level := splitHeading(target)
			if key == "" {
				continue
			}
			out = append(out, Ref{
				Line:         line.Number,
				StartByte:    m[2],
				EndByte:      m[3],
				OpenByte:     m[0],
				CloseByte:    m[1],
				Text:         key,
				Display:      display,
				Heading:      heading,
				HeadingLevel: level,
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

// splitHeading separates a link target into its note key and an optional heading anchor.
// The first "#" introduces the anchor: the run of "#" gives the heading level (1 == h1, 2 == h2, ...)
// and the trimmed remainder is the heading text. When the text after the "#" run is empty — e.g. a
// note key that itself ends in "#" like "C#" — there is no anchor and the "#" stays part of the key.
// Heading text is not unique within a note, so resolution adopts the first matching heading.
func splitHeading(target string) (key, heading string, level int) {
	i := strings.IndexByte(target, '#')
	if i < 0 {
		return target, "", 0
	}
	rest := target[i:]
	for level < len(rest) && rest[level] == '#' {
		level++
	}
	heading = strings.TrimSpace(rest[level:])
	if heading == "" {
		return target, "", 0
	}
	return strings.TrimSpace(target[:i]), heading, level
}

// Heading is one ATX heading occurrence in note text.
type Heading struct {
	Level int    // number of leading "#" (1 == h1, 2 == h2, ...)
	Text  string // heading text, trimmed of surrounding whitespace and any closing "#"
	Line  int    // 0-based line number
}

// atxHeading matches an ATX heading line ("# text" through "###### text") after leading whitespace is trimmed.
var atxHeading = regexp.MustCompile(`^(#{1,6})[ \t]+(.*)$`)

// Headings returns every ATX heading in text, skipping fenced code blocks.
// Heading text is trimmed and any closing "#" run (closed ATX, e.g. "## bar ##") is dropped.
func Headings(text string) []Heading {
	var out []Heading
	for _, line := range scannableLines(text) {
		m := atxHeading.FindStringSubmatch(strings.TrimSpace(line.Text))
		if m == nil {
			continue
		}
		out = append(out, Heading{
			Level: len(m[1]),
			Text:  strings.TrimSpace(strings.TrimRight(strings.TrimSpace(m[2]), "#")),
			Line:  line.Number,
		})
	}
	return out
}

// FindHeading returns the 0-based line of the first heading in text whose level and text match exactly.
// First match wins because heading text is not guaranteed unique within a note.
func FindHeading(text string, level int, heading string) (int, bool) {
	for _, h := range Headings(text) {
		if h.Level == level && h.Text == heading {
			return h.Line, true
		}
	}
	return 0, false
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
