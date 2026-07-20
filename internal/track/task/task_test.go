package task

import (
	"strings"
	"testing"
	"time"
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
	tasks := Parse(body, nil)
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
	tasks := Parse("- [X] shouted done", nil)
	if len(tasks) != 1 || tasks[0].State != "DONE" {
		t.Fatalf("expected [X] to match DONE, got %+v", tasks)
	}
}

func TestSetStateStampsAndClearsCompletion(t *testing.T) {
	body := "- [ ] Ship it [#B]\n"

	updated, tr, err := SetState(body, 1, "done", nil, testNow)
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
	updated, tr, err = SetState(updated, 1, "DOING", nil, testNow)
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
	updated, _, err = SetState("- [x] old [done:2026-01-01]\n", 1, "cancelled", nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated != "- [-] old [done:2026-01-01]\n" {
		t.Fatalf("done-to-done should keep the stamp: %q", updated)
	}
}

func TestSetStateSameStateIsNoChange(t *testing.T) {
	updated, tr, err := SetState("- [ ] idle\n", 1, "todo", nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Changed || updated != "- [ ] idle\n" {
		t.Fatalf("expected no change, got %+v %q", tr, updated)
	}
}

func TestSetStateErrors(t *testing.T) {
	if _, _, err := SetState("- [ ] a\n", 1, "nope", nil, testNow); err == nil {
		t.Fatal("expected unknown-state error")
	}
	if _, _, err := SetState("plain text\n", 1, "done", nil, testNow); err == nil {
		t.Fatal("expected non-task-line error")
	}
	if _, _, err := SetState("- [ ] a\n", 5, "done", nil, testNow); err == nil {
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

	updated, _, err := SetState(body, 4, "done", nil, testNow)
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

func TestCustomStates(t *testing.T) {
	states := []State{
		{Name: "OPEN", Char: " "},
		{Name: "BLOCKED", Char: "b"},
		{Name: "CLOSED", Char: "c", Done: true},
	}
	updated, tr, err := SetState("- [b] stuck\n", 1, "closed", states, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated != "- [c] stuck [done:2026-07-11]\n" || tr.From != "BLOCKED" {
		t.Fatalf("unexpected custom-state result: %q %+v", updated, tr)
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

	tasks := Parse(body, nil)
	if len(tasks) != 1 || tasks[0].Line != 7 {
		t.Fatalf("only the real task should parse: %+v", tasks)
	}
	if _, ok := At(body, 4, nil); ok {
		t.Fatal("a fenced example line must not be a task")
	}
	if _, _, err := SetState(body, 4, "DONE", nil, testNow); err == nil {
		t.Fatal("setting state on a fenced line should fail")
	}

	// The cookie counts only the real task, and the fenced cookie example is left alone.
	updated, _, err := SetState(body, 7, "DONE", nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "# Plan [1/1]") || !strings.Contains(updated, "# Heading [9/9]") {
		t.Fatalf("cookie recompute should skip fenced lines: %q", updated)
	}
}

func TestValidateStates(t *testing.T) {
	if err := ValidateStates(DefaultStates()); err != nil {
		t.Fatalf("default states should validate: %v", err)
	}
	if err := ValidateStates([]State{{Name: "A", Char: "aa"}}); err == nil {
		t.Fatal("expected multi-character marker to fail")
	}
	if err := ValidateStates([]State{{Name: "A", Char: "a"}, {Name: "a", Char: "b"}}); err == nil {
		t.Fatal("expected duplicate name to fail")
	}
	if err := ValidateStates([]State{{Name: "A", Char: "a"}, {Name: "B", Char: "a"}}); err == nil {
		t.Fatal("expected duplicate marker to fail")
	}
}

func TestFirstStates(t *testing.T) {
	todo, done, err := FirstStates(nil)
	if err != nil || todo.Name != "TODO" || done.Name != "DONE" {
		t.Fatalf("unexpected first states: %+v %+v %v", todo, done, err)
	}
	if _, _, err := FirstStates([]State{{Name: "ONLY", Char: " "}}); err == nil {
		t.Fatal("expected error when no done state exists")
	}
}
