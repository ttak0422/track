package query

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestParseFullQuery(t *testing.T) {
	q, err := Parse(`TABLE title, status, due FROM #project WHERE status != "done" AND due < 2026-01-01 AND #work AND owner SORT due DESC LIMIT 10`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Query{
		Columns: []string{"title", "status", "due"},
		From:    "project",
		Where: []Cond{
			{Key: "status", Op: "!=", Value: "done"},
			{Key: "due", Op: "<", Value: "2026-01-01"},
			{Tag: "work"},
			{Key: "owner"},
		},
		Sort:  "due",
		Desc:  true,
		Limit: 10,
	}
	if !reflect.DeepEqual(q, want) {
		t.Fatalf("parsed = %+v, want %+v", q, want)
	}
}

func TestParseMinimalQuery(t *testing.T) {
	q, err := Parse("TABLE title")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(q.Columns, []string{"title"}) || q.From != "" || q.Where != nil || q.Sort != "" || q.Limit != 0 {
		t.Fatalf("parsed = %+v", q)
	}
}

func TestParseOperatorsWithoutSpaces(t *testing.T) {
	q, err := Parse("TABLE title WHERE status!=done AND n>3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []Cond{{Key: "status", Op: "!=", Value: "done"}, {Key: "n", Op: ">", Value: "3"}}
	if !reflect.DeepEqual(q.Where, want) {
		t.Fatalf("where = %+v, want %+v", q.Where, want)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		"",
		"SELECT title",
		"TABLE",
		"TABLE title FROM project",  // missing #
		"TABLE title WHERE",         // missing condition
		"TABLE title WHERE due <",   // missing value
		"TABLE title LIMIT many",    // not a number
		"TABLE title SORT due up",   // trailing junk
		`TABLE title WHERE a = "b`,  // unterminated string
		"TABLE title WHERE a ! b",   // lone !
		"TABLE title WHERE a = AND", // keyword as value
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", bad)
		}
	}
}

func testRows() []NoteRow {
	return []NoteRow{
		{ID: 1, Title: "Alpha", Tags: []string{"project/web"}, Props: []note.Prop{
			{Key: "status", Value: "open", Type: note.TypeString},
			{Key: "due", Value: "2026-02-01", Type: note.TypeDate},
			{Key: "points", Value: "9", Type: note.TypeNumber},
		}},
		{ID: 2, Title: "Beta", Tags: []string{"project"}, Props: []note.Prop{
			{Key: "status", Value: "done", Type: note.TypeString},
			{Key: "due", Value: "2026-01-15", Type: note.TypeDate},
			{Key: "points", Value: "10", Type: note.TypeNumber},
		}},
		{ID: 3, Title: "Gamma", Tags: []string{"journal"}, Props: []note.Prop{
			{Key: "status", Value: "open", Type: note.TypeString},
		}},
		{ID: 4, Title: "Delta", Tags: []string{"project", "urgent"}},
	}
}

func run(t *testing.T, expr string) Result {
	t.Helper()
	q, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return Run(q, testRows())
}

func ids(res Result) []int64 {
	out := make([]int64, 0, len(res.Rows))
	for _, r := range res.Rows {
		out = append(out, r.NoteID)
	}
	return out
}

func TestRunFromTagMatchesHierarchically(t *testing.T) {
	if got := ids(run(t, "TABLE title FROM #project")); !reflect.DeepEqual(got, []int64{1, 2, 4}) {
		t.Fatalf("FROM #project = %v, want [1 2 4]", got)
	}
	if got := ids(run(t, "TABLE title FROM #project/web")); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("FROM #project/web = %v, want [1]", got)
	}
	// #pro must not match #project: hierarchy, not substring.
	if got := ids(run(t, "TABLE title FROM #pro")); len(got) != 0 {
		t.Fatalf("FROM #pro = %v, want none", got)
	}
}

func TestRunWhereComparisons(t *testing.T) {
	if got := ids(run(t, "TABLE title WHERE status = open")); !reflect.DeepEqual(got, []int64{1, 3}) {
		t.Fatalf("= : %v", got)
	}
	// != is none-of: a note without the key matches.
	if got := ids(run(t, "TABLE title WHERE status != done")); !reflect.DeepEqual(got, []int64{1, 3, 4}) {
		t.Fatalf("!= : %v", got)
	}
	if got := ids(run(t, "TABLE title WHERE due < 2026-02-01")); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("< date: %v", got)
	}
	// Numbers compare numerically, not lexically (9 < 10).
	if got := ids(run(t, "TABLE title WHERE points > 9")); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("> number: %v", got)
	}
	if got := ids(run(t, "TABLE title WHERE due")); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("presence: %v", got)
	}
	if got := ids(run(t, "TABLE title WHERE #urgent AND status != done")); !reflect.DeepEqual(got, []int64{4}) {
		t.Fatalf("tag AND comparison: %v", got)
	}
}

func TestRunSortAndLimit(t *testing.T) {
	if got := ids(run(t, "TABLE title FROM #project SORT due")); !reflect.DeepEqual(got, []int64{2, 1, 4}) {
		t.Fatalf("sort asc (missing last) = %v, want [2 1 4]", got)
	}
	if got := ids(run(t, "TABLE title FROM #project SORT due DESC")); !reflect.DeepEqual(got, []int64{1, 2, 4}) {
		t.Fatalf("sort desc (missing still last) = %v, want [1 2 4]", got)
	}
	if got := ids(run(t, "TABLE title FROM #project SORT due LIMIT 1")); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("limit = %v, want [2]", got)
	}
}

func TestRunCellsJoinMultiValues(t *testing.T) {
	res := run(t, "TABLE title, tags WHERE #urgent")
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %+v", res.Rows)
	}
	if got := res.Rows[0].Cells; !reflect.DeepEqual(got, []string{"Delta", "project, urgent"}) {
		t.Fatalf("cells = %v", got)
	}
}

func TestMarkdownTable(t *testing.T) {
	md := Markdown(run(t, "TABLE title, status WHERE status = open"))
	want := strings.Join([]string{
		"| title | status |",
		"| --- | --- |",
		"| [[Alpha]] | open |",
		"| [[Gamma]] | open |",
	}, "\n")
	if md != want {
		t.Fatalf("markdown =\n%s\nwant\n%s", md, want)
	}
	if got := Markdown(run(t, "TABLE title WHERE status = nothing-matches")); got != "_No results._" {
		t.Fatalf("empty result = %q", got)
	}
}

func TestExpandBlocks(t *testing.T) {
	body := "before\n\n```track-query\nTABLE title WHERE #urgent\n```\n\nafter"
	got := ExpandBlocks(body, nil, testRows())
	want := "before\n\n| title |\n| --- |\n| [[Delta]] |\n\nafter"
	if got != want {
		t.Fatalf("expanded =\n%s\nwant\n%s", got, want)
	}
}

func TestExpandBlocksSavedAndErrors(t *testing.T) {
	saved := map[string]string{"urgent": "TABLE title WHERE #urgent"}
	got := ExpandBlocks("```track-query\nsaved: urgent\n```", saved, testRows())
	if !strings.Contains(got, "[[Delta]]") {
		t.Fatalf("saved query not expanded: %s", got)
	}

	got = ExpandBlocks("```track-query\nsaved: missing\n```", saved, testRows())
	if !strings.Contains(got, "> Query error:") || !strings.Contains(got, "saved: missing") {
		t.Fatalf("missing saved query should render an inline error with the source: %s", got)
	}

	got = ExpandBlocks("```track-query\nnot a query\n```", nil, testRows())
	if !strings.Contains(got, "> Query error:") || !strings.Contains(got, "not a query") {
		t.Fatalf("bad query should render an inline error with the source: %s", got)
	}

	if got := ExpandBlocks("no fences here", nil, testRows()); got != "no fences here" {
		t.Fatalf("body without fences must pass through: %q", got)
	}
}
