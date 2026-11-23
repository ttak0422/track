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
	events := []Event{
		intervalEvent(t, 1, "project", start.Add(-time.Hour), "project.create", Project{Title: "Plan"}),
		intervalEvent(t, 2, "alpha-active", start.Add(-time.Nanosecond), "task.upsert", task("alpha", "", "active")),
		intervalEvent(t, 3, "alpha-done", start, "task.upsert", task("alpha", "", "done")),
		intervalEvent(t, 4, "alpha-edit", start.Add(time.Minute), "task.upsert", edit),
		intervalEvent(t, 5, "alpha-reopen", start.Add(3*time.Minute), "task.upsert", task("alpha", "", "pending")),
		intervalEvent(t, 6, "alpha-resume", start.Add(3*time.Minute), "task.upsert", task("alpha", "", "active")),
		intervalEvent(t, 7, "alpha-done-again", mid.In(time.FixedZone("JST", 9*3600)), "task.upsert", task("alpha", "", "done")),
		intervalEvent(t, 8, "beta-initial-done", mid.Add(time.Minute), "task.upsert", task("beta", "", "done")),
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
		SchemaVersion: 1, ProjectID: "plan", Revision: 12, IntervalMinutes: 15,
		Intervals: []IntervalBucket{
			{Start: mid, End: end, Counts: IntervalCounts{Created: 1, Completed: 1}, Transitions: []IntervalTransition{
				{Sequence: 7, At: mid, TaskID: "alpha", FromStatus: "active", ToStatus: "done"},
				{Sequence: 8, At: mid.Add(time.Minute), TaskID: "beta", ToStatus: "done"},
				{Sequence: 11, At: end.Add(-time.Nanosecond), TaskID: "alpha", FromStatus: "done", ToStatus: "retired"},
			}},
			{Start: start, End: mid, Counts: IntervalCounts{Activated: 1, Completed: 1, Reopened: 1}, Transitions: []IntervalTransition{
				{Sequence: 3, At: start, TaskID: "alpha", FromStatus: "active", ToStatus: "done"},
				{Sequence: 5, At: start.Add(3 * time.Minute), TaskID: "alpha", FromStatus: "done", ToStatus: "pending"},
				{Sequence: 6, At: start.Add(3 * time.Minute), TaskID: "alpha", FromStatus: "pending", ToStatus: "active"},
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
