// Package request models a live-mode agent request: one user instruction and the attempts that
// execute it. It is the connection-agnostic request foundation of the agent request gateway
// (docs/spec/live-agent-requests.md): a Request is a durable JSON file under
// <vault>/.track/requests/, a Dispatch is one attempt to execute the request, and the package owns
// the idempotent create/claim/result/fail/cancel/retry transitions with stale-dispatch and
// stale-result rejection.
//
// The package is deliberately free of connection types: agmsg team/sender/destination names, message
// ids, and read state stay inside connection implementations (docs/spec/live-agent-requests.md).
// Connections only ever hand this package normalized facts — a claim, a result, a failure — keyed by
// request id and dispatch id.
//
// Relationship to internal/track/dispatch: that package records note-level agent executions in a
// note's sidecar exec_log (the task = note model of docs/spec/agent-state-model.md). This package
// models request-level attempts and does not apply "task = note" to the request gateway; instead it
// carries over that spec's dual key (request id + dispatch id) and stale-completion rejection. The
// per-attempt type here is request.Dispatch, a distinct model from dispatch.Record — there is no
// second same-named top-level Dispatch model.
package request

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// CurrentVersion is the request JSON schema version this build reads and writes. Stored requests
// carry it; a file whose version is outside [1, CurrentVersion] is refused at load, so a future
// schema bump is detected instead of silently misread.
const CurrentVersion = 1

// Intent is the kind of work a request asks an agent to do.
type Intent string

const (
	// IntentExplain asks for an explanation of a selection or note. Nothing is written to the vault.
	IntentExplain Intent = "explain"
	// IntentResearch asks a question to be investigated. Nothing is written to the vault; the answer
	// may later be saved to a new note (stage 2).
	IntentResearch Intent = "research"
	// IntentUpdate asks the agent to produce a new body for one note. The server applies it (stage 3);
	// in stage 1 the proposed body is recorded with the result and no note is written.
	IntentUpdate Intent = "update"
)

// Intents returns the closed intent set in model order.
func Intents() []Intent {
	return []Intent{IntentExplain, IntentResearch, IntentUpdate}
}

// ValidIntent reports whether in is one of the closed intent values.
func ValidIntent(in Intent) bool {
	for _, i := range Intents() {
		if in == i {
			return true
		}
	}
	return false
}

// Status is the request-level state of a Request: the lifecycle the user sees. The set is closed and
// matches the state table of docs/spec/live-agent-requests.md.
type Status string

const (
	// StatusQueued means the request was accepted and no execution start is confirmed yet. Even after
	// a connection reports a successful send, the request stays queued until a claim confirms the run.
	StatusQueued Status = "queued"
	// StatusRunning means a claim confirmed that an execution started.
	StatusRunning Status = "running"
	// StatusApplying means an update's proposed body is saved and the apply is in progress. Reached
	// only by the update-apply path (stage 3); the stage-1 transitions do not enter it.
	StatusApplying Status = "applying"
	// StatusCompleted means the answer (or, for updates, the reflected body) is saved and confirmed.
	StatusCompleted Status = "completed"
	// StatusFailed means a confirmed delivery/execution/apply failure. The reason and a retry affordance
	// are shown to the user.
	StatusFailed Status = "failed"
	// StatusConflict means an update target changed or vanished and nothing was applied; the answer and
	// the proposed change are kept. Reached only by the update-apply path (stage 3).
	StatusConflict Status = "conflict"
	// StatusCancelled means the request was withdrawn; later results are not adopted.
	StatusCancelled Status = "cancelled"
)

// Statuses returns the closed request status set in model order.
func Statuses() []Status {
	return []Status{StatusQueued, StatusRunning, StatusApplying, StatusCompleted, StatusFailed, StatusConflict, StatusCancelled}
}

// ValidStatus reports whether s is one of the closed request status values.
func ValidStatus(s Status) bool {
	for _, st := range Statuses() {
		if s == st {
			return true
		}
	}
	return false
}

// DispatchStatus is the per-attempt state of one Dispatch. The attempt lifecycle is separate from the
// request lifecycle: a retry closes the old attempt and starts a new one without disturbing the
// request's history.
type DispatchStatus string

const (
	// AttemptQueued means the attempt exists but no execution start is confirmed.
	AttemptQueued DispatchStatus = "queued"
	// AttemptRunning means the attempt was claimed and is being executed.
	AttemptRunning DispatchStatus = "running"
	// AttemptCompleted means the attempt's result was adopted.
	AttemptCompleted DispatchStatus = "completed"
	// AttemptFailed means the agent reported a failure for this attempt.
	AttemptFailed DispatchStatus = "failed"
	// AttemptCancelled means the attempt was closed without being adopted: the request was cancelled,
	// or a retry superseded the attempt while it was still running.
	AttemptCancelled DispatchStatus = "cancelled"
)

// DispatchStatuses returns the closed attempt status set in model order.
func DispatchStatuses() []DispatchStatus {
	return []DispatchStatus{AttemptQueued, AttemptRunning, AttemptCompleted, AttemptFailed, AttemptCancelled}
}

// ValidDispatchStatus reports whether s is one of the closed attempt status values.
func ValidDispatchStatus(s DispatchStatus) bool {
	for _, st := range DispatchStatuses() {
		if s == st {
			return true
		}
	}
	return false
}

// DeliveryStatus is a connection's reported outcome of handing an attempt to an agent, normalized to
// the connection-agnostic set of docs/spec/live-agent-requests.md: a successful send is never read as
// an execution start or a completion. It is written by the connection layer, never by this package,
// so a stage-1 attempt carries an empty value.
type DeliveryStatus string

const (
	// DeliverySent means the connection delivered the attempt to the agent.
	DeliverySent DeliveryStatus = "sent"
	// DeliveryFailed means the connection could not deliver the attempt.
	DeliveryFailed DeliveryStatus = "failed"
	// DeliveryUnknown means the delivery outcome is unknown and may be re-attempted with the same
	// dispatch id.
	DeliveryUnknown DeliveryStatus = "unknown"
)

// NoteRef identifies one note in a vault, with the content the client saw. Body and ETag let a
// later stage re-read the note server-side and detect changes; they are not trusted as authoritative
// content.
type NoteRef struct {
	Vault    string `json:"vault,omitempty"` // registry label, "" for the unregistered active vault
	NoteID   int64  `json:"note_id"`         // vault-local note id
	Title    string `json:"title,omitempty"` // display title at request time
	Body     string `json:"body,omitempty"`  // content the client saw (authoritative read is the server's)
	ETag     string `json:"etag,omitempty"`  // content hash of Body as the client read it
	FileKind string `json:"file_kind,omitempty"`
}

// Context is the material a request carries alongside its instruction: a client-selected quote, the
// note the user pointed at, and any referenced notes. References are context, never instructions —
// sentences in a note are not execution directions.
type Context struct {
	Quote      string    `json:"quote,omitempty"` // client-selected quotation (e.g. from live-mode dictation)
	Note       *NoteRef  `json:"note,omitempty"`
	References []NoteRef `json:"references,omitempty"`
}

// Result is the settled outcome of a request: the answer text (explain/research), or the proposed
// body and change rationale (update). Fingerprint is a server-computed hash of the submitted content,
// used to make result re-submission idempotent and to refuse a different result over a completed one.
type Result struct {
	AnswerMarkdown string   `json:"answer_markdown,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	ProposedBody   string   `json:"proposed_body,omitempty"` // update intents: the new body, applied in stage 3
	Fingerprint    string   `json:"fingerprint,omitempty"`
}

// Dispatch is one attempt to execute a Request. Every attempt keeps its own id, status, and
// timestamps; retries append new attempts and past results are preserved. It is the per-attempt half
// of the request+dispatch dual key this package carries over from docs/spec/agent-state-model.md, and
// it is a distinct model from dispatch.Record (the note-level execution log) — see the package doc.
type Dispatch struct {
	ID                string         `json:"id"`
	Status            DispatchStatus `json:"status"`
	Delivery          DeliveryStatus `json:"delivery,omitempty"` // set by the connection layer
	CreatedAt         string         `json:"created_at"`
	ClaimedAt         string         `json:"claimed_at,omitempty"`
	SettledAt         string         `json:"settled_at,omitempty"`
	FailureReason     string         `json:"failure_reason,omitempty"`
	ResultFingerprint string         `json:"result_fingerprint,omitempty"`
}

// Request is one user instruction and its attempt history. It is the authoritative record — the whole
// file is stored under <vault>/.track/requests/<id>.json and replaced atomically on every transition;
// it is never rebuilt from the search index or the browser.
type Request struct {
	Version          int        `json:"version"`
	ID               string     `json:"id"`
	ClientRequestID  string     `json:"client_request_id,omitempty"`
	ParentRequestID  string     `json:"parent_request_id,omitempty"`
	Vault            string     `json:"vault"`      // registry label of the vault this request belongs to
	VaultPath        string     `json:"vault_path"` // canonical (symlink-resolved) vault directory
	Intent           Intent     `json:"intent"`
	Instruction      string     `json:"instruction"`
	AgentID          string     `json:"agent_id"`
	Context          Context    `json:"context,omitempty"`
	UpdateTarget     *NoteRef   `json:"update_target,omitempty"` // required for update intents
	InputFingerprint string     `json:"input_fingerprint"`
	Status           Status     `json:"status"`
	Error            string     `json:"error,omitempty"` // request-level failure reason
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
	Attempts         []Dispatch `json:"attempts"`
	Result           *Result    `json:"result,omitempty"`
}

// Size limits. Inputs, context, and results are bounded and rejected outright — never silently
// truncated — as the request spec requires.
const (
	MaxInstructionBytes   = 16 << 10
	MaxQuoteBytes         = 16 << 10
	MaxNoteBodyBytes      = 512 << 10
	MaxReferencesBytes    = 1 << 20
	MaxAnswerBytes        = 512 << 10
	MaxProposedBodyBytes  = 512 << 10
	MaxFailureReasonBytes = 8 << 10
	MaxSources            = 64
)

// NewRequest is the create input, in a separate shape from the stored Request so a caller can never
// set stored-only fields (id, status, timestamps, attempts, result).
type NewRequest struct {
	ClientRequestID string
	ParentRequestID string
	Intent          Intent
	Instruction     string
	AgentID         string
	Context         Context
	UpdateTarget    *NoteRef
}

// RejectReason names why an operation was refused. The set is the stale-completion rejection set of
// docs/spec/agent-state-model.md (unknown task/dispatch, inactive, stale) adapted to the request
// model, plus the request-specific conflict reasons.
type RejectReason string

const (
	// RejectUnknownRequest: no request file with that id.
	RejectUnknownRequest RejectReason = "unknown_request"
	// RejectUnknownDispatch: no attempt with that dispatch id.
	RejectUnknownDispatch RejectReason = "unknown_dispatch"
	// RejectStaleDispatch: the dispatch is not the request's current attempt (a superseded or old
	// attempt's report).
	RejectStaleDispatch RejectReason = "stale_dispatch"
	// RejectInactiveDispatch: the request or the current attempt is in a state the transition cannot
	// run from (already settled, withdrawn, or moved while the report was in flight).
	RejectInactiveDispatch RejectReason = "inactive_dispatch"
	// RejectInputConflict: the same client_request_id was used with different input.
	RejectInputConflict RejectReason = "input_conflict"
	// RejectResultConflict: a different result was submitted over an already-completed request.
	RejectResultConflict RejectReason = "result_conflict"
	// RejectInvalidRequest: the create or transition input violates the model (bad intent, missing
	// required field, unknown state).
	RejectInvalidRequest RejectReason = "invalid_request"
	// RejectOversize: input, context, or result exceeds its size limit.
	RejectOversize RejectReason = "oversize"
)

// Error is a refused transition or create. The Reason lets the web layer map to a status code without
// string matching.
type Error struct {
	Reason RejectReason
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// reject builds an *Error with reason and a formatted message.
func reject(reason RejectReason, format string, args ...any) error {
	return &Error{Reason: reason, Msg: fmt.Sprintf(format, args...)}
}

// fingerprint hashes parts into a compact content fingerprint. It is used for both input identity
// (idempotent create) and result identity (idempotent result / stale-result rejection).
func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = io.WriteString(h, p)
		_, _ = io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// inputFingerprint hashes everything that defines a request's input: intent, instruction, agent, and
// the whole context (quote, target note, references, update target). Two creates with the same
// client_request_id compare by it.
func inputFingerprint(in *NewRequest) string {
	var refs strings.Builder
	for _, ref := range in.Context.References {
		refs.WriteString(noteRefPart(&ref))
	}
	var notePart, updatePart string
	if n := in.Context.Note; n != nil {
		notePart = noteRefPart(n)
	}
	if up := in.UpdateTarget; up != nil {
		updatePart = noteRefPart(up)
	}
	return fingerprint(
		string(in.Intent), in.Instruction, in.AgentID, in.ParentRequestID, in.Context.Quote,
		notePart, updatePart, refs.String(),
	)
}

func noteRefPart(ref *NoteRef) string {
	var b strings.Builder
	b.WriteString(ref.Vault)
	b.WriteByte(0)
	b.WriteString(itoa(ref.NoteID))
	b.WriteByte(0)
	b.WriteString(ref.Title)
	b.WriteByte(0)
	b.WriteString(ref.Body)
	b.WriteByte(0)
	b.WriteString(ref.ETag)
	b.WriteByte(0)
	b.WriteString(ref.FileKind)
	b.WriteByte(0)
	return b.String()
}

// resultFingerprint hashes the submitted result content. The fingerprint is recomputed server-side on
// every submission — clients never supply it.
func resultFingerprint(res *Result) string {
	return fingerprint(res.AnswerMarkdown, strings.Join(res.Sources, "\x00"), res.ProposedBody)
}

// genID returns a time-ordered, collision-resistant id: unix milliseconds plus a random suffix. The
// millisecond prefix keeps ids sortable newest-first; the suffix makes simultaneous creates distinct.
func genID(prefix string, now time.Time) (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return fmt.Sprintf("%s-%d-%s", prefix, now.UnixMilli(), hex.EncodeToString(b[:])), nil
}

func itoa(id int64) string {
	return fmt.Sprintf("%d", id)
}
