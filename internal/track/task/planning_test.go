package task

import (
	"testing"
)

// mustTask builds a Task from its field parts, mirroring what Parse produces from a task line.
func mustTask(done bool, priority, scheduled, due string) Task {
	return Task{
		State:     "TODO",
		Done:      done,
		Priority:  priority,
		Scheduled: scheduled,
		Due:       due,
	}
}

func TestOnPlanningAgenda(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want bool
	}{
		{"overdue", mustTask(false, "", "", "2026-07-09"), true},
		{"due today", mustTask(false, "", "", "2026-07-11"), true},
		{"due within horizon", mustTask(false, "", "", "2026-07-15"), true},
		{"due at horizon edge", mustTask(false, "", "", "2026-07-18"), true},
		{"due past horizon", mustTask(false, "", "", "2026-07-19"), false},
		{"scheduled today, no due", mustTask(false, "B", "2026-07-11", ""), true},
		{"scheduled today, future due", mustTask(false, "", "2026-07-11", "2026-07-20"), true},
		{"scheduled other day, no due", mustTask(false, "", "2026-07-12", ""), false},
		{"scheduled other day, due soon", mustTask(false, "", "2026-07-05", "2026-07-14"), true},
		{"done overdue excluded", mustTask(true, "", "", "2026-07-01"), false},
		{"done scheduled today excluded", mustTask(true, "", "2026-07-11", ""), false},
		{"undated excluded", mustTask(false, "A", "", ""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OnPlanningAgenda(tt.task, "2026-07-11", PlanningHorizonDays); got != tt.want {
				t.Errorf("OnPlanningAgenda(%+v) = %v, want %v", tt.task, got, tt.want)
			}
		})
	}
}

func TestSortByUrgency(t *testing.T) {
	// Source order is deliberately scrambled. The sort is date-first — the due date, or the scheduled
	// date when there is no deadline — then priority [#A] > [#B] > [#C] > none. The done task leads
	// purely because its deadline is the earliest: sorting is urgency, not selection.
	tasks := []Task{
		{Line: 1, State: "TODO", Priority: "C", Due: "2026-07-14"},
		{Line: 2, State: "TODO", Due: "2026-07-10"},
		{Line: 3, State: "TODO", Priority: "B", Scheduled: "2026-07-11"}, // no deadline: scheduled is its date
		{Line: 4, State: "TODO", Priority: "A", Due: "2026-07-11"},
		{Line: 5, State: "TODO", Priority: "A", Due: "2026-07-10"},
		{Line: 6, State: "TODO", Scheduled: "2026-07-12"}, // no deadline, no priority
		{Line: 7, State: "DONE", Done: true, Due: "2026-07-01"},
		{Line: 8, State: "TODO", Due: "2026-07-10"},
	}
	SortByUrgency(tasks)

	wantOrder := []Task{
		{Line: 7, Priority: "", Scheduled: "", Due: "2026-07-01"},  // earliest deadline
		{Line: 5, Priority: "A", Scheduled: "", Due: "2026-07-10"}, // overdue A beats overdue plain
		{Line: 2, Priority: "", Scheduled: "", Due: "2026-07-10"},  // ties on date and priority keep source order
		{Line: 8, Priority: "", Scheduled: "", Due: "2026-07-10"},
		{Line: 4, Priority: "A", Scheduled: "", Due: "2026-07-11"}, // due today, A
		{Line: 3, Priority: "B", Scheduled: "2026-07-11", Due: ""}, // scheduled today, no deadline
		{Line: 6, Priority: "", Scheduled: "2026-07-12", Due: ""},  // scheduled another day, no deadline
		{Line: 1, Priority: "C", Scheduled: "", Due: "2026-07-14"},
	}
	if len(tasks) != len(wantOrder) {
		t.Fatalf("got %d tasks, want %d", len(tasks), len(wantOrder))
	}
	for i, want := range wantOrder {
		got := tasks[i]
		if got.Priority != want.Priority || got.Scheduled != want.Scheduled || got.Due != want.Due || got.Line != want.Line {
			t.Errorf("position %d = line %d {%q sched %q due %q}, want line %d {%q sched %q due %q}",
				i, got.Line, got.Priority, got.Scheduled, got.Due, want.Line, want.Priority, want.Scheduled, want.Due)
		}
	}
}
