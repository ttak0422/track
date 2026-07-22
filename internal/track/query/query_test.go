package query

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestParseFullQuery(t *testing.T) {
	q, err := Parse(`TABLE title, props.status, props.due FROM #project WHERE props.status != "done" AND props.due < 2026-01-01 AND #work AND props.owner SORT props.due DESC LIMIT 10`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Query{
		Columns: []string{"title", "props.status", "props.due"},
		From:    "project",
		Where: []Cond{
			{Key: "props.status", Op: "!=", Value: "done"},
			{Key: "props.due", Op: "<", Value: "2026-01-01"},
			{Tag: "work"},
			{Key: "props.owner"},
		},
		Sort:  "props.due",
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
	q, err := Parse("TABLE title WHERE props.status!=done AND props.n>3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []Cond{{Key: "props.status", Op: "!=", Value: "done"}, {Key: "props.n", Op: ">", Value: "3"}}
	if !reflect.DeepEqual(q.Where, want) {
		t.Fatalf("where = %+v, want %+v", q.Where, want)
	}
}

// A comparison may put its value first ("2026-05-31 < props.reviewed" — the order a date range
// reads in); the parser flips it, so the evaluator only ever sees key-first conds.
func TestParseValueFirstComparison(t *testing.T) {
	q, err := Parse("TABLE title WHERE 2026-05-31 < props.due AND props.due < 2026-07-01 AND 3 = props.n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []Cond{
		{Key: "props.due", Op: ">", Value: "2026-05-31"},
		{Key: "props.due", Op: "<", Value: "2026-07-01"},
		{Key: "props.n", Op: "=", Value: "3"},
	}
	if !reflect.DeepEqual(q.Where, want) {
		t.Fatalf("where = %+v, want %+v", q.Where, want)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		"",
		"SELECT title",
		"TABLE",
		"TABLE title FROM project",        // missing #
		"TABLE title WHERE",               // missing condition
		"TABLE title WHERE props.due <",   // missing value
		"TABLE title LIMIT many",          // not a number
		"TABLE title SORT props.due up",   // trailing junk
		`TABLE title WHERE a = "b`,        // unterminated string
		"TABLE title WHERE props.a ! b",   // lone !
		"TABLE title WHERE props.a = AND", // keyword as value
		"TABLE status",                    // unknown bare key: props live under props.
		"TABLE title WHERE status = open", // unknown bare key in a condition
		"TABLE props.",                    // props. with no property name
		"TABLE title SORT mtime",          // mtime is not a note attribute
		"TABLE title WHERE 3 < 5",         // a comparison needs a key on one side
		"TABLE title WHERE 3 <",           // value-first with nothing after the op
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", bad)
		}
	}
}

// TestUnknownBareKeyErrorMessage locks the loud-error contract: a bare key that is not a note
// attribute names the offending key, lists the attributes, and points at the props. form.
func TestUnknownBareKeyErrorMessage(t *testing.T) {
	_, err := Parse("TABLE status")
	if err == nil {
		t.Fatal("unknown bare key should error, not silently return empty")
	}
	for _, want := range []string{`unknown key "status"`, "title, tags", "props.status"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

// TestPropsNamespaceNotShadowed is the core regression: a property literally named "title" is
// reachable ONLY as props.title and must NOT be shadowed by (nor shadow) the note's title attribute.
func TestPropsNamespaceNotShadowed(t *testing.T) {
	rows := []NoteRow{{ID: 1, Title: "Note Title", Props: []note.Prop{
		{Key: "title", Value: "Prop Title", Type: note.TypeString},
		{Key: "status", Value: "open", Type: note.TypeString},
	}}}
	mustRun := func(expr string) Result {
		q, err := Parse(expr)
		if err != nil {
			t.Fatalf("parse %q: %v", expr, err)
		}
		return Run(q, rows)
	}
	bare := mustRun("TABLE title").Rows[0].Cells[0]
	prop := mustRun("TABLE props.title").Rows[0].Cells[0]
	if bare != "Note Title" {
		t.Fatalf("bare title = %q, want the note attribute", bare)
	}
	if prop != "Prop Title" {
		t.Fatalf("props.title = %q, want the property", prop)
	}
	if bare == prop {
		t.Fatalf("title attribute and props.title must differ, both = %q", bare)
	}
	// props.<key> resolves in WHERE and SORT as well.
	if got := ids(mustRun("TABLE title WHERE props.status = open")); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("WHERE props.status = %v, want [1]", got)
	}
	if got := ids(mustRun("TABLE title SORT props.title")); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("SORT props.title = %v, want [1]", got)
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
	if got := ids(run(t, "TABLE title WHERE props.status = open")); !reflect.DeepEqual(got, []int64{1, 3}) {
		t.Fatalf("= : %v", got)
	}
	// != is none-of: a note without the key matches.
	if got := ids(run(t, "TABLE title WHERE props.status != done")); !reflect.DeepEqual(got, []int64{1, 3, 4}) {
		t.Fatalf("!= : %v", got)
	}
	if got := ids(run(t, "TABLE title WHERE props.due < 2026-02-01")); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("< date: %v", got)
	}
	// Value-first spells the same comparison: both orders select the same notes.
	if got := ids(run(t, "TABLE title WHERE 2026-02-01 > props.due")); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("value-first > date: %v", got)
	}
	// Numbers compare numerically, not lexically (9 < 10).
	if got := ids(run(t, "TABLE title WHERE props.points > 9")); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("> number: %v", got)
	}
	if got := ids(run(t, "TABLE title WHERE props.due")); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("presence: %v", got)
	}
	if got := ids(run(t, "TABLE title WHERE #urgent AND props.status != done")); !reflect.DeepEqual(got, []int64{4}) {
		t.Fatalf("tag AND comparison: %v", got)
	}
}

func TestRunSortAndLimit(t *testing.T) {
	if got := ids(run(t, "TABLE title FROM #project SORT props.due")); !reflect.DeepEqual(got, []int64{2, 1, 4}) {
		t.Fatalf("sort asc (missing last) = %v, want [2 1 4]", got)
	}
	if got := ids(run(t, "TABLE title FROM #project SORT props.due DESC")); !reflect.DeepEqual(got, []int64{1, 2, 4}) {
		t.Fatalf("sort desc (missing still last) = %v, want [1 2 4]", got)
	}
	if got := ids(run(t, "TABLE title FROM #project SORT props.due LIMIT 1")); !reflect.DeepEqual(got, []int64{2}) {
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
	// The props.status column renders its header as just "status" — the props. prefix scopes the
	// query, not the reader's table.
	md := Markdown(run(t, "TABLE title, props.status WHERE props.status = open"))
	want := strings.Join([]string{
		"| title | status |",
		"| --- | --- |",
		"| [[Alpha]] | open |",
		"| [[Gamma]] | open |",
	}, "\n")
	if md != want {
		t.Fatalf("markdown =\n%s\nwant\n%s", md, want)
	}
	if got := Markdown(run(t, "TABLE title WHERE props.status = nothing-matches")); got != "_No results._" {
		t.Fatalf("empty result = %q", got)
	}
}

func TestExpandBlocks(t *testing.T) {
	body := "before\n\n```track-query\nTABLE title WHERE #urgent\n```\n\nafter"
	got := ExpandBlocks(body, nil, testRows(), nil)
	want := "before\n\n| title |\n| --- |\n| [[Delta]] |\n\nafter"
	if got != want {
		t.Fatalf("expanded =\n%s\nwant\n%s", got, want)
	}
}

func TestExpandBlocksSavedAndErrors(t *testing.T) {
	saved := map[string]string{"urgent": "TABLE title WHERE #urgent"}
	got := ExpandBlocks("```track-query\nsaved: urgent\n```", saved, testRows(), nil)
	if !strings.Contains(got, "[[Delta]]") {
		t.Fatalf("saved query not expanded: %s", got)
	}

	got = ExpandBlocks("```track-query\nsaved: missing\n```", saved, testRows(), nil)
	if !strings.Contains(got, "> Query error:") || !strings.Contains(got, "saved: missing") {
		t.Fatalf("missing saved query should render an inline error with the source: %s", got)
	}

	got = ExpandBlocks("```track-query\nnot a query\n```", nil, testRows(), nil)
	if !strings.Contains(got, "> Query error:") || !strings.Contains(got, "not a query") {
		t.Fatalf("bad query should render an inline error with the source: %s", got)
	}

	if got := ExpandBlocks("no fences here", nil, testRows(), nil); got != "no fences here" {
		t.Fatalf("body without fences must pass through: %q", got)
	}
}
