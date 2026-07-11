package query

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
)

// FenceLang is the fence language that marks an embedded query (```track-query ... ```), mirroring
// how ```viewspec marks an embedded chart. The fence body is a query expression, or "saved: <name>"
// referencing a named query from config.
const FenceLang = "track-query"

// ExpandBlocks replaces every fenced ```track-query block in body with its result rendered as a GFM
// Markdown table, so the block draws as a live table wherever notes render — no dedicated frontend
// component needed. A bad query is replaced by an inline error plus the original expression, so the
// note still renders and the typo is visible at the block position. saved supplies named queries for
// "saved: <name>" bodies; rows is the query domain.
func ExpandBlocks(body string, saved map[string]string, rows []NoteRow) string {
	return babel.ReplaceBlocks(body, FenceLang, func(b babel.Block) []string {
		expr, err := ResolveSaved(b.Body, saved)
		if err == nil {
			var q Query
			if q, err = Parse(expr); err == nil {
				return strings.Split(Markdown(Run(q, rows)), "\n")
			}
		}
		return []string{"> Query error: " + err.Error(), "", "```", strings.TrimSpace(b.Body), "```"}
	})
}

// ResolveSaved resolves a fence body to a query expression: "saved: <name>" looks the expression up
// in the named queries from config; anything else is the expression itself.
func ResolveSaved(body string, saved map[string]string) (string, error) {
	expr := strings.TrimSpace(body)
	name, ok := strings.CutPrefix(expr, "saved:")
	if !ok {
		return expr, nil
	}
	name = strings.TrimSpace(name)
	if q, ok := saved[name]; ok {
		return q, nil
	}
	return "", fmt.Errorf("no saved query %q (config queries:)", name)
}

// Markdown renders a result as a GFM table. The title column links each note as [[Title]], which
// every rendering surface already resolves like any other wiki link; other cells are plain text. A
// props.<name> column shows just <name> in the header — the props. prefix disambiguates the query,
// not the reader's table.
func Markdown(res Result) string {
	if len(res.Rows) == 0 {
		return "_No results._"
	}
	headers := make([]string, len(res.Columns))
	for i, col := range res.Columns {
		if name, ok := propName(col); ok {
			headers[i] = name
		} else {
			headers[i] = col
		}
	}
	var b strings.Builder
	b.WriteString("| " + strings.Join(escapeCells(headers), " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(res.Columns)))
	for _, row := range res.Rows {
		cells := escapeCells(row.Cells)
		for i, col := range res.Columns {
			if col == "title" && row.Title != "" {
				cells[i] = "[[" + row.Title + "]]"
			}
		}
		b.WriteString("\n| " + strings.Join(cells, " | ") + " |")
	}
	return b.String()
}

// escapeCells keeps cell text inside its table cell: pipes are escaped and newlines flattened.
func escapeCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		c = strings.ReplaceAll(c, "\n", " ")
		out[i] = strings.ReplaceAll(c, "|", "\\|")
	}
	return out
}
