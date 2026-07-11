package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func viewRows() []NoteRow {
	return []NoteRow{
		{ID: 1, Title: "Alpha", Props: []note.Prop{
			{Key: "state", Value: "doing", Type: note.TypeString},
			{Key: "due", Value: "2026-07-02", Type: note.TypeDate},
		}},
		{ID: 2, Title: "Beta", Props: []note.Prop{
			{Key: "state", Value: "todo", Type: note.TypeString},
			{Key: "due", Value: "2026-07-01", Type: note.TypeDate},
		}},
		{ID: 3, Title: "Gamma", Props: []note.Prop{
			{Key: "state", Value: "doing", Type: note.TypeString},
			{Key: "due", Value: "2026-08-15T09:00", Type: note.TypeDate},
		}},
		{ID: 4, Title: "Delta"}, // no state, no due
	}
}

func buildView(t *testing.T, layout, by, expr string, cover func(int64) string) View {
	t.Helper()
	q, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	v, err := BuildView(layout, by, Run(q, viewRows()), cover)
	if err != nil {
		t.Fatalf("BuildView(%s): %v", layout, err)
	}
	return v
}

func groupNames(v View) []string {
	out := make([]string, 0, len(v.Groups))
	for _, g := range v.Groups {
		out = append(out, g.Name)
	}
	return out
}

func groupTitles(g ViewGroup) []string {
	out := make([]string, 0, len(g.Rows))
	for _, r := range g.Rows {
		out = append(out, r.Title)
	}
	return out
}

func TestBoardGroupsByColumnFirstAppearance(t *testing.T) {
	v := buildView(t, "board", "state", "TABLE title, state", nil)
	if v.Key != "state" {
		t.Fatalf("key = %q, want state", v.Key)
	}
	// Row order is id order (equal mtimes); lanes appear in first-appearance order, missing last.
	if got := groupNames(v); !reflect.DeepEqual(got, []string{"doing", "todo", "(no state)"}) {
		t.Fatalf("groups = %v", got)
	}
	if got := groupTitles(v.Groups[0]); !reflect.DeepEqual(got, []string{"Alpha", "Gamma"}) {
		t.Fatalf("doing lane = %v", got)
	}
	if got := groupTitles(v.Groups[2]); !reflect.DeepEqual(got, []string{"Delta"}) {
		t.Fatalf("missing lane = %v", got)
	}
}

func TestBoardDefaultsToFirstNonTitleColumn(t *testing.T) {
	v := buildView(t, "board", "", "TABLE title, state", nil)
	if v.Key != "state" {
		t.Fatalf("default key = %q, want state", v.Key)
	}
	if _, err := BuildView("board", "", Result{Columns: []string{"title"}, Rows: []Row{{Title: "x", Cells: []string{"x"}}}}, nil); err == nil {
		t.Fatal("board over only title must error")
	}
	if _, err := BuildView("board", "owner", Result{Columns: []string{"title"}, Rows: []Row{{Title: "x", Cells: []string{"x"}}}}, nil); err == nil {
		t.Fatal(":by outside TABLE columns must error")
	}
}

func TestGalleryCarriesCovers(t *testing.T) {
	covers := map[int64]string{1: "assets/a.png", 3: "assets/g.png"}
	v := buildView(t, "gallery", "", "TABLE title", func(id int64) string { return covers[id] })
	if len(v.Groups) != 1 || v.Key != "" {
		t.Fatalf("gallery view = %+v", v)
	}
	got := map[string]string{}
	for _, r := range v.Groups[0].Rows {
		got[r.Title] = r.Cover
	}
	want := map[string]string{"Alpha": "assets/a.png", "Beta": "", "Gamma": "assets/g.png", "Delta": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("covers = %v, want %v", got, want)
	}
}

func TestCalendarBucketsByDayAscending(t *testing.T) {
	v := buildView(t, "calendar", "due", "TABLE title, due SORT due DESC", nil)
	// Days sort ascending regardless of query SORT; the datetime value lands on its day; the
	// undated row (Delta) stays off the grid.
	if got := groupNames(v); !reflect.DeepEqual(got, []string{"2026-07-01", "2026-07-02", "2026-08-15"}) {
		t.Fatalf("days = %v", got)
	}
	if got := groupTitles(v.Groups[2]); !reflect.DeepEqual(got, []string{"Gamma"}) {
		t.Fatalf("2026-08-15 = %v", got)
	}
}

func TestExpandBlocksLayouts(t *testing.T) {
	got := ExpandBlocks("```track-query :layout board :by state\nTABLE title, state\n```", nil, viewRows(), nil)
	if !strings.HasPrefix(got, "```track-view\n") || !strings.HasSuffix(got, "\n```") {
		t.Fatalf("board expansion must be a track-view fence:\n%s", got)
	}
	var v View
	if err := json.Unmarshal([]byte(strings.Split(got, "\n")[1]), &v); err != nil {
		t.Fatalf("fence body is not View JSON: %v", err)
	}
	if v.Layout != "board" || len(v.Groups) != 3 {
		t.Fatalf("view = %+v", v)
	}

	// An unknown layout and a bad :by degrade to the inline error, keeping the source visible.
	for _, body := range []string{
		"```track-query :layout waterfall\nTABLE title\n```",
		"```track-query :layout board :by owner\nTABLE title, state\n```",
	} {
		if got := ExpandBlocks(body, nil, viewRows(), nil); !strings.Contains(got, "> Query error:") {
			t.Fatalf("expected inline error for %q, got:\n%s", body, got)
		}
	}

	// An empty result renders the shared no-results text, not an empty board.
	got = ExpandBlocks("```track-query :layout board :by state\nTABLE title, state WHERE state = nope\n```", nil, viewRows(), nil)
	if got != "_No results._" {
		t.Fatalf("empty layout = %q", got)
	}

	// The default layout stays the Markdown table.
	got = ExpandBlocks("```track-query\nTABLE title WHERE state = todo\n```", nil, viewRows(), nil)
	if !strings.Contains(got, "| [[Beta]] |") {
		t.Fatalf("table expansion = %q", got)
	}
}
