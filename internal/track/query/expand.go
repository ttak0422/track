package query

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
)

// FenceLang is the fence language that marks an embedded query (```track-query ... ```), mirroring
// how ```viewspec marks an embedded chart. The fence body is a query expression, or "saved: <name>"
// referencing a named query from config. Org-style header arguments on the fence choose the layout:
// ":layout table|board|gallery|calendar" (default table) and ":by <column>" (the board grouping /
// calendar date column).
const FenceLang = "track-query"

// ExpandBlocks replaces every fenced ```track-query block in body with its rendered result: a GFM
// Markdown table by default, or — for a board/gallery/calendar :layout — a ```track-view fence whose
// body is the laid-out View JSON the frontend draws. A bad query is replaced by an inline error plus
// the original expression, so the note still renders and the typo is visible at the block position.
// saved supplies named queries for "saved: <name>" bodies; rows is the query domain; cover supplies
// note cover images for gallery cards (nil = no covers).
func ExpandBlocks(body string, saved map[string]string, rows []NoteRow, cover func(noteID int64) string) string {
	return babel.ReplaceBlocks(body, FenceLang, func(b babel.Block) []string {
		expr, err := ResolveSaved(b.Body, saved)
		if err == nil {
			var q Query
			if q, err = Parse(expr); err == nil {
				var lines []string
				if lines, err = resultLines(b, Run(q, rows), cover); err == nil {
					return lines
				}
			}
		}
		return []string{"> Query error: " + err.Error(), "", "```", strings.TrimSpace(b.Body), "```"}
	})
}

// resultLines renders one evaluated block per its :layout header argument. An empty result renders
// as the table path's "no results" text in every layout — an empty board or month grid would just
// look broken.
func resultLines(b babel.Block, res Result, cover func(int64) string) ([]string, error) {
	layout := headerArg(b, "layout")
	if layout == "" || layout == "table" || len(res.Rows) == 0 {
		return strings.Split(Markdown(res), "\n"), nil
	}
	v, err := BuildView(layout, headerArg(b, "by"), res, cover)
	if err != nil {
		return nil, err
	}
	// json.Marshal emits a single line, so the payload can never contain a fence-closing line.
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return []string{"```" + ViewFenceLang, string(data), "```"}, nil
}

// headerArg returns the first value of an Org-style ":key value" fence header argument.
func headerArg(b babel.Block, key string) string {
	if vs := b.HeaderArgs[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
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
// every rendering surface already resolves like any other wiki link; other cells are plain text.
func Markdown(res Result) string {
	if len(res.Rows) == 0 {
		return "_No results._"
	}
	var b strings.Builder
	b.WriteString("| " + strings.Join(escapeCells(res.Columns), " | ") + " |\n")
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
