package timeline

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	IntervalSchemaVersion   = 1
	IntervalDuration        = 15 * time.Minute
	DefaultIntervalLimit    = 8
	MaxIntervalLimit        = 96 // One UTC day.
	IntervalReportSource    = "interval-report"
	IntervalEvidenceVersion = 2
	IntervalReportV2Source  = "interval-report-v2"
	IntervalProgressSource  = "interval-progress-v1"
)

// IntervalHistory is a read projection, separate from authored Report records.
// Its version describes this response, not the persisted journal.
type IntervalHistory struct {
	SchemaVersion   int              `json:"schema_version"`
	ProjectID       string           `json:"project_id"`
	Revision        uint64           `json:"revision"`
	IntervalMinutes int              `json:"interval_minutes"`
	ReportSource    string           `json:"report_source"`
	Intervals       []IntervalBucket `json:"intervals"` // Newest first; includes empty intervals.
}

type IntervalBucket struct {
	Start             time.Time            `json:"start"`
	End               time.Time            `json:"end"`
	Counts            IntervalCounts       `json:"counts"`
	Transitions       []IntervalTransition `json:"transitions"` // Journal sequence order.
	ReportID          string               `json:"report_id"`
	ReportStatus      string               `json:"report_status"` // missing, saved, conflict.
	Progress          []IntervalProgress   `json:"progress,omitempty"`
	EvidenceSequences []uint64             `json:"evidence_sequences,omitempty"`
	UncoveredProgress int                  `json:"uncovered_progress,omitempty"`
	LateTransitions   int                  `json:"late_transitions,omitempty"`
}

type IntervalCounts struct {
	Created   int `json:"created"`
	Activated int `json:"activated"`
	Completed int `json:"completed"`
	Reopened  int `json:"reopened"`
}

type IntervalTransition struct {
	Sequence   uint64    `json:"sequence"`
	At         time.Time `json:"at"`
	TaskID     string    `json:"task_id"`
	TaskTitle  string    `json:"task_title"`
	FromStatus string    `json:"from_status,omitempty"` // Absent on a task's first record.
	ToStatus   string    `json:"to_status"`
}

// IntervalProgress is explicit task-scoped evidence, not a status transition.
type IntervalProgress struct {
	Sequence uint64    `json:"sequence"`
	At       time.Time `json:"at"`
	TaskID   string    `json:"task_id"`
	Category string    `json:"category"`
	Text     string    `json:"text"`
}

func intervalProgress(event Event) (IntervalProgress, bool) {
	if event.Source != IntervalProgressSource || event.Kind != "discussion.add" {
		return IntervalProgress{}, false
	}
	var d Discussion
	if decode(event.Payload, &d) != nil || d.TargetType != "task" || d.ID != event.EventID ||
		!validText(d.Text, 512, true) || intervalTitle(d.Text) != d.Text {
		return IntervalProgress{}, false
	}
	for _, category := range []string{"result", "blocker", "next"} {
		if suffix, ok := strings.CutPrefix(d.ID, "progress-"+category+"-"); ok && validID(suffix) {
			at, err := time.Parse(time.RFC3339Nano, event.At)
			return IntervalProgress{Sequence: event.Sequence, At: at.UTC(), TaskID: d.TargetID, Category: category, Text: d.Text}, err == nil
		}
	}
	return IntervalProgress{}, false
}

// ReadIntervals projects fixed UTC windows from one validated journal read.
// Replay determines transitions in append order; only bucket membership uses
// wall-clock timestamps, which may move backwards.
func (s *Store) ReadIntervals(ctx context.Context, id string, until time.Time, limit int) (IntervalHistory, error) {
	return s.ReadIntervalsVersion(ctx, id, until, limit, IntervalSchemaVersion)
}

// ReadIntervalsVersion retains the v1 contract and opts into authored evidence in v2.
func (s *Store) ReadIntervalsVersion(ctx context.Context, id string, until time.Time, limit, version int) (IntervalHistory, error) {
	if version != IntervalSchemaVersion && version != IntervalEvidenceVersion {
		return IntervalHistory{}, invalid("evidence version must be 1 or 2")
	}
	reportSource := IntervalReportSource
	if version == IntervalEvidenceVersion {
		reportSource = IntervalReportV2Source
	}

	if limit < 1 || limit > MaxIntervalLimit {
		return IntervalHistory{}, invalid("interval limit must be between 1 and %d", MaxIntervalLimit)
	}
	until = until.UTC()
	if !until.Equal(until.Truncate(IntervalDuration)) {
		return IntervalHistory{}, invalid("until must be a UTC-aligned 15-minute boundary")
	}
	start := until.Add(-time.Duration(limit) * IntervalDuration)
	if start.Year() < 1 || until.Year() > 9999 {
		return IntervalHistory{}, invalid("interval range is outside supported RFC3339 years")
	}

	snapshot, err := s.Read(ctx, id)
	if err != nil {
		return IntervalHistory{}, err
	}
	out := IntervalHistory{
		SchemaVersion: version, ProjectID: snapshot.ProjectID, Revision: snapshot.Revision,
		IntervalMinutes: int(IntervalDuration / time.Minute), ReportSource: reportSource,
		Intervals: make([]IntervalBucket, limit),
	}
	reportIDs := make(map[string]bool, limit)
	for i := range out.Intervals {
		end := until.Add(-time.Duration(i) * IntervalDuration)
		reportID := "interval-" + end.Format("20060102T1504Z")
		out.Intervals[i] = IntervalBucket{
			Start: end.Add(-IntervalDuration), End: end, Transitions: []IntervalTransition{},
			ReportID: reportID, ReportStatus: "missing",
		}
		reportIDs[reportID] = true
	}
	receipts := make(map[string]Event, limit)
	receiptsV2 := make(map[string]Event, limit)
	lastProgress := make(map[[2]string]string)
	statuses := make(map[string]string, len(snapshot.Tasks))
	for _, event := range snapshot.Events {
		if err := ctx.Err(); err != nil {
			return IntervalHistory{}, err
		}
		if event.Source == IntervalReportSource && reportIDs[event.EventID] {
			receipts[event.EventID] = event
		}
		if version == IntervalEvidenceVersion {
			if event.Source == IntervalReportV2Source && reportIDs[event.EventID] {
				receiptsV2[event.EventID] = event
			}
			if progress, ok := intervalProgress(event); ok {
				key := [2]string{progress.TaskID, progress.Category}
				if lastProgress[key] != progress.Text {
					lastProgress[key] = progress.Text
					if !progress.At.Before(start) && progress.At.Before(until) {
						bucket := &out.Intervals[limit-1-int(progress.At.Sub(start)/IntervalDuration)]
						bucket.Progress = append(bucket.Progress, progress)
						bucket.EvidenceSequences = append(bucket.EvidenceSequences, event.Sequence)
					}
				}
			}
		}
		if event.Kind != "task.upsert" {
			continue
		}
		// Store.Read already validated the full payload. Expose only identity,
		// status and a printable one-line title, not detail, owner or raw payload.
		var state struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(event.Payload, &state); err != nil {
			return IntervalHistory{}, err
		}
		from, existed := statuses[state.ID]
		statuses[state.ID] = state.Status
		if existed && from == state.Status {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil {
			return IntervalHistory{}, err
		}
		if at.Before(start) || !at.Before(until) {
			continue
		}
		bucket := &out.Intervals[limit-1-int(at.Sub(start)/IntervalDuration)]
		bucket.Transitions = append(bucket.Transitions, IntervalTransition{
			Sequence: event.Sequence, At: at.UTC(), TaskID: state.ID, TaskTitle: intervalTitle(state.Title),
			FromStatus: from, ToStatus: state.Status,
		})
		if version == IntervalEvidenceVersion {
			bucket.EvidenceSequences = append(bucket.EvidenceSequences, event.Sequence)
		}
		if !existed {
			bucket.Counts.Created++
		}
		switch {
		case state.Status == "active":
			bucket.Counts.Activated++
		case state.Status == "done" && existed:
			bucket.Counts.Completed++
		}
		if from == "done" && state.Status != "done" && state.Status != "retired" {
			bucket.Counts.Reopened++
		}
	}
	occupied := make(map[string]bool, limit)
	for _, report := range snapshot.Reports {
		if reportIDs[report.ID] {
			occupied[report.ID] = true
		}
	}
	for i := range out.Intervals {
		bucket := &out.Intervals[i]
		receipt, ok := receipts[bucket.ReportID]
		nextReceipt, hasV2 := receiptsV2[bucket.ReportID]
		if !ok && !hasV2 && !occupied[bucket.ReportID] {
			continue
		}
		bucket.ReportStatus = "conflict"
		if late, valid := intervalReceipt(*bucket, receipt); ok && valid {
			bucket.ReportStatus = "saved"
			bucket.LateTransitions = late
			bucket.UncoveredProgress = len(bucket.Progress)
		} else if hasV2 && intervalReceiptV2(*bucket, nextReceipt) {
			bucket.ReportStatus = "saved"
			for _, transition := range bucket.Transitions {
				if transition.Sequence >= nextReceipt.Sequence {
					bucket.LateTransitions++
				}
			}
			for _, progress := range bucket.Progress {
				if progress.Sequence >= nextReceipt.Sequence {
					bucket.UncoveredProgress++
				}
			}
		}
	}
	return out, nil
}

func intervalTitle(title string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case !unicode.IsPrint(r):
			return -1
		default:
			return r
		}
	}, title)), " ")
}

// A normal authored report cannot accidentally satisfy the reserved journal
// identity. Its dates and complete pre-save transition provenance must also
// match. Later events can acquire older wall-clock timestamps; expose these
// as late, rather than silently treating them as covered by the original text.
func intervalReceipt(bucket IntervalBucket, event Event) (late int, valid bool) {
	report, ok := intervalReport(bucket, event)
	if !ok {
		return 0, false
	}
	covered := 0
	for _, transition := range bucket.Transitions {
		if transition.Sequence >= event.Sequence {
			break
		}
		if covered >= len(report.EventSequences) || report.EventSequences[covered] != transition.Sequence {
			return 0, false
		}
		covered++
	}
	if covered == 0 || covered != len(report.EventSequences) {
		return 0, false
	}
	return len(bucket.Transitions) - covered, true
}

// V2 uses the same report ID as v1 but attests to both evidence streams in
// journal sequence order. A saved v1 report is never rewritten to claim v2 evidence.
func intervalReceiptV2(bucket IntervalBucket, event Event) bool {
	report, ok := intervalReport(bucket, event)
	if !ok {
		return false
	}
	before := len(bucket.EvidenceSequences)
	for i, sequence := range bucket.EvidenceSequences {
		if sequence >= event.Sequence {
			before = i
			break
		}
	}
	return before > 0 && slices.Equal(bucket.EvidenceSequences[:before], report.EventSequences)
}

func intervalReport(bucket IntervalBucket, event Event) (Report, bool) {
	if event.Kind != "report.add" {
		return Report{}, false
	}
	var report Report
	if err := decode(event.Payload, &report); err != nil || report.ID != bucket.ReportID || report.TaskID != "" {
		return Report{}, false
	}
	start, startErr := time.Parse(time.RFC3339, report.PeriodStart)
	end, endErr := time.Parse(time.RFC3339, report.PeriodEnd)
	if startErr != nil || endErr != nil || !start.Equal(bucket.Start) || !end.Equal(bucket.End) {
		return Report{}, false
	}
	return report, true
}
