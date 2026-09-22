package provenance

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
)

// Position selects at most one heading, block, physical page, or inclusive line range.
// Level optionally constrains Heading; an empty Position selects the whole body.
type Position struct {
	Heading   string `json:"heading,omitempty"`
	Level     int    `json:"level,omitempty"`
	Block     string `json:"block,omitempty"`
	Page      int    `json:"page,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// Selection preserves the original bytes, including block markers and line endings.
// Lines are 1-based; an empty selection has a 0..0 range. A final newline belongs
// to its preceding line and does not create an extra line.
type Selection struct {
	Body      string `json:"body"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Page      int    `json:"page,omitempty"`
}

// Select resolves an exact position without falling back to another location.
// Pages require explicit form-feed boundaries; printed page labels are not parsed.
func Select(body string, position Position) (Selection, error) {
	if position.Level < 0 || position.Level > 6 || (position.Level != 0 && position.Heading == "") {
		return Selection{}, fmt.Errorf("heading level must be 1..6 and requires a heading")
	}
	if position.Page < 0 || position.StartLine < 0 || position.EndLine < 0 {
		return Selection{}, fmt.Errorf("page and line numbers must be positive")
	}
	selectors := 0
	for _, present := range []bool{position.Heading != "", position.Block != "", position.Page != 0, position.StartLine != 0 || position.EndLine != 0} {
		if present {
			selectors++
		}
	}
	if selectors > 1 {
		return Selection{}, fmt.Errorf("choose only one heading, block, page, or line range")
	}
	if position.Page != 0 {
		if !strings.Contains(body, "\f") {
			return Selection{}, fmt.Errorf("page selection requires explicit form-feed boundaries")
		}
		pages := strings.Split(body, "\f")
		// A final form feed terminates the last physical page. Note writes add a
		// trailing newline, which belongs to that terminator rather than a new page.
		if strings.Trim(pages[len(pages)-1], "\r\n") == "" {
			pages = pages[:len(pages)-1]
		}
		if position.Page > len(pages) {
			return Selection{}, fmt.Errorf("page %d is out of range (document has %d pages)", position.Page, len(pages))
		}
		start := 0
		for _, page := range pages[:position.Page-1] {
			start += len(page) + 1
		}
		selection := selectBytes(body, start, start+len(pages[position.Page-1]))
		selection.Page = position.Page
		return selection, nil
	}
	lines := strings.SplitAfter(body, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	from, to := 0, len(lines)
	// The shared Markdown parser matches normalized CRLF text. Offsets and evidence
	// always come from the original body, so normalization never changes the result.
	markdown := strings.ReplaceAll(body, "\r\n", "\n")
	switch {
	case position.Heading != "":
		headings := link.Headings(markdown)
		var matches []link.Heading
		for _, heading := range headings {
			if heading.Text == position.Heading && (position.Level == 0 || heading.Level == position.Level) {
				matches = append(matches, heading)
			}
		}
		if len(matches) != 1 {
			return Selection{}, fmt.Errorf("heading %q must match exactly once (found %d)", position.Heading, len(matches))
		}
		heading := matches[0]
		from = heading.Line
		for _, next := range headings {
			if next.Line > from && next.Level <= heading.Level {
				to = next.Line
				break
			}
		}
	case position.Block != "":
		matches := 0
		for _, block := range link.Blocks(markdown) {
			if block.ID == position.Block {
				matches++
			}
		}
		if matches != 1 {
			return Selection{}, fmt.Errorf("block %q must match exactly once (found %d)", position.Block, matches)
		}
		from, to, _ = link.FindBlock(markdown, position.Block)
	case position.StartLine != 0 || position.EndLine != 0:
		if position.StartLine < 1 || position.EndLine < position.StartLine || position.EndLine > len(lines) {
			return Selection{}, fmt.Errorf("invalid line range %d..%d (document has %d lines)", position.StartLine, position.EndLine, len(lines))
		}
		from, to = position.StartLine-1, position.EndLine
	}
	start := len(strings.Join(lines[:from], ""))
	return selectBytes(body, start, start+len(strings.Join(lines[from:to], ""))), nil
}

func selectBytes(body string, start, end int) Selection {
	selection := Selection{Body: body[start:end]}
	if start != end {
		selection.StartLine = strings.Count(body[:start], "\n") + 1
		selection.EndLine = selection.StartLine + strings.Count(strings.TrimSuffix(selection.Body, "\n"), "\n")
	}
	return selection
}
