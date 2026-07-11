package query

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ViewFenceLang is the fence language ExpandBlocks emits for a non-table layout: the body is a
// ready-to-draw View JSON that the frontend renders as a board, gallery, or calendar — mirroring how
// a ```viewspec fence resolves into a ```echarts option block. Layout semantics (grouping, date
// bucketing, covers) are decided here in the engine, so the live workspace and the static export draw
// the same thing from the same payload.
const ViewFenceLang = "track-view"

// View is a laid-out query result: the rows of a Result distributed into named groups, ready to be
// marshaled into a ```track-view fence. Rows link by title (never by internal note id), so a
// published view exposes exactly what a published table does.
type View struct {
	Layout  string      `json:"layout"` // "board", "gallery", or "calendar"
	Key     string      `json:"key,omitempty"`
	Columns []string    `json:"columns"`
	Groups  []ViewGroup `json:"groups"`
}

// ViewGroup is one lane of a view: a board column (Name is the grouping value), a calendar day
// (Name is YYYY-MM-DD), or the single unnamed group of a gallery.
type ViewGroup struct {
	Name string    `json:"name,omitempty"`
	Rows []ViewRow `json:"rows"`
}

// ViewRow is one note on a card: its title (the link target), the query's cells, and — in a gallery —
// its cover image reference ("assets/<file>", already in the form the rendering surface serves).
type ViewRow struct {
	Title string   `json:"title"`
	Cells []string `json:"cells"`
	Cover string   `json:"cover,omitempty"`
}

// dayPrefix matches the YYYY-MM-DD day a calendar cell is placed on; a datetime value places on its
// day, anything without a leading ISO date is undated and stays off the grid.
var dayPrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// BuildView distributes a query result into the groups of a layout. by names the grouping column
// (board) or the date-valued column (calendar); empty defaults to the first non-title column. cover
// supplies a note's cover image for gallery cards (nil = no covers).
func BuildView(layout, by string, res Result, cover func(noteID int64) string) (View, error) {
	v := View{Layout: layout, Columns: res.Columns, Groups: []ViewGroup{}}
	if layout == "gallery" {
		rows := make([]ViewRow, 0, len(res.Rows))
		for _, r := range res.Rows {
			row := ViewRow{Title: r.Title, Cells: r.Cells}
			if cover != nil {
				row.Cover = cover(r.NoteID)
			}
			rows = append(rows, row)
		}
		v.Groups = append(v.Groups, ViewGroup{Rows: rows})
		return v, nil
	}

	idx, err := keyColumn(res.Columns, by)
	if err != nil {
		return View{}, err
	}
	v.Key = res.Columns[idx]
	switch layout {
	case "board":
		v.Groups = boardGroups(res, idx)
	case "calendar":
		v.Groups = calendarGroups(res, idx)
	default:
		return View{}, fmt.Errorf("unknown layout %q (table, board, gallery, calendar)", layout)
	}
	return v, nil
}

// keyColumn resolves the grouping column: an explicit :by key must be one of the TABLE columns (the
// cells are the only values a result carries); the default is the first non-title column.
func keyColumn(columns []string, by string) (int, error) {
	if by != "" {
		for i, c := range columns {
			if c == by {
				return i, nil
			}
		}
		return 0, fmt.Errorf(":by %s must be one of the TABLE columns", by)
	}
	for i, c := range columns {
		if c != "title" {
			return i, nil
		}
	}
	return 0, fmt.Errorf("this layout needs a grouping column: add one to TABLE or name it with :by")
}

// boardGroups lanes rows by their grouping cell, lanes ordered by first appearance (so SORT orders
// the lanes too). Rows without a value gather in a trailing "(no <key>)" lane.
func boardGroups(res Result, idx int) []ViewGroup {
	order := []string{}
	byName := map[string][]ViewRow{}
	var missing []ViewRow
	for _, r := range res.Rows {
		name := strings.TrimSpace(r.Cells[idx])
		if name == "" {
			missing = append(missing, ViewRow{Title: r.Title, Cells: r.Cells})
			continue
		}
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		byName[name] = append(byName[name], ViewRow{Title: r.Title, Cells: r.Cells})
	}
	groups := make([]ViewGroup, 0, len(order)+1)
	for _, name := range order {
		groups = append(groups, ViewGroup{Name: name, Rows: byName[name]})
	}
	if len(missing) > 0 {
		groups = append(groups, ViewGroup{Name: "(no " + res.Columns[idx] + ")", Rows: missing})
	}
	return groups
}

// calendarGroups buckets rows onto days by the leading YYYY-MM-DD of their date cell, days ascending
// (ISO days sort chronologically as text). Undated rows stay off the grid.
func calendarGroups(res Result, idx int) []ViewGroup {
	days := []string{}
	byDay := map[string][]ViewRow{}
	for _, r := range res.Rows {
		day := dayPrefix.FindString(strings.TrimSpace(r.Cells[idx]))
		if day == "" {
			continue
		}
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], ViewRow{Title: r.Title, Cells: r.Cells})
	}
	sort.Strings(days)
	groups := make([]ViewGroup, 0, len(days))
	for _, day := range days {
		groups = append(groups, ViewGroup{Name: day, Rows: byDay[day]})
	}
	return groups
}
