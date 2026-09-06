package task

import (
	"cmp"
	"slices"
	"time"
)

// PlanningHorizonDays is the default number of days past a planning day inside which a deadline counts
// as "due soon". It is a fixed prototype default, not a per-vault setting; the CLI passes it so a
// future --horizon flag can override it without touching this package's decision rules.
const PlanningHorizonDays = 7

// OnPlanningAgenda reports whether a task belongs on a planning agenda for day: it is not done, it
// carries a scheduled or due date, and it is either overdue (due before day), scheduled on day itself,
// or due within horizonDays after day. Dates are the fixed YYYY-MM-DD tokens of the task line, so they
// compare lexically like the store's date filters and a malformed token simply lands outside every
// window instead of erroring.
func OnPlanningAgenda(t Task, day string, horizonDays int) bool {
	if t.Done || (t.Scheduled == "" && t.Due == "") {
		return false
	}
	if t.Due != "" && t.Due < day {
		return true // overdue: the deadline already passed
	}
	if t.Scheduled == day {
		return true // scheduled for the planning day itself
	}
	return t.Due != "" && t.Due >= day && t.Due <= addDays(day, horizonDays)
}

// CompareUrgency orders two tasks for a planning agenda: the nearer date comes first — the due date,
// or the scheduled date when there is no deadline — so overdue work leads the list, then priority
// [#A] > [#B] > [#C] > none, then source order (Line) for a stable total order.
func CompareUrgency(a, b Task) int {
	if c := cmp.Compare(urgencyDate(a), urgencyDate(b)); c != 0 {
		return c
	}
	if c := cmp.Compare(priorityRank(a), priorityRank(b)); c != 0 {
		return c
	}
	return cmp.Compare(a.Line, b.Line)
}

// SortByUrgency orders tasks for a planning agenda in place (see CompareUrgency). It is the one-shot
// helper for the common "filter then sort" shape; callers that interleave their own filtering keep
// CompareUrgency for the comparator.
func SortByUrgency(tasks []Task) {
	slices.SortStableFunc(tasks, CompareUrgency)
}

// urgencyDate is the task's position on a planning agenda: its deadline, or its scheduled date when it
// has no deadline. A task scheduled today with a future deadline sorts by that deadline, not by today.
func urgencyDate(t Task) string {
	if t.Due != "" {
		return t.Due
	}
	return t.Scheduled
}

// priorityRank maps a [#X] priority to an ascending rank: A first, then B, C, and unprioritized last.
func priorityRank(t Task) int {
	switch t.Priority {
	case "A":
		return 0
	case "B":
		return 1
	case "C":
		return 2
	default:
		return 3
	}
}

// addDays returns the date horizonDays days after day, or day unchanged when day is not a YYYY-MM-DD
// date. The unchanged fallback keeps the "due soon" window empty for a malformed day rather than
// failing, matching the lexical date handling elsewhere in the package.
func addDays(day string, horizonDays int) string {
	d, err := time.Parse(dateLayout, day)
	if err != nil {
		return day
	}
	return d.AddDate(0, 0, horizonDays).Format(dateLayout)
}
