// Package dispatch models a single agent execution attempt over a note: one concrete attempt by
// one agent to work on one note, as recorded in the note's sidecar execution log (exec_log).
//
// It is the model half of the task+dispatch dual key (docs/spec/agent-state-model.md): a task is a
// note, and a dispatch is the record of which attempt, with what status. A note can be retried, so
// a note can have several dispatches; each is a distinct record with its own id and status.
package dispatch

// Status is one state of a dispatch record.
type Status string

// The dispatch status set is closed and fixed. It is the note-level analog of the source model's
// dispatch status, reduced by dropping the federation/remote states: circuit_broken is out of
// scope for a single local vault. The set matches the values the spec fixes
// (`pending | dispatched | completed | failed`).
const (
	// StatusPending means the dispatch was created but not yet handed to a worker.
	StatusPending Status = "pending"
	// StatusDispatched means the dispatch was handed to a worker and is live.
	StatusDispatched Status = "dispatched"
	// StatusCompleted means a completion report settled the dispatch.
	StatusCompleted Status = "completed"
	// StatusFailed means a failure report settled the dispatch.
	StatusFailed Status = "failed"
)

// Statuses returns the closed status set in model order.
func Statuses() []Status {
	return []Status{StatusPending, StatusDispatched, StatusCompleted, StatusFailed}
}

// ValidStatus reports whether s is one of the closed dispatch status values.
func ValidStatus(s Status) bool {
	for _, st := range Statuses() {
		if s == st {
			return true
		}
	}
	return false
}

// Record is one dispatch record as stored in a note's sidecar exec_log. It carries both the note
// id and the stable dispatch id — the pair the model's dual-key invariant keys completion and
// heartbeat reports on, so a retried note's records stay distinguishable. The id is a distinct
// namespace from note ids, so the generator (the settlement phase) is free to choose its format.
//
// The log is append-only, following the task_log convention: every state change appends a new
// record carrying the full dispatch state, and an existing record is never mutated. The latest
// record for a dispatch id is that dispatch's current state. All timestamps are RFC 3339, so a
// machine reading the log compares them unambiguously across machines.
type Record struct {
	At              string `yaml:"at" json:"at"` // when this transition was recorded
	ID              string `yaml:"id" json:"id"` // the stable dispatch id (not the note id)
	Note            int64  `yaml:"note" json:"note"`
	Status          Status `yaml:"status" json:"status"`
	FailureCount    int    `yaml:"failure_count,omitempty" json:"failure_count,omitempty"`
	DispatchedAt    string `yaml:"dispatched_at,omitempty" json:"dispatched_at,omitempty"`
	CompletedAt     string `yaml:"completed_at,omitempty" json:"completed_at,omitempty"`
	LastHeartbeatAt string `yaml:"last_heartbeat_at,omitempty" json:"last_heartbeat_at,omitempty"`
}
