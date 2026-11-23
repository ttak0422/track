package timeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeIntervalJournal(t *testing.T, root string, events []Event) ([]byte, string) {
	t.Helper()
	raw, err := json.Marshal(document{SchemaVersion: SchemaVersion, ProjectID: "plan", Events: events})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".track", "timelines")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw, path
}

func intervalEvent(t *testing.T, seq uint64, id string, at time.Time, kind string, payload any) Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Event{Sequence: seq, At: at.Format(time.RFC3339Nano), Source: "fixture", EventID: id, Kind: kind, Payload: raw}
}

func TestReadIntervalsBoundariesAndTransitions(t *testing.T) {
	store, root := fixture(t)
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	mid := start.Add(IntervalDuration)
	end := mid.Add(IntervalDuration)
	edit := task("alpha", "", "done")
	edit.Title = "Changed title, not another completion"
	edit.Detail = "private-example-payload"
	beta := task("beta", "", "done")
	beta.Title = "Beta\n test\u001b\u202e"
	events := []Event{
		intervalEvent(t, 1, "project", start.Add(-time.Hour), "project.create", Project{Title: "Plan"}),
		intervalEvent(t, 2, "alpha-active", start.Add(-time.Nanosecond), "task.upsert", task("alpha", "", "active")),
		intervalEvent(t, 3, "alpha-done", start, "task.upsert", task("alpha", "", "done")),
		intervalEvent(t, 4, "alpha-edit", start.Add(time.Minute), "task.upsert", edit),
		intervalEvent(t, 5, "alpha-reopen", start.Add(3*time.Minute), "task.upsert", task("alpha", "", "pending")),
		intervalEvent(t, 6, "alpha-resume", start.Add(3*time.Minute), "task.upsert", task("alpha", "", "active")),
		intervalEvent(t, 7, "alpha-done-again", mid.In(time.FixedZone("JST", 9*3600)), "task.upsert", task("alpha", "", "done")),
		intervalEvent(t, 8, "beta-initial-done", mid.Add(time.Minute), "task.upsert", beta),
		intervalEvent(t, 9, "attached", mid.Add(2*time.Minute), "discussion.add", Discussion{
			ID: "attached", TargetType: "project", TargetID: "plan", Author: "fixture", Text: "private-example-payload",
		}),
		intervalEvent(t, 10, "authored", mid.Add(3*time.Minute), "report.add", Report{
			ID: "authored", Author: "fixture", PeriodStart: start.Format(time.RFC3339), PeriodEnd: mid.Format(time.RFC3339),
			Summary: "private-example-payload", EventSequences: []uint64{3, 7},
		}),
		intervalEvent(t, 11, "alpha-retired", end.Add(-time.Nanosecond), "task.upsert", task("alpha", "", "retired")),
		intervalEvent(t, 12, "beta-at-end", end, "task.upsert", task("beta", "", "active")),
	}
	before, path := writeIntervalJournal(t, root, events)
	ctx := context.Background()
	got, err := store.ReadIntervals(ctx, "plan", end, 2)
	if err != nil {
		t.Fatal(err)
	}
	expect := IntervalHistory{
		SchemaVersion: 1, ProjectID: "plan", Revision: 12, IntervalMinutes: 15, ReportSource: "interval-report",
		Intervals: []IntervalBucket{
			{Start: mid, End: end, ReportID: "interval-20261101T1230Z", ReportStatus: "missing", Counts: IntervalCounts{Created: 1, Completed: 1}, Transitions: []IntervalTransition{
				{Sequence: 7, At: mid, TaskID: "alpha", TaskTitle: "alpha", FromStatus: "active", ToStatus: "done"},
				{Sequence: 8, At: mid.Add(time.Minute), TaskID: "beta", TaskTitle: "Beta test", ToStatus: "done"},
				{Sequence: 11, At: end.Add(-time.Nanosecond), TaskID: "alpha", TaskTitle: "alpha", FromStatus: "done", ToStatus: "retired"},
			}},
			{Start: start, End: mid, ReportID: "interval-20261101T1215Z", ReportStatus: "missing", Counts: IntervalCounts{Activated: 1, Completed: 1, Reopened: 1}, Transitions: []IntervalTransition{
				{Sequence: 3, At: start, TaskID: "alpha", TaskTitle: "alpha", FromStatus: "active", ToStatus: "done"},
				{Sequence: 5, At: start.Add(3 * time.Minute), TaskID: "alpha", TaskTitle: "alpha", FromStatus: "done", ToStatus: "pending"},
				{Sequence: 6, At: start.Add(3 * time.Minute), TaskID: "alpha", TaskTitle: "alpha", FromStatus: "pending", ToStatus: "active"},
			}},
		},
	}
	if !reflect.DeepEqual(got, expect) {
		t.Fatalf("actual: %#v; expected: %#v", got, expect)
	}
	repeat, err := store.ReadIntervals(ctx, "plan", end.In(time.FixedZone("offset", 3600)), 2)
	if err != nil || !reflect.DeepEqual(repeat, got) {
		t.Fatalf("same instant changed projection: %#v, %v", repeat, err)
	}
	raw, err := json.Marshal(got)
	if err != nil || strings.Contains(string(raw), "private-example-payload") || strings.Contains(string(raw), "authored") {
		t.Fatalf("projection exposed authored text: %s (%v)", raw, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("read changed journal: %v", err)
	}

	empty, err := store.ReadIntervals(ctx, "plan", start.Add(-IntervalDuration), 1)
	if err != nil || len(empty.Intervals) != 1 || empty.Intervals[0].Transitions == nil ||
		len(empty.Intervals[0].Transitions) != 0 || empty.Intervals[0].Counts != (IntervalCounts{}) {
		t.Fatalf("expected a labeled empty bucket: %#v, %v", empty, err)
	}
}

func TestReadIntervalsClassifiesUsingSequenceWhenClockRegresses(t *testing.T) {
	store, root := fixture(t)
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	events := []Event{
		intervalEvent(t, 1, "project", start, "project.create", Project{Title: "Plan"}),
		intervalEvent(t, 2, "future-clock", start.Add(20*time.Minute), "task.upsert", task("alpha", "", "active")),
		intervalEvent(t, 3, "clock-regressed", start.Add(time.Minute), "task.upsert", task("alpha", "", "done")),
	}
	writeIntervalJournal(t, root, events)
	got, err := store.ReadIntervals(context.Background(), "plan", start.Add(IntervalDuration), 1)
	if err != nil {
		t.Fatal(err)
	}
	bucket := got.Intervals[0]
	if bucket.Counts != (IntervalCounts{Completed: 1}) || len(bucket.Transitions) != 1 || bucket.Transitions[0].FromStatus != "active" {
		t.Fatalf("timestamp reordered journal state: %#v", bucket)
	}
}

func TestReadIntervalsReplayAndValidation(t *testing.T) {
	store, _ := fixture(t)
	create(t, store)
	apply(t, store, req(1, "started", "task.upsert", task("alpha", "", "active")))
	done := req(2, "done", "task.upsert", task("alpha", "", "done"))
	apply(t, store, done)
	apply(t, store, done) // Exact operation retry: no second journal event.
	ctx := context.Background()
	until := time.Now().UTC().Truncate(IntervalDuration).Add(IntervalDuration)
	got, err := store.ReadIntervals(ctx, "plan", until, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 3 || got.Intervals[0].Counts.Completed+got.Intervals[1].Counts.Completed != 1 {
		t.Fatalf("retry duplicated completion: %#v", got)
	}
	for _, limit := range []int{-1, 0, MaxIntervalLimit + 1} {
		if _, err = store.ReadIntervals(ctx, "plan", until, limit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid limit %d: %v", limit, err)
		}
	}
	if _, err = store.ReadIntervals(ctx, "plan", until.Add(time.Nanosecond), 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("misaligned end: %v", err)
	}
	if _, err = store.ReadIntervals(ctx, "missing", until, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = store.ReadIntervals(cancelled, "plan", until, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
}

func TestReadIntervalsDistinguishesCanonicalReportFromAuthoredRecords(t *testing.T) {
	start := time.Date(2020, 5, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(IntervalDuration)
	reportID := "interval-20200501T1215Z"
	for _, tc := range []struct {
		name, source, eventID, id, want string
		seq                             []uint64
		wrongDate, taskSpecific         bool
		late                            bool
	}{
		{name: "ordinary report", source: "author", eventID: "report", id: "ordinary", seq: []uint64{2, 3}, want: "missing"},
		{name: "ordinary reserved id", source: "author", eventID: "report", id: reportID, seq: []uint64{2, 3}, want: "conflict"},
		{name: "wrong event id", source: IntervalReportSource, eventID: "different", id: reportID, seq: []uint64{2, 3}, want: "conflict"},
		{name: "different report id", source: IntervalReportSource, eventID: reportID, id: "ordinary", seq: []uint64{2, 3}, want: "conflict"},
		{name: "different period", source: IntervalReportSource, eventID: reportID, id: reportID, seq: []uint64{2, 3}, wrongDate: true, want: "conflict"},
		{name: "task report", source: IntervalReportSource, eventID: reportID, id: reportID, seq: []uint64{2, 3}, taskSpecific: true, want: "conflict"},
		{name: "incomplete provenance", source: IntervalReportSource, eventID: reportID, id: reportID, seq: []uint64{3}, want: "conflict"},
		{name: "nontransition provenance", source: IntervalReportSource, eventID: reportID, id: reportID, seq: []uint64{1, 2, 3}, want: "conflict"},
		{name: "canonical", source: IntervalReportSource, eventID: reportID, id: reportID, seq: []uint64{2, 3}, want: "saved"},
		{name: "late transition", source: IntervalReportSource, eventID: reportID, id: reportID, seq: []uint64{2, 3}, late: true, want: "saved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, root := fixture(t)
			periodEnd := end
			if tc.wrongDate {
				periodEnd = end.Add(time.Minute)
			}
			report := Report{
				ID: tc.id, Author: "coder", PeriodStart: start.Format(time.RFC3339), PeriodEnd: periodEnd.Format(time.RFC3339),
				Summary: "Implemented alpha.", EventSequences: tc.seq,
			}
			if tc.taskSpecific {
				report.TaskID = "alpha"
			}
			authored := intervalEvent(t, 4, tc.eventID, end.Add(time.Minute), "report.add", report)
			authored.Source = tc.source
			events := []Event{
				intervalEvent(t, 1, "project", start, "project.create", Project{Title: "Plan"}),
				intervalEvent(t, 2, "active", start.Add(time.Minute), "task.upsert", task("alpha", "", "active")),
				intervalEvent(t, 3, "done", start.Add(2*time.Minute), "task.upsert", task("alpha", "", "done")),
				authored,
			}
			late := 0
			if tc.late {
				events = append(events, intervalEvent(t, 5, "clock-regressed", start.Add(3*time.Minute), "task.upsert", task("alpha", "", "active")))
				late = 1
			}
			writeIntervalJournal(t, root, events)
			got, err := store.ReadIntervals(context.Background(), "plan", end, 1)
			if err != nil {
				t.Fatal(err)
			}
			bucket := got.Intervals[0]
			if bucket.ReportID != reportID || bucket.ReportStatus != tc.want || bucket.LateTransitions != late {
				t.Fatalf("unexpected report recognition: %#v", bucket)
			}
		})
	}
}

func TestIntervalAuthoredReportRoundTripThroughExistingApply(t *testing.T) {
	store, root := fixture(t)
	start := time.Date(2020, 5, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(IntervalDuration)
	writeIntervalJournal(t, root, []Event{
		intervalEvent(t, 1, "project", start, "project.create", Project{Title: "Plan"}),
		intervalEvent(t, 2, "active", start.Add(time.Minute), "task.upsert", task("alpha", "", "active")),
		intervalEvent(t, 3, "done", start.Add(2*time.Minute), "task.upsert", task("alpha", "", "done")),
	})
	ctx := context.Background()
	read, err := store.ReadIntervals(ctx, "plan", end, 1)
	if err != nil {
		t.Fatal(err)
	}
	bucket := read.Intervals[0]
	if bucket.ReportStatus != "missing" {
		t.Fatalf("prior report: %#v", bucket)
	}
	report := Report{
		ID: bucket.ReportID, Author: "codex", PeriodStart: bucket.Start.Format(time.RFC3339),
		PeriodEnd: bucket.End.Format(time.RFC3339), Summary: "Alpha moved from active to done.",
	}
	for _, transition := range bucket.Transitions {
		report.EventSequences = append(report.EventSequences, transition.Sequence)
	}
	request := req(read.Revision, bucket.ReportID, "report.add", report)
	request.Source = read.ReportSource
	apply(t, store, req(read.Revision, "project-edited", "project.update", Project{Title: "Renamed"}))
	if _, err = store.Apply(ctx, "plan", request); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale report write did not conflict: %v", err)
	}
	refreshed, err := store.ReadIntervals(ctx, "plan", end, 1)
	if err != nil || !reflect.DeepEqual(refreshed.Intervals, read.Intervals) {
		t.Fatalf("unexpected inputs after reread: %#v, %v", refreshed, err)
	}
	request.ExpectedRevision = refreshed.Revision
	saved := apply(t, store, request)
	again := apply(t, store, request)
	if len(saved.Reports) != 1 || len(again.Reports) != 1 || saved.Revision != again.Revision ||
		saved.Reports[0].Summary != report.Summary {
		t.Fatalf("authored report missing or duplicated: %#v", again.Reports)
	}
	report.Summary = "Another writer's draft."
	duplicate := req(saved.Revision, bucket.ReportID, "report.add", report)
	duplicate.Source = read.ReportSource
	if _, err = store.Apply(ctx, "plan", duplicate); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("different text replaced the interval report: %v", err)
	}
	result, err := store.ReadIntervals(ctx, "plan", end, 1)
	if err != nil || result.Intervals[0].ReportStatus != "saved" {
		t.Fatalf("saved report not recognized: %#v, %v", result, err)
	}
}
