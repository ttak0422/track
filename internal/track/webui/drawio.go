package webui

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
)

// Positions always refer to the original request body, never expanded Markdown.
// A per-render marker survives dashboard/query expansion without giving generated
// or included diagrams an editing capability.
type drawioFence struct {
	ID        string `json:"id"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	XML       string `json:"xml"`
}

func markDrawioFences(body string) (string, []drawioFence) {
	lines := strings.Split(body, "\n")
	var fences []drawioFence
	token := rand.Text()
	topLevel := topLevelDrawioFences(lines)
	for _, b := range babel.ParseBlocks(body) {
		// Only complete, unindented backtick fences from this note. Nested/list
		// fences and attachments remain read-only, even if they contain identical XML.
		if !strings.EqualFold(b.Language, "drawio") || !topLevel[b.StartLine] || !strings.HasPrefix(lines[b.EndLine], "`") {
			continue
		}
		id := fmt.Sprintf("track-drawio-%s-%d", token, len(fences))
		fences = append(fences, drawioFence{ID: id, StartLine: b.StartLine, EndLine: b.EndLine, XML: b.Body})
		lines[b.StartLine] = strings.TrimSuffix(lines[b.StartLine], "\r") + " " + id
	}
	return strings.Join(lines, "\n"), fences
}

// Babel uses backtick fences; CommonMark also permits tilde wrappers. Respect
// those wrappers so annotating a quoted example never changes its visible text.
func topLevelDrawioFences(lines []string) map[int]bool {
	starts := make(map[int]bool)
	var delimiter byte
	length := 0
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
			continue
		}
		char := trimmed[0]
		if char != '`' && char != '~' {
			continue
		}
		n := 0
		for n < len(trimmed) && trimmed[n] == char {
			n++
		}
		if delimiter != 0 {
			if char == delimiter && n >= length && strings.TrimSpace(trimmed[n:]) == "" {
				delimiter = 0
			}
			continue
		}
		if n < 3 {
			continue
		}
		if char == '`' && strings.Contains(trimmed[n:], "`") {
			continue
		}
		delimiter, length = char, n
		if char == '`' && len(trimmed) == len(line) {
			starts[i] = true
		}
	}
	return starts
}
