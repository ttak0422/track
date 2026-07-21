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
	BlockID      string // block anchor id after "#^" ([[note#^id]]), or "" when the link has no block anchor
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
			anchor := splitAnchor(target)
			if anchor.Key == "" {
				continue
			}
			out = append(out, Ref{
				Line:         line.Number,
				StartByte:    m[2],
				EndByte:      m[3],
				OpenByte:     m[0],
				CloseByte:    m[1],
				Text:         anchor.Key,
				Display:      display,
				Heading:      anchor.Heading,
				HeadingLevel: anchor.HeadingLevel,
				BlockID:      anchor.BlockID,
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

// anchor is the parsed form of a link target: the note key and its optional heading or block anchor.
type anchor struct {
	Key          string
	Heading      string
	HeadingLevel int
	BlockID      string
}

// blockIDRe is the accepted block id form: the "^" sigil then letters, digits, "-" or "_". Manual
// ids only — track never generates one — so the grammar stays typeable and URL-safe.
var blockIDRe = regexp.MustCompile(`^\^([A-Za-z0-9][A-Za-z0-9_-]*)$`)

// splitAnchor separates a link target into its note key and an optional anchor.
// The first "#" introduces the anchor. "#^id" is a block anchor (see Blocks). Otherwise the run of
// "#" gives the heading level (1 == h1, 2 == h2, ...) and the trimmed remainder is the heading text.
// When the text after the "#" run is empty — e.g. a note key that itself ends in "#" like "C#" —
// there is no anchor and the "#" stays part of the key. Heading text is not unique within a note,
// so resolution adopts the first matching heading; block ids adopt the first matching marker.
func splitAnchor(target string) anchor {
	i := strings.IndexByte(target, '#')
	if i < 0 {
		return anchor{Key: target}
	}
	rest := target[i:]
	if m := blockIDRe.FindStringSubmatch(strings.TrimSpace(rest[1:])); m != nil {
		return anchor{Key: strings.TrimSpace(target[:i]), BlockID: m[1]}
	}
	level := 0
	for level < len(rest) && rest[level] == '#' {
		level++
	}
	heading := strings.TrimSpace(rest[level:])
	if heading == "" {
		return anchor{Key: target}
	}
	return anchor{Key: strings.TrimSpace(target[:i]), Heading: heading, HeadingLevel: level}
}

// ReplaceRefKey rewrites the resolution key of every [[key]] reference whose key equals oldKey,
// replacing just the key span with newKey. Heading anchors and "|display" aliases are preserved
// because only the key portion is touched. It returns the rewritten text and the number of
// references replaced. This is the shared backlink rewriter used when a note is renamed.
func ReplaceRefKey(text, oldKey, newKey string) (string, int) {
	if oldKey == "" {
		return text, 0
	}
	lines := strings.Split(text, "\n")
	byLine := map[int][]Ref{}
	for _, ref := range Refs(text) {
		if ref.Text == oldKey {
			byLine[ref.Line] = append(byLine[ref.Line], ref)
		}
	}
	count := 0
	for lineNo, refs := range byLine {
		line := lines[lineNo]
		// Replace right-to-left so earlier byte offsets stay valid as the line is edited.
		for i := len(refs) - 1; i >= 0; i-- {
			ref := refs[i]
			if ref.StartByte > ref.EndByte || ref.EndByte > len(line) {
				continue
			}
			inner := line[ref.StartByte:ref.EndByte]
			j := strings.Index(inner, ref.Text)
			if j < 0 {
				continue
			}
			keyStart := ref.StartByte + j
			keyEnd := keyStart + len(ref.Text)
			line = line[:keyStart] + newKey + line[keyEnd:]
			count++
		}
		lines[lineNo] = line
	}
	return strings.Join(lines, "\n"), count
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

// Block is one manual block marker in note text: a "^id" written at the end of a content line marks
// the paragraph or list item it closes as a link target ([[note#^id]]).
type Block struct {
	ID   string // the id after "^"
	Line int    // 0-based line the marker sits on
}

// blockMarker matches a trailing " ^id" on a line (whitespace-separated so "foo^2" stays prose).
var blockMarker = regexp.MustCompile(`(?:^|[ \t])\^([A-Za-z0-9][A-Za-z0-9_-]*)[ \t]*$`)

// listItemRe matches the marker of a Markdown list item, capturing its leading indentation.
var listItemRe = regexp.MustCompile(`^([ \t]*)(?:[-*+]|\d+[.)])[ \t]`)

// Blocks returns every block marker in text, skipping fenced code blocks and blank lines (a marker
// needs content on its line — track has no detached "marker under the block" form).
func Blocks(text string) []Block {
	var out []Block
	for _, line := range scannableLines(text) {
		m := blockMarker.FindStringSubmatch(line.Text)
		if m == nil || strings.TrimSpace(strings.TrimSuffix(line.Text, m[0])) == "" {
			continue
		}
		out = append(out, Block{ID: m[1], Line: line.Number})
	}
	return out
}

// FindBlock returns the 0-based line range [from, to) of the block the marker "^id" closes: for a
// list item, the item line plus its more-indented continuation lines; otherwise the contiguous run
// of non-blank lines around the marker line. false when no marker matches; first match wins.
// ponytail: blank lines are the only paragraph boundary (an unspaced adjacent heading or fence would
// join the block); split blocks with blank lines, which is how marked blocks are written anyway.
func FindBlock(text, id string) (from, to int, ok bool) {
	line := -1
	for _, b := range Blocks(text) {
		if b.ID == id {
			line = b.Line
			break
		}
	}
	if line < 0 {
		return 0, 0, false
	}
	all := strings.Split(text, "\n")
	if m := listItemRe.FindStringSubmatch(all[line]); m != nil {
		indent := len(m[1])
		to = line + 1
		for to < len(all) {
			t := all[to]
			if strings.TrimSpace(t) == "" || indentOf(t) <= indent {
				break
			}
			to++
		}
		return line, to, true
	}
	from = line
	for from > 0 && strings.TrimSpace(all[from-1]) != "" {
		from--
	}
	to = line + 1
	for to < len(all) && strings.TrimSpace(all[to]) != "" {
		to++
	}
	return from, to, true
}

// StripBlockMarker removes a trailing "^id" marker from a line, returning the line otherwise intact.
// Lines without a marker pass through unchanged.
func StripBlockMarker(line string) string {
	m := blockMarker.FindStringSubmatchIndex(line)
	if m == nil {
		return line
	}
	return strings.TrimRight(line[:m[0]], " \t")
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
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
