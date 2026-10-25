// Package request reads the preserved JSON history of the retired agent request gateway.
// Version 1 records retain their format. Reads never execute or recover requests.
package request

// CurrentVersion is the archived JSON schema this reader supports.
const CurrentVersion = 1

// Intent records the original operation.
type Intent string

const (
	IntentExplain  Intent = "explain"
	IntentResearch Intent = "research"
	IntentUpdate   Intent = "update"
)

// Status is the archived lifecycle state; applying remains untouched on read.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusApplying  Status = "applying"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusConflict  Status = "conflict"
	StatusCancelled Status = "cancelled"
)

func Statuses() []Status {
	return []Status{StatusQueued, StatusRunning, StatusApplying, StatusCompleted, StatusFailed, StatusConflict, StatusCancelled}
}

func ValidStatus(s Status) bool {
	for _, st := range Statuses() {
		if s == st {
			return true
		}
	}
	return false
}

// DispatchStatus is one historical attempt's state.
type DispatchStatus string

const (
	AttemptQueued    DispatchStatus = "queued"
	AttemptRunning   DispatchStatus = "running"
	AttemptCompleted DispatchStatus = "completed"
	AttemptFailed    DispatchStatus = "failed"
	AttemptCancelled DispatchStatus = "cancelled"
)

func DispatchStatuses() []DispatchStatus {
	return []DispatchStatus{AttemptQueued, AttemptRunning, AttemptCompleted, AttemptFailed, AttemptCancelled}
}

func ValidDispatchStatus(s DispatchStatus) bool {
	for _, st := range DispatchStatuses() {
		if s == st {
			return true
		}
	}
	return false
}

// DeliveryStatus records the delivery outcome independently of execution.
type DeliveryStatus string

const (
	DeliverySent    DeliveryStatus = "sent"
	DeliveryFailed  DeliveryStatus = "failed"
	DeliveryUnknown DeliveryStatus = "unknown"
)

// NoteRef preserves the note identity and send-time content.
type NoteRef struct {
	Vault    string `json:"vault,omitempty"` // registry label, "" for the unregistered active vault
	NoteID   int64  `json:"note_id"`         // vault-local note id
	Title    string `json:"title,omitempty"` // display title at request time
	Body     string `json:"body,omitempty"`  // content the client saw (authoritative read is the server's)
	ETag     string `json:"etag,omitempty"`  // content hash of Body as the client read it
	FileKind string `json:"file_kind,omitempty"`
}

// Context preserves selected text, references, and prior answers.
type Context struct {
	Quote        string        `json:"quote,omitempty"` // client-selected quotation (e.g. from live-mode dictation)
	Note         *NoteRef      `json:"note,omitempty"`
	References   []NoteRef     `json:"references,omitempty"`
	PriorAnswers []PriorAnswer `json:"prior_answers,omitempty"`
}

// Result preserves the answer, proposal, saved-note link, and applied change.
type Result struct {
	AnswerMarkdown string         `json:"answer_markdown,omitempty"`
	Sources        []string       `json:"sources,omitempty"`
	ProposedBody   string         `json:"proposed_body,omitempty"` // update intents: the proposed new body
	Fingerprint    string         `json:"fingerprint,omitempty"`
	Saved          *SavedNote     `json:"saved,omitempty"`
	Apply          *AppliedUpdate `json:"apply,omitempty"`
}

// AppliedUpdate records an original update's before/after content and rationale.
type AppliedUpdate struct {
	BeforeBody string `json:"before_body,omitempty"`
	BeforeETag string `json:"before_etag,omitempty"`
	AfterBody  string `json:"after_body,omitempty"`
	AfterETag  string `json:"after_etag,omitempty"`
	Reason     string `json:"reason,omitempty"`
	AppliedAt  string `json:"applied_at,omitempty"`
}

// SavedNote identifies the note an answer was saved to.
type SavedNote struct {
	ClientRequestID string     `json:"client_request_id,omitempty"`
	Vault           string     `json:"vault"` // registry label; an explicit empty label addresses the launch vault
	NoteID          int64      `json:"note_id"`
	Title           string     `json:"title"`
	Status          SaveStatus `json:"status"`
	SavedAt         string     `json:"saved_at,omitempty"`
}

type SaveStatus string

const (
	SaveStatusSaved SaveStatus = "saved"
)

// PriorAnswer is one historical follow-up ancestor.
type PriorAnswer struct {
	RequestID      string `json:"request_id"`
	Intent         Intent `json:"intent,omitempty"`
	Instruction    string `json:"instruction,omitempty"`
	AnswerMarkdown string `json:"answer_markdown,omitempty"`
}

// Dispatch preserves one attempt; it is separate from note-level dispatch.Record.
type Dispatch struct {
	ID                string         `json:"id"`
	Status            DispatchStatus `json:"status"`
	Delivery          DeliveryStatus `json:"delivery,omitempty"`      // set by the connection layer
	DeliveryNote      string         `json:"delivery_note,omitempty"` // connection detail (exit code, stderr clip)
	CreatedAt         string         `json:"created_at"`
	ClaimedAt         string         `json:"claimed_at,omitempty"`
	SettledAt         string         `json:"settled_at,omitempty"`
	FailureReason     string         `json:"failure_reason,omitempty"`
	ResultFingerprint string         `json:"result_fingerprint,omitempty"`
}

// Request is the versioned record stored at .track/requests/<id>.json.
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
