package timeline

import (
	"context"
	"encoding/json"
	"time"
)

const (
	IntervalSchemaVersion = 1
	IntervalDuration      = 15 * time.Minute
	DefaultIntervalLimit  = 8
	MaxIntervalLimit      = 96 // One UTC day.
)

// IntervalHistory is a read projection, separate from authored Report records.
// Its version describes this response, not the persisted journal.
type IntervalHistory struct {
	SchemaVersion   int              `json:"schema_version"`
	ProjectID       string           `json:"project_id"`
	Revision        uint64           `json:"revision"`
	IntervalMinutes int              `json:"interval_minutes"`
	Intervals       []IntervalBucket `json:"intervals"` // Newest first; includes empty intervals.
}

type IntervalBucket struct {
	Start       time.Time            `json:"start"`
	End         time.Time            `json:"end"`
	Counts      IntervalCounts       `json:"counts"`
	Transitions []IntervalTransition `json:"transitions"` // Journal sequence order.
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
	FromStatus string    `json:"from_status,omitempty"` // Absent on a task's first record.
	ToStatus   string    `json:"to_status"`
}

// ReadIntervals projects fixed UTC windows from one validated journal read.
// Replay determines transitions in append order; only bucket membership uses
// wall-clock timestamps, which may move backwards.
func (s *Store) ReadIntervals(ctx context.Context, id string, until time.Time, limit int) (IntervalHistory, error) {
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
		SchemaVersion: IntervalSchemaVersion, ProjectID: snapshot.ProjectID, Revision: snapshot.Revision,
		IntervalMinutes: int(IntervalDuration / time.Minute), Intervals: make([]IntervalBucket, limit),
	}
	for i := range out.Intervals {
		end := until.Add(-time.Duration(i) * IntervalDuration)
		out.Intervals[i] = IntervalBucket{Start: end.Add(-IntervalDuration), End: end, Transitions: []IntervalTransition{}}
	}
	statuses := make(map[string]string, len(snapshot.Tasks))
	for _, event := range snapshot.Events {
		if err := ctx.Err(); err != nil {
			return IntervalHistory{}, err
		}
		if event.Kind != "task.upsert" {
			continue
		}
		// Store.Read already validated the full payload. The projection emits
		// only identity and status, never the detail, title, owner or raw text.
		var state struct {
			ID     string `json:"id"`
			Status string `json:"status"`
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
			Sequence: event.Sequence, At: at.UTC(), TaskID: state.ID, FromStatus: from, ToStatus: state.Status,
		})
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
	return out, nil
}
