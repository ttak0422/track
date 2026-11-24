package timeline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestIntervalProgressProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, source, wantV1, wantV2 string
		provenance                   []uint64
		uncovered, late              int
	}{
		{"v1", IntervalReportSource, "saved", "saved", []uint64{8}, 3, 1},
		{"v2", IntervalReportV2Source, "conflict", "saved", []uint64{5, 8, 9}, 1, 1},
		{"v2 out of order", IntervalReportV2Source, "conflict", "conflict", []uint64{8, 5, 9}, 0, 0},
		{"v2 omits active evidence", IntervalReportV2Source, "conflict", "conflict", []uint64{8, 9}, 0, 0},
		{"v2 includes ordinary discussion", IntervalReportV2Source, "conflict", "conflict", []uint64{4, 5, 8, 9}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, root := fixture(t)
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			end := start.Add(IntervalDuration)
			at := start.Add(time.Minute)
			note := func(seq uint64, id, text string, when time.Time, source, eventID string) Event {
				e := intervalEvent(t, seq, eventID, when, "discussion.add", Discussion{ID: id, TargetType: "task", TargetID: "alpha", Author: "agent", Text: text})
				e.Source = source
				return e
			}
			edit := task("alpha", "", "active")
			edit.Title = "Title only"
			edit.Detail = "private detail"
			e := []Event{
				intervalEvent(t, 1, "project", start.Add(-time.Hour), "project.create", Project{Title: "Plan"}),
				intervalEvent(t, 2, "active", start.Add(-time.Nanosecond), "task.upsert", task("alpha", "", "active")),
				intervalEvent(t, 3, "rename", at, "task.upsert", edit),
				note(4, "progress-result-ordinary", "private comment", at, "fixture", "progress-result-ordinary"),
				note(5, "progress-result-test", "Passed the fixture", at, IntervalProgressSource, "progress-result-test"),
				note(6, "progress-result-replay", "Passed the fixture", at, IntervalProgressSource, "progress-result-replay"),
				note(7, "progress-result-lookalike", "private lookalike", at, IntervalProgressSource, "different"),
				intervalEvent(t, 8, "blocked", at, "task.upsert", task("alpha", "", "blocked")),
				note(9, "progress-blocker-lib", "Waiting for native library", at.Add(-time.Nanosecond), IntervalProgressSource, "progress-blocker-lib"),
			}
			reportID := "interval-" + end.Format("20060102T1504Z")
			receipt := intervalEvent(t, 10, reportID, end, "report.add", Report{ID: reportID, Author: "agent", Summary: "Explicit summary", PeriodStart: start.Format(time.RFC3339), PeriodEnd: end.Format(time.RFC3339), EventSequences: tc.provenance})
			receipt.Source = tc.source
			e = append(e, receipt, note(11, "progress-next-step", "Re-run the native check", at, IntervalProgressSource, "progress-next-step"), intervalEvent(t, 12, "resumed", at, "task.upsert", task("alpha", "", "active")))
			before, path := writeIntervalJournal(t, root, e)
			ctx := context.Background()
			v1, err := store.ReadIntervals(ctx, "plan", end, 2)
			if err != nil || v1.Intervals[0].ReportStatus != tc.wantV1 || len(v1.Intervals[0].Progress) != 0 {
				t.Fatalf("legacy: %+v (%v)", v1, err)
			}
			v2, err := store.ReadIntervalsVersion(ctx, "plan", end, 2, IntervalEvidenceVersion)
			if err != nil {
				t.Fatal(err)
			}
			got := v2.Intervals[0]
			if got.ReportStatus != tc.wantV2 || got.UncoveredProgress != tc.uncovered || got.LateTransitions != tc.late ||
				!reflect.DeepEqual(got.EvidenceSequences, []uint64{5, 8, 9, 11, 12}) || len(got.Progress) != 3 ||
				got.Progress[0].Category != "result" || got.Progress[0].Text != "Passed the fixture" ||
				got.Progress[1].Category != "blocker" || !got.Progress[1].At.Before(got.Progress[0].At) ||
				got.Progress[2].Category != "next" || len(v2.Intervals[1].Progress) != 0 {
				t.Fatalf("evidence: %+v", v2)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("read mutated journal", err)
			}
		})
	}
}

func TestApplyIntervalProgressIdentityAndValidation(t *testing.T) {
	store, _ := fixture(t)
	create(t, store)
	apply(t, store, req(1, "active", "task.upsert", task("alpha", "", "active")))
	progress := func(rev uint64, id, text string) Request {
		r := req(rev, id, "discussion.add", Discussion{ID: id, TargetType: "task", TargetID: "alpha", Author: "agent", Text: text})
		r.Source = IntervalProgressSource
		return r
	}
	valid := progress(2, "progress-result-1", "Passed fixture")
	apply(t, store, valid)
	if snap := apply(t, store, valid); snap.Revision != 3 {
		t.Fatalf("retry appended: %d", snap.Revision)
	}
	for _, tc := range []struct {
		request Request
		want    error
	}{
		{progress(3, "progress-result-1", "Different text"), ErrDuplicate},
		{progress(2, "progress-next-1", "Re-run the fixture"), ErrConflict},
		{progress(3, "progress-next-1", ""), ErrInvalid},
		{progress(3, "progress-next-1", "inject\nnew line"), ErrInvalid},
		{progress(3, "progress-next-1", "inject\u202e"), ErrInvalid},
		{progress(3, "ordinary", "Not explicit"), ErrInvalid},
	} {
		if _, err := store.Apply(context.Background(), "plan", tc.request); !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc.request, err)
		}
	}
}
