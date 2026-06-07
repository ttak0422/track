package lsp

import (
	"regexp"
	"sort"
	"strings"
)

// mentionTerm is a keyword (note title or alias) that may appear as plain text in a note body.
// Term is what is searched for in the text; Title is the canonical title the link should point to,
// so an alias mention still rewrites to [[<canonical title>]].
type mentionTerm struct {
	Term  string
	Title string
}

// mentionMatch is one plain-text occurrence of a keyword found outside any excluded region.
// Byte offsets are within the occurrence's line; Line is 0-based.
type mentionMatch struct {
	Line      int
	StartByte int
	EndByte   int
	Title     string
}

// mentionFence detects fenced code block delimiters, matching link.scannableLines.
var (
	mentionWikiLink   = regexp.MustCompile(`\[\[[^\[\]]+\]\]`)
	mentionInlineCode = regexp.MustCompile("`[^`]+`")
	mentionMDLink     = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
	mentionURL        = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://\S+`)
)

// mentions finds plain-text occurrences of terms in text that are eligible for link suggestions.
// It is deliberately store-free and side-effect-free: callers assemble terms (e.g. from the keyword
// dictionary, minus the current note) and pass the document text, which keeps the scan a pure function
// of (text, terms) and makes it straightforward to move behind a per-document cache or goroutine later.
//
// The scan skips regions where a link would be wrong: fenced code blocks, inline code, existing
// [[...]] links, markdown links ([text](url)), bare URLs, and h1 title lines. When several terms match
// at the same place, the longest match wins so "test note" is preferred over "test".
func mentions(text string, terms []mentionTerm) []mentionMatch {
	if len(terms) == 0 {
		return nil
	}
	// Longest term first so a longer match claims its span before any shorter overlapping term.
	sorted := make([]mentionTerm, len(terms))
	copy(sorted, terms)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Term) > len(sorted[j].Term)
	})

	var out []mentionMatch
	inFence := false
	for lineNo, line := range strings.Split(text, "\n") {
		if isMentionFence(line) {
			inFence = !inFence
			continue
		}
		if inFence || isH1Line(line) {
			continue
		}
		excluded := excludedSpans(line)
		used := make([]bool, len(line))
		for _, t := range sorted {
			if t.Term == "" {
				continue
			}
			for _, span := range findTermSpans(line, t.Term) {
				if spanOverlaps(excluded, span[0], span[1]) || anyUsed(used, span[0], span[1]) {
					continue
				}
				markUsed(used, span[0], span[1])
				out = append(out, mentionMatch{
					Line:      lineNo,
					StartByte: span[0],
					EndByte:   span[1],
					Title:     t.Title,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].StartByte < out[j].StartByte
	})
	return out
}

func isMentionFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func isH1Line(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "# ")
}

// excludedSpans returns the byte ranges of a line where a plain-text mention must not be reported:
// existing [[...]] links, inline code, markdown links, and bare URLs.
func excludedSpans(line string) [][2]int {
	var spans [][2]int
	for _, re := range []*regexp.Regexp{mentionWikiLink, mentionInlineCode, mentionMDLink, mentionURL} {
		for _, m := range re.FindAllStringIndex(line, -1) {
			spans = append(spans, [2]int{m[0], m[1]})
		}
	}
	return spans
}

// findTermSpans returns every non-overlapping byte range where term appears as a standalone token.
// A match is rejected when it sits mid-word against an ASCII word character on either side, so "test"
// does not match inside "latest" or "testing"; CJK text (no ASCII word boundary) still matches.
func findTermSpans(line, term string) [][2]int {
	var spans [][2]int
	for from := 0; from < len(line); {
		i := strings.Index(line[from:], term)
		if i < 0 {
			break
		}
		start := from + i
		end := start + len(term)
		if termBoundaryOK(line, start, end) {
			spans = append(spans, [2]int{start, end})
			from = end
		} else {
			from = start + 1
		}
	}
	return spans
}

func termBoundaryOK(line string, start, end int) bool {
	if start > 0 && isWordByte(line[start-1]) && isWordByte(line[start]) {
		return false
	}
	if end < len(line) && isWordByte(line[end-1]) && isWordByte(line[end]) {
		return false
	}
	return true
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

func spanOverlaps(spans [][2]int, start, end int) bool {
	for _, sp := range spans {
		if start < sp[1] && sp[0] < end {
			return true
		}
	}
	return false
}

func anyUsed(used []bool, start, end int) bool {
	for i := start; i < end && i < len(used); i++ {
		if used[i] {
			return true
		}
	}
	return false
}

func markUsed(used []bool, start, end int) {
	for i := start; i < end && i < len(used); i++ {
		used[i] = true
	}
}
