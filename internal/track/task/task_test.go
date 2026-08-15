package task

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var testNow = time.Date(2026, 7, 11, 14, 30, 0, 0, time.UTC)

func TestParse(t *testing.T) {
	body := strings.Join([]string{
		"# Plan [1/3]",
		"",
		"- [ ] Write the report [#A] [due:2026-07-18]",
		"- [/] Draft slides [sched:2026-07-14]",
		"- [x] Ship the parser [done:2026-07-10]",
		"- [?] Hear back from Alex",
		"- [-] Rewrite everything",
		"- not a task",
		"- [unknown] marker",
		"1. [ ] ordered task",
		"  - [ ] nested task",
	}, "\n")

	// The plain list line and the multi-character "[unknown]" marker are not tasks.
	tasks := Parse(body)
	if len(tasks) != 7 {
		t.Fatalf("expected 7 tasks, got %d: %+v", len(tasks), tasks)
	}

	first := tasks[0]
	if first.Line != 3 || first.State != "TODO" || first.Done {
		t.Fatalf("unexpected first task: %+v", first)
	}
	if first.Priority != "A" || first.Due != "2026-07-18" {
		t.Fatalf("priority/due not parsed: %+v", first)
	}
	if first.Text != "Write the report" {
		t.Fatalf("tokens should be stripped from text, got %q", first.Text)
	}

	if tasks[1].State != "DOING" || tasks[1].Scheduled != "2026-07-14" {
		t.Fatalf("unexpected DOING task: %+v", tasks[1])
	}
	if tasks[2].State != "DONE" || !tasks[2].Done || tasks[2].Completed != "2026-07-10" {
		t.Fatalf("unexpected DONE task: %+v", tasks[2])
	}
	if tasks[3].State != "WAITING" || tasks[4].State != "CANCELLED" || !tasks[4].Done {
		t.Fatalf("unexpected waiting/cancelled tasks: %+v %+v", tasks[3], tasks[4])
	}
	if tasks[5].Line != 10 || tasks[6].Line != 11 {
		t.Fatalf("ordered/nested tasks not parsed: %+v %+v", tasks[5], tasks[6])
	}
	if tasks[6].Indent != 2 {
		t.Fatalf("nested indent not recorded: %+v", tasks[6])
	}
}

func TestParseUppercaseXMatchesDone(t *testing.T) {
	tasks := Parse("- [X] shouted done")
	if len(tasks) != 1 || tasks[0].State != "DONE" {
		t.Fatalf("expected [X] to match DONE, got %+v", tasks)
	}
}

func TestSetStateStampsAndClearsCompletion(t *testing.T) {
	body := "- [ ] Ship it [#B]\n"

	updated, tr, err := SetState(body, 1, "done", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated != "- [x] Ship it [#B] [done:2026-07-11]\n" {
		t.Fatalf("unexpected body: %q", updated)
	}
	if !tr.Changed || tr.From != "TODO" || tr.To != "DONE" || !tr.Done || tr.Completed != "2026-07-11" {
		t.Fatalf("unexpected transition: %+v", tr)
	}
	if tr.Text != "Ship it" {
		t.Fatalf("transition text should strip tokens, got %q", tr.Text)
	}

	// Leaving the done family removes the stamp.
	updated, tr, err = SetState(updated, 1, "DOING", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated != "- [/] Ship it [#B]\n" {
		t.Fatalf("stamp should be removed: %q", updated)
	}
	if tr.Completed != "" || tr.Done {
		t.Fatalf("unexpected transition: %+v", tr)
	}

	// Done to done (CANCELLED) keeps whatever stamp exists and does not double-stamp.
	updated, _, err = SetState("- [x] old [done:2026-01-01]\n", 1, "cancelled", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated != "- [-] old [done:2026-01-01]\n" {
		t.Fatalf("done-to-done should keep the stamp: %q", updated)
	}
}

func TestSetStateSameStateIsNoChange(t *testing.T) {
	updated, tr, err := SetState("- [ ] idle\n", 1, "todo", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Changed || updated != "- [ ] idle\n" {
		t.Fatalf("expected no change, got %+v %q", tr, updated)
	}
}

func TestSetStateErrors(t *testing.T) {
	if _, _, err := SetState("- [ ] a\n", 1, "nope", testNow); err == nil {
		t.Fatal("expected unknown-state error")
	}
	if _, _, err := SetState("plain text\n", 1, "done", testNow); err == nil {
		t.Fatal("expected non-task-line error")
	}
	if _, _, err := SetState("- [ ] a\n", 5, "done", testNow); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestSetStateRecomputesCookies(t *testing.T) {
	body := strings.Join([]string{
		"# Sprint [0/3]",
		"",
		"- [ ] Parent [0%]",
		"  - [ ] child one",
		"  - [x] child two",
		"- [ ] sibling",
		"",
		"# Next [9/9]",
		"",
		"- [ ] later",
	}, "\n") + "\n"

	updated, _, err := SetState(body, 4, "done", testNow)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(updated, "\n")
	// The heading counts every task below it up to the next heading of the same level (parent, both
	// children, sibling — two of which are now done); the parent's percent cookie counts only its
	// deeper-indented children.
	if lines[0] != "# Sprint [2/4]" {
		t.Fatalf("heading cookie not recomputed: %q", lines[0])
	}
	if lines[2] != "- [ ] Parent [100%]" {
		t.Fatalf("list cookie not recomputed: %q", lines[2])
	}
	if lines[7] != "# Next [0/1]" {
		t.Fatalf("second heading cookie not recomputed: %q", lines[7])
	}
}

// TestStateSet holds the invariants the set had to be validated for while it was configurable: every
// state carries a name and exactly one marker character, and no two share either. A duplicate marker
// would make one of them unreachable, since a line is read back by its character.
func TestStateSet(t *testing.T) {
	names := map[string]bool{}
	chars := map[string]bool{}
	for _, st := range States() {
		if st.Name == "" || utf8.RuneCountInString(st.Char) != 1 {
			t.Fatalf("state %+v needs a name and a single-character marker", st)
		}
		lower := strings.ToLower(st.Name)
		if names[lower] || chars[st.Char] {
			t.Fatalf("state %+v duplicates a name or marker", st)
		}
		names[lower], chars[st.Char] = true, true
	}

	// States hands out a copy: reordering the result must not reorder the set everyone else reads.
	got := States()
	got[0] = State{Name: "MANGLED", Char: "!"}
	if States()[0].Name != "TODO" {
		t.Fatal("States returned the package slice, not a copy")
	}
}

func TestFencedCodeIsNotATask(t *testing.T) {
	body := strings.Join([]string{
		"# Plan [0/1]",
		"",
		"```md",
		"- [ ] notation example",
		"# Heading [9/9]",
		"```",
		"- [ ] real task",
		"",
	}, "\n")

	tasks := Parse(body)
	if len(tasks) != 1 || tasks[0].Line != 7 {
		t.Fatalf("only the real task should parse: %+v", tasks)
	}
	if _, ok := At(body, 4); ok {
		t.Fatal("a fenced example line must not be a task")
	}
	if _, _, err := SetState(body, 4, "DONE", testNow); err == nil {
		t.Fatal("setting state on a fenced line should fail")
	}

	// The cookie counts only the real task, and the fenced cookie example is left alone.
	updated, _, err := SetState(body, 7, "DONE", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "# Plan [1/1]") || !strings.Contains(updated, "# Heading [9/9]") {
		t.Fatalf("cookie recompute should skip fenced lines: %q", updated)
	}
}

func TestFirstStates(t *testing.T) {
	todo, done := FirstStates()
	if todo.Name != "TODO" || done.Name != "DONE" {
		t.Fatalf("unexpected first states: %+v %+v", todo, done)
	}
}

func TestSetDate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		field DateField
		date  string
		want  string
	}{
		{"appends when absent", "- [ ] write it\n", "due", "2026-08-01", "- [ ] write it [due:2026-08-01]\n"},
		{
			"replaces in place, keeping the line's shape",
			"- [ ] write it [due:2026-01-01] [#A]\n", "due", "2026-08-01",
			"- [ ] write it [due:2026-08-01] [#A]\n",
		},
		{"clears with an empty date", "- [ ] write it [sched:2026-01-01]\n", "sched", "", "- [ ] write it\n"},
		{
			"lands before the completion stamp",
			"- [x] write it [done:2026-07-30]\n", "due", "2026-08-01",
			"- [x] write it [due:2026-08-01] [done:2026-07-30]\n",
		},
		{"clearing an absent token is a no-op", "- [ ] write it\n", "sched", "", "- [ ] write it\n"},
	} {
		got, task, err := SetDate(tc.body, 1, tc.field, tc.date)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: body = %q, want %q", tc.name, got, tc.want)
		}
		// The reparsed task is what the caller reports back, so it must agree with the line.
		if tc.field == "due" && task.Due != tc.date {
			t.Errorf("%s: due = %q, want %q", tc.name, task.Due, tc.date)
		}
		if tc.field == "sched" && task.Scheduled != tc.date {
			t.Errorf("%s: scheduled = %q, want %q", tc.name, task.Scheduled, tc.date)
		}
	}

	for _, tc := range []struct {
		name, body string
		field      DateField
		date       string
	}{
		{"a bad date", "- [ ] x\n", "due", "2026-13-45"},
		{"a non-date", "- [ ] x\n", "due", "tomorrow"},
		{"an unknown field", "- [ ] x\n", "start", "2026-08-01"},
		{"a non-task line", "just prose\n", "due", "2026-08-01"},
		{"a fenced line", "```\n- [ ] x\n```\n", "due", "2026-08-01"},
	} {
		line := 1
		if tc.name == "a fenced line" {
			line = 2
		}
		if _, _, err := SetDate(tc.body, line, tc.field, tc.date); err == nil {
			t.Errorf("%s should be refused", tc.name)
		}
	}
}

func TestAppend(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		opts     AppendOpts
		wantBody string
		wantLine int
	}{
		{"to an empty body", "", AppendOpts{Text: "beta"},
			"- [ ] beta\n", 1},
		{"to a body with trailing newline", "# T\n\n- [ ] alpha\n", AppendOpts{Text: "beta"},
			"# T\n\n- [ ] alpha\n- [ ] beta\n", 4},
		{"to a body without trailing newline", "# T\n- [ ] alpha", AppendOpts{Text: "beta"},
			"# T\n- [ ] alpha\n- [ ] beta\n", 3},
		{"tokens in documented order", "- [ ] alpha\n", AppendOpts{Text: "beta", Priority: "a", Scheduled: "2026-08-21", Due: "2026-08-25"},
			"- [ ] alpha\n- [ ] beta [#A] [sched:2026-08-21] [due:2026-08-25]\n", 2},
	} {
		got, task, err := Append(tc.body, tc.opts)
		if err != nil {
			t.Errorf("%s: Append error: %v", tc.name, err)
			continue
		}
		if got != tc.wantBody {
			t.Errorf("%s: body = %q, want %q", tc.name, got, tc.wantBody)
		}
		if task.Line != tc.wantLine {
			t.Errorf("%s: line = %d, want %d", tc.name, task.Line, tc.wantLine)
		}
	}

	for _, tc := range []struct {
		name string
		opts AppendOpts
	}{
		{"an empty text", AppendOpts{Text: "  "}},
		{"a multi-line text", AppendOpts{Text: "a\nb"}},
		{"a bad priority", AppendOpts{Text: "x", Priority: "AA"}},
		{"a bad scheduled date", AppendOpts{Text: "x", Scheduled: "2026-13-45"}},
		{"a bad due date", AppendOpts{Text: "x", Due: "tomorrow"}},
	} {
		if _, _, err := Append("", tc.opts); err == nil {
			t.Errorf("%s should be refused", tc.name)
		}
	}
}
