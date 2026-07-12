// Package mdfmt applies canonical Markdown formatting to note bodies. It is the style counterpart to
// package doctor: doctor finds breakage, mdfmt fixes style. The rule set is intentionally small and
// idempotent (Format(Format(x)) == Format(x)):
//
//  1. Strip trailing whitespace (spaces, tabs, carriage returns) from every line.
//  2. Collapse runs of blank lines to a single blank line, and drop blank lines at the start and end
//     of the document.
//  3. Ensure exactly two blank lines before and one blank line after each heading.
//  4. Normalize unordered-list markers to "-" (from "*" or "+").
//  5. Ensure the document ends with exactly one newline.
//
// Fenced code blocks (``` or ~~~) are never touched: their content, including blank lines and any
// list-like or heading-like lines, passes through verbatim. The line-oriented rules only ever touch a
// line's end (rule 1) or its leading marker (rule 4), so inline code span content, which lives in the
// interior of a line, is never rewritten.
//
// ponytail: indented (4-space) code blocks are not specially protected — fenced blocks are the
// protected construct. Notes should fence their code; upgrade to full block parsing only if indented
// code blocks in the wild start getting mangled.
package mdfmt

import "strings"

// Format returns src rewritten to canonical form.
func Format(src string) string {
	items := scan(src)

	out := make([]string, 0, len(items))
	pendingBlank := false     // an unprotected blank line is waiting to be emitted before the next content
	forceBlankBefore := false // the last emitted line was a heading, so the next content needs a blank first

	emit := func(text string, heading bool) {
		requiredBlanks := 0
		if heading {
			requiredBlanks = 2
		} else if pendingBlank || forceBlankBefore {
			requiredBlanks = 1
		}

		if len(out) > 0 && requiredBlanks > 0 {
			trailingBlanks := 0
			for i := len(out) - 1; i >= 0 && out[i] == ""; i-- {
				trailingBlanks++
			}
			for trailingBlanks < requiredBlanks {
				out = append(out, "")
				trailingBlanks++
			}
		}
		out = append(out, text)
		pendingBlank = false
		forceBlankBefore = heading
	}

	for _, it := range items {
		switch {
		case it.protected:
			emit(it.text, false) // fenced lines are content, but never headings
		case it.text == "":
			if len(out) > 0 { // drop leading blanks
				pendingBlank = true
			}
		default:
			emit(it.text, isHeading(it.text))
		}
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// item is one source line after fence classification. Protected lines (inside or on the boundary of a
// fenced code block) are kept verbatim; other lines carry their cleaned text.
type item struct {
	text      string
	protected bool
}

// scan splits src into classified lines, tracking fenced-code state so code content is left verbatim.
func scan(src string) []item {
	lines := strings.Split(src, "\n")
	items := make([]item, 0, len(lines))

	inFence := false
	var marker byte
	var fenceLen int

	for _, raw := range lines {
		if inFence {
			items = append(items, item{text: raw, protected: true})
			if m, l, info, ok := scanFence(raw); ok && m == marker && l >= fenceLen && info == "" {
				inFence = false
			}
			continue
		}
		if m, l, _, ok := scanFence(raw); ok {
			inFence = true
			marker = m
			fenceLen = l
			items = append(items, item{text: raw, protected: true})
			continue
		}
		items = append(items, item{text: formatLine(raw), protected: false})
	}
	return items
}

// formatLine applies the per-line rules (normalize list marker, then strip trailing whitespace) to one
// unfenced line. Thematic breaks keep their marker so "* * *" is not read as a list item.
func formatLine(raw string) string {
	if !isThematicBreak(raw) {
		raw = normalizeListMarker(raw)
	}
	return strings.TrimRight(raw, " \t\r")
}

// scanFence reports whether line is a code-fence line and returns its marker char, run length, and
// info string. Follows CommonMark: up to 3 leading spaces, then 3+ backticks or tildes; a backtick
// fence's info string may not contain a backtick.
func scanFence(line string) (marker byte, length int, info string, ok bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 {
		return 0, 0, "", false
	}
	rest := line[indent:]
	if len(rest) < 3 {
		return 0, 0, "", false
	}
	ch := rest[0]
	if ch != '`' && ch != '~' {
		return 0, 0, "", false
	}
	n := 0
	for n < len(rest) && rest[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	info = strings.TrimSpace(rest[n:])
	if ch == '`' && strings.Contains(info, "`") {
		return 0, 0, "", false
	}
	return ch, n, info, true
}

// isHeading reports whether line is an ATX heading (up to 3 leading spaces, 1-6 "#", then a space/tab
// or end of line).
func isHeading(line string) bool {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 {
		return false
	}
	s := line[indent:]
	hashes := 0
	for hashes < len(s) && s[hashes] == '#' {
		hashes++
	}
	if hashes < 1 || hashes > 6 {
		return false
	}
	if hashes == len(s) {
		return true
	}
	return s[hashes] == ' ' || s[hashes] == '\t'
}

// isThematicBreak reports whether line is a thematic break: 3+ of the same "*", "-", or "_", possibly
// separated by spaces or tabs, and nothing else.
func isThematicBreak(line string) bool {
	stripped := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(line))
	if len(stripped) < 3 {
		return false
	}
	c := stripped[0]
	if c != '*' && c != '-' && c != '_' {
		return false
	}
	for i := 0; i < len(stripped); i++ {
		if stripped[i] != c {
			return false
		}
	}
	return true
}

// normalizeListMarker rewrites a leading "*" or "+" list bullet to "-", preserving indentation and the
// rest of the line. A marker is only recognized when followed by a space or tab, so emphasis like
// "*bold*" is left alone.
func normalizeListMarker(line string) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	if n >= len(line) {
		return line
	}
	if line[n] != '*' && line[n] != '+' {
		return line
	}
	if n+1 >= len(line) || (line[n+1] != ' ' && line[n+1] != '\t') {
		return line
	}
	return line[:n] + "-" + line[n+1:]
}
