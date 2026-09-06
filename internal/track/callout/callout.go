// Package callout extracts GitHub-style admonition blockquotes from note text. A callout is a
// blockquote whose first line begins with a "[!TYPE]" marker (NOTE, TIP, IMPORTANT, WARNING, or
// CAUTION); the web frontend renders it as a colored box, and the Neovim client highlights the marker
// line and drops a sign-column indicator per type. This package is the engine-side parsing contract
// that both surfaces could converge on; it is store-free so it stays cheap and unit-testable.
package callout

import (
	"regexp"
	"strings"
)

// Type is one of the five recognized callout kinds. The string is the canonical lowercase form.
type Type string

const (
	Note      Type = "note"
	Tip       Type = "tip"
	Important Type = "important"
	Warning   Type = "warning"
	Caution   Type = "caution"
)

// Callout is one admonition blockquote occurrence in note text. Lines are 0-based; EndLine is the
// 0-based line number of the last line of the block (inclusive).
type Callout struct {
	Type      Type
	StartLine int // 0-based line of the "[!TYPE]" marker (and the blockquote's first line)
	EndLine   int // 0-based line of the blockquote's last line
}

// marker matches a leading "[!TYPE]" run on a blockquote line. It is anchored to the first non-space
// text so it only fires on the marker position, never on prose that merely quotes the pattern.
var marker = regexp.MustCompile(`(?i)^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\](?:\s|$)`)

// Callouts extracts every "[!TYPE]" admonition blockquote in text, skipping fenced code blocks. A
// callout block is the maximal consecutive run of blockquote lines (" > " prefixed) whose first line
// carries the marker.
func Callouts(text string) []Callout {
	lines := strings.Split(text, "\n")
	var out []Callout
	inFence := false
	for i := 0; i < len(lines); i++ {
		if isFence(lines[i]) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		content, ok := blockquoteContent(lines[i])
		if !ok {
			continue
		}
		m := marker.FindStringSubmatch(content)
		if m == nil {
			continue
		}
		typ := Type(strings.ToLower(m[1]))
		end := i
		for end+1 < len(lines) {
			if _, isQuote := blockquoteContent(lines[end+1]); isQuote {
				end++
			} else {
				break
			}
		}
		out = append(out, Callout{Type: typ, StartLine: i, EndLine: end})
		i = end
	}
	return out
}

// TypeFromMarker maps a lowercase type string to its canonical Type, or "" when unknown. It is the
// shared vocabulary between the parser and callers that read a type name (e.g. a highlight-group key).
func TypeFromMarker(s string) Type {
	t := Type(strings.ToLower(strings.TrimSpace(s)))
	switch t {
	case Note, Tip, Important, Warning, Caution:
		return t
	}
	return ""
}

// blockquoteContent strips a blockquote's ">" prefix plus one following space from a line, reporting
// whether the line is a blockquote at all. A lone ">" (a blank blockquote line) still counts as a
// blockquote line, so a wrapped callout block that wraps with a bare ">" stays one block.
func blockquoteContent(line string) (content string, isQuote bool) {
	stripped := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(stripped, ">") {
		return "", false
	}
	rest := stripped[1:]
	if strings.HasPrefix(rest, " ") {
		rest = rest[1:]
	}
	return rest, true
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}
