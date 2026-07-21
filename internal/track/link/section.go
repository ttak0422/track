package link

// This file is section surgery over heading anchors: the store-free text operations behind
// capture, refile, and archive. They reuse the same heading grammar and section bounds as
// includes (Extract), so what a command moves is exactly what an ![[note##heading]] embed shows.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SplitAnchor parses a "<note>[#heading]" target — the same grammar as a [[...]] link target —
// into its note key and optional heading anchor. The number of "#" gives the heading level. It
// shares the link package's anchor grammar, so a "#^id" block anchor yields no heading (block ids
// are not heading surgery targets), the same way link resolution treats them.
func SplitAnchor(target string) (key, heading string, level int) {
	a := splitAnchor(strings.TrimSpace(target))
	return a.Key, a.Heading, a.HeadingLevel
}

// ResolveAnchor resolves a heading anchor to a single heading, for edit operations that must not
// guess: navigation may adopt the first match, but a command that moves or appends text refuses
// ambiguity instead. An exact (level, text) match wins when unique; with no exact match a
// text-only match is accepted when unique, so "Note#Tasks" finds a lone "## Tasks" without the
// caller spelling the level. Anything else — no match, or several candidates — is an error.
func ResolveAnchor(body string, heading string, level int) (Heading, error) {
	var exact, byText []Heading
	for _, h := range Headings(body) {
		if h.Text != heading {
			continue
		}
		byText = append(byText, h)
		if h.Level == level {
			exact = append(exact, h)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = byText
	}
	switch len(pick) {
	case 0:
		return Heading{}, fmt.Errorf("heading %q not found", heading)
	case 1:
		return pick[0], nil
	default:
		lines := make([]string, len(pick))
		for i, h := range pick {
			lines[i] = strconv.Itoa(h.Line + 1)
		}
		return Heading{}, fmt.Errorf("heading %q is ambiguous (lines %s); write more '#' to pick a level",
			heading, strings.Join(lines, ", "))
	}
}

// sectionEnd returns the exclusive 0-based end line of h's section: the next heading at the same
// or a shallower level, or the line count. Headings inside fenced code blocks do not terminate a
// section, mirroring Extract.
func sectionEnd(all []string, h Heading) int {
	end := len(all)
	for _, other := range Headings(strings.Join(all, "\n")) {
		if other.Line > h.Line && other.Level <= h.Level {
			end = other.Line
			break
		}
	}
	return end
}

// CutSection removes h's section — the heading line through the line before the next heading at
// the same or a shallower level — from body. It returns the remaining body and the removed lines
// with trailing blank lines trimmed, ready for insertion elsewhere. Deeper headings inside the
// section move with it.
func CutSection(body string, h Heading) (rest string, section []string) {
	all := strings.Split(body, "\n")
	end := sectionEnd(all, h)
	section = append([]string(nil), trimBlankEdges(all[h.Line:end])...)
	rest = strings.Join(append(all[:h.Line:h.Line], all[end:]...), "\n")
	return rest, section
}

// listItemLine matches the marker of a Markdown list item: indentation, then -, *, +, or an
// ordered "1." / "1)" marker, then whitespace. Group 1 is the indentation.
var listItemLine = regexp.MustCompile(`^([ \t]*)(?:[-*+]|\d+[.)])[ \t]`)

// CutListItem removes the list item at 1-based line n plus its continuation: the following lines
// indented deeper than the item's marker, including interior blank lines when deeper content
// follows them (a trailing blank stays with the source). It errors when n is out of range or the
// line is not a list item.
func CutListItem(body string, n int) (rest string, item []string, err error) {
	all := strings.Split(body, "\n")
	if n < 1 || n > len(all) {
		return "", nil, fmt.Errorf("line %d is out of range (note has %d lines)", n, len(all))
	}
	m := listItemLine.FindStringSubmatch(all[n-1])
	if m == nil {
		return "", nil, fmt.Errorf("line %d is not a list item: %q", n, strings.TrimSpace(all[n-1]))
	}
	// ponytail: indentation compares byte counts, so a tab counts as one column; switch to
	// tab-expanded widths if mixed-indent vaults show up.
	indent := len(m[1])
	end := n
	for i := n; i < len(all); i++ {
		if strings.TrimSpace(all[i]) == "" {
			continue // included only when deeper-indented content follows
		}
		if leadingWS(all[i]) <= indent {
			break
		}
		end = i + 1
	}
	item = append([]string(nil), all[n-1:end]...)
	rest = strings.Join(append(all[:n-1:n-1], all[end:]...), "\n")
	return rest, item, nil
}

func leadingWS(s string) int {
	return len(s) - len(strings.TrimLeft(s, " \t"))
}

// AppendUnder appends block after the last non-blank line of h's section and returns the new body
// (single trailing newline) plus the 1-based line the block starts on. A nil h appends at the end
// of the note. Blank edges of block are trimmed; a single blank line separates it from what
// precedes it — except between two list items, so captured entries pack into an existing list —
// and from any non-blank line that follows (e.g. the next heading).
func AppendUnder(body string, h *Heading, block []string) (string, int) {
	block = trimBlankEdges(block)
	if len(block) == 0 {
		return body, 0
	}
	all := strings.Split(strings.TrimRight(body, "\n"), "\n")
	at := len(all)
	if h != nil {
		at = sectionEnd(all, *h)
	}
	for at > 0 && strings.TrimSpace(all[at-1]) == "" {
		at--
	}
	out := make([]string, 0, len(all)+len(block)+2)
	out = append(out, all[:at]...)
	if at > 0 && !(isListItem(all[at-1]) && isListItem(block[0])) {
		out = append(out, "")
	}
	startLine := len(out) + 1
	out = append(out, block...)
	if at < len(all) && strings.TrimSpace(all[at]) != "" {
		out = append(out, "")
	}
	out = append(out, all[at:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", startLine
}

func isListItem(line string) bool {
	return listItemLine.MatchString(line)
}
