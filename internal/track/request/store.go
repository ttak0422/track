package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// RequestsDirName is the per-vault directory that holds request files, under .track (the same
// directory tree the spec designates as backup scope and out of static publish).
const RequestsDirName = "requests"

// maxListLimit bounds a list page. The stage-1 directory read is the only listing mechanism; the
// spec defers an index until measured counts make one necessary.
const maxListLimit = 100

// Store reads and mutates the request files of one vault. It is the authoritative home of request
// state: every transition loads the current file, applies the change under a per-store mutex
// (compare-and-swap on the stored status), and replaces the file atomically via temp+rename. The
// mutex serializes transitions within this server so two concurrent reports cannot both win; the
// spec's cross-process guard (one request server per vault) keeps the file itself the CAS boundary.
type Store struct {
	mu sync.Mutex
	// dir is <vault>/.track/requests, the authoritative home of the request files.
	dir string
	// vault and vaultPath are this store's own vault identity, stamped onto every request it creates:
	// the registry label the wire uses (?vault=) and the canonical (symlink-resolved) vault directory.
	// A request's file already lives in this vault, so the identity is a self-describing record, not a
	// routing key — client-supplied vault names are never trusted for it.
	vault     string
	vaultPath string
}

// New returns the request store for a vault. The requests directory is created on first write.
func New(cfg *config.Config) *Store {
	return &Store{
		dir:       cfg.RequestsDir(),
		vault:     cfg.VaultName,
		vaultPath: cfg.VaultDir,
	}
}

// CreateResult is the outcome of Create: the stored request and whether it was reused from a prior
// create with the same client_request_id and identical input.
type CreateResult struct {
	Request Request
	Reused  bool
}

// TransitionResult is the outcome of a transition: the stored request and whether the call was an
// idempotent replay that changed nothing (a repeated claim, the same result, a second cancel/fail).
type TransitionResult struct {
	Request    Request
	Idempotent bool
}

// Create stores a new request and its first attempt in StatusQueued. With a client_request_id, the
// create is idempotent: the same key with identical input returns the existing request (Reused), the
// same key with different input is refused with RejectInputConflict, and without a key every call
// creates a distinct request.
func (s *Store) Create(in NewRequest, now time.Time) (CreateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateNew(in); err != nil {
		return CreateResult{}, err
	}
	if in.ClientRequestID != "" {
		existing, err := s.findByClientRequestID(in.ClientRequestID)
		if err != nil {
			return CreateResult{}, err
		}
		if existing.ID != "" {
			if existing.InputFingerprint == inputFingerprint(&in) {
				return CreateResult{Request: existing, Reused: true}, nil
			}
			return CreateResult{}, reject(RejectInputConflict,
				"client_request_id %q was already used with different input (request %s)", in.ClientRequestID, existing.ID)
		}
	}

	id, err := genID("req", now)
	if err != nil {
		return CreateResult{}, err
	}
	dispatchID, err := genID("d", now)
	if err != nil {
		return CreateResult{}, err
	}
	r := Request{
		Version:          CurrentVersion,
		ID:               id,
		ClientRequestID:  in.ClientRequestID,
		ParentRequestID:  in.ParentRequestID,
		Vault:            s.vault,
		VaultPath:        s.vaultPath,
		Intent:           in.Intent,
		Instruction:      in.Instruction,
		AgentID:          in.AgentID,
		Context:          in.Context,
		UpdateTarget:     in.UpdateTarget,
		InputFingerprint: inputFingerprint(&in),
		Status:           StatusQueued,
		CreatedAt:        now.Format(time.RFC3339),
		Attempts: []Dispatch{{
			ID:        dispatchID,
			Status:    AttemptQueued,
			CreatedAt: now.Format(time.RFC3339),
		}},
	}
	if err := s.save(&r, now); err != nil {
		return CreateResult{}, fmt.Errorf("persist request: %w", err)
	}
	return CreateResult{Request: r}, nil
}

// validateNew checks a create input against the model and the size limits.
func validateNew(in NewRequest) error {
	if !ValidIntent(in.Intent) {
		return reject(RejectInvalidRequest, "invalid intent %q (want one of: %s)", in.Intent, intentsString())
	}
	if strings.TrimSpace(in.Instruction) == "" {
		return reject(RejectInvalidRequest, "instruction is required")
	}
	if strings.TrimSpace(in.AgentID) == "" {
		return reject(RejectInvalidRequest, "agent_id is required")
	}
	if in.Intent == IntentUpdate {
		if in.UpdateTarget == nil || in.UpdateTarget.NoteID <= 0 {
			return reject(RejectInvalidRequest, "update requests require an update_target note with a note_id")
		}
	} else if in.UpdateTarget != nil {
		return reject(RejectInvalidRequest, "update_target is only valid for the update intent")
	}
	if len(in.Instruction) > MaxInstructionBytes {
		return reject(RejectOversize, "instruction exceeds %d bytes", MaxInstructionBytes)
	}
	if len(in.Context.Quote) > MaxQuoteBytes {
		return reject(RejectOversize, "context.quote exceeds %d bytes", MaxQuoteBytes)
	}
	if n := in.Context.Note; n != nil && len(n.Body) > MaxNoteBodyBytes {
		return reject(RejectOversize, "context.note body exceeds %d bytes", MaxNoteBodyBytes)
	}
	var refsBytes int
	for _, ref := range in.Context.References {
		refsBytes += len(ref.Body)
	}
	if refsBytes > MaxReferencesBytes {
		return reject(RejectOversize, "reference bodies exceed %d bytes in total", MaxReferencesBytes)
	}
	return nil
}

func intentsString() string {
	names := make([]string, len(Intents()))
	for i, in := range Intents() {
		names[i] = string(in)
	}
	return strings.Join(names, ", ")
}

// Claim confirms the execution start of one attempt. Only the request's current attempt can be
// claimed, and only while the request is queued. A repeated claim of the same already-running current
// attempt is an idempotent success (the connection may retry a lost response).
func (s *Store) Claim(id, dispatchID string, now time.Time) (TransitionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.load(id)
	if err != nil {
		return TransitionResult{}, err
	}
	if strings.TrimSpace(dispatchID) == "" {
		return TransitionResult{}, reject(RejectInvalidRequest, "dispatch_id is required")
	}
	d, err := r.dispatch(dispatchID)
	if err != nil {
		return TransitionResult{}, err
	}
	if cur := r.currentDispatch(); cur == nil || cur.ID != dispatchID {
		return TransitionResult{}, reject(RejectStaleDispatch,
			"dispatch %s is not the current attempt of request %s", dispatchID, id)
	}
	switch r.Status {
	case StatusQueued:
		if d.Status != AttemptQueued {
			return TransitionResult{}, reject(RejectInactiveDispatch,
				"attempt %s of request %s is %s, not queued", dispatchID, id, d.Status)
		}
	case StatusRunning:
		if d.Status == AttemptRunning {
			return TransitionResult{Request: r, Idempotent: true}, nil
		}
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"attempt %s of request %s is %s, not running", dispatchID, id, d.Status)
	default:
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"request %s is %s; only queued requests can be claimed", id, r.Status)
	}
	r.Status = StatusRunning
	d.Status = AttemptRunning
	d.ClaimedAt = now.Format(time.RFC3339)
	if err := s.save(&r, now); err != nil {
		return TransitionResult{}, fmt.Errorf("persist claim: %w", err)
	}
	return TransitionResult{Request: r}, nil
}

// Result adopts an attempt's answer or proposed update body. Only the current attempt's result is
// accepted, and only while the request is running. Re-submitting the identical result to a completed
// request is an idempotent success; a different result over a completed request is refused with
// RejectResultConflict. The result fingerprint is computed server-side from the submitted content.
func (s *Store) Result(id, dispatchID string, res Result, now time.Time) (TransitionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.load(id)
	if err != nil {
		return TransitionResult{}, err
	}
	if strings.TrimSpace(dispatchID) == "" {
		return TransitionResult{}, reject(RejectInvalidRequest, "dispatch_id is required")
	}
	if err := validateResult(r.Intent, res); err != nil {
		return TransitionResult{}, err
	}
	d, err := r.dispatch(dispatchID)
	if err != nil {
		return TransitionResult{}, err
	}
	if cur := r.currentDispatch(); cur == nil || cur.ID != dispatchID {
		return TransitionResult{}, reject(RejectStaleDispatch,
			"dispatch %s is not the current attempt of request %s", dispatchID, id)
	}
	fp := resultFingerprint(&res)
	switch r.Status {
	case StatusRunning:
		if d.Status != AttemptRunning {
			return TransitionResult{}, reject(RejectInactiveDispatch,
				"attempt %s of request %s is %s, not running", dispatchID, id, d.Status)
		}
	case StatusCompleted:
		if d.Status == AttemptCompleted && r.Result != nil && r.Result.Fingerprint == fp {
			return TransitionResult{Request: r, Idempotent: true}, nil
		}
		return TransitionResult{}, reject(RejectResultConflict,
			"request %s is already completed with a different result", id)
	default:
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"request %s is %s; only running requests accept results", id, r.Status)
	}
	// An update's proposed body is recorded, not applied: the server-side apply with ETag re-read and
	// the running -> applying -> completed/conflict path is the update-apply stage. Until then the
	// request completes with the proposal kept.
	r.Status = StatusCompleted
	r.Error = ""
	d.Status = AttemptCompleted
	d.SettledAt = now.Format(time.RFC3339)
	d.ResultFingerprint = fp
	res.Fingerprint = fp
	r.Result = &res
	if err := s.save(&r, now); err != nil {
		return TransitionResult{}, fmt.Errorf("persist result: %w", err)
	}
	return TransitionResult{Request: r}, nil
}

// validateResult checks a submitted result against the request intent and the size limits. An update
// result must carry the proposed body; explain/research results must carry the answer.
func validateResult(intent Intent, res Result) error {
	if intent == IntentUpdate {
		if strings.TrimSpace(res.ProposedBody) == "" {
			return reject(RejectInvalidRequest, "an update result requires a proposed_body")
		}
	} else if strings.TrimSpace(res.AnswerMarkdown) == "" {
		return reject(RejectInvalidRequest, "an explain/research result requires an answer_markdown")
	}
	if len(res.AnswerMarkdown) > MaxAnswerBytes {
		return reject(RejectOversize, "answer exceeds %d bytes", MaxAnswerBytes)
	}
	if len(res.ProposedBody) > MaxProposedBodyBytes {
		return reject(RejectOversize, "proposed body exceeds %d bytes", MaxProposedBodyBytes)
	}
	if len(res.Sources) > MaxSources {
		return reject(RejectOversize, "sources exceed %d entries", MaxSources)
	}
	return nil
}

// SetDelivery records a connection's delivery outcome on one attempt. Delivery is the connection
// layer's report of handing the attempt to an agent — a successful send is never read as an execution
// start or a completion, so this transition touches only the attempt's delivery fields and never the
// request or attempt status. The write is confined to the current attempt: a superseded or foreign
// dispatch's outcome is skipped (Idempotent) so a late send.sh report can never overwrite the live
// attempt's record. Only the closed delivery status set is accepted.
func (s *Store) SetDelivery(id, dispatchID string, d DeliveryStatus, note string, now time.Time) (TransitionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !ValidDeliveryStatus(d) {
		return TransitionResult{}, reject(RejectInvalidRequest, "invalid delivery status %q", d)
	}
	r, err := s.load(id)
	if err != nil {
		return TransitionResult{}, err
	}
	if strings.TrimSpace(dispatchID) == "" {
		return TransitionResult{}, reject(RejectInvalidRequest, "dispatch_id is required")
	}
	attempt, err := r.dispatch(dispatchID)
	if err != nil {
		return TransitionResult{}, err
	}
	if cur := r.currentDispatch(); cur == nil || cur.ID != dispatchID {
		// The attempt was superseded (a retry) while the send was in flight: the outcome no longer
		// matters and must not touch the live attempt's record.
		return TransitionResult{Request: r, Idempotent: true}, nil
	}
	if attempt.Delivery == d && attempt.DeliveryNote == note {
		return TransitionResult{Request: r, Idempotent: true}, nil
	}
	attempt.Delivery = d
	attempt.DeliveryNote = note
	if err := s.save(&r, now); err != nil {
		return TransitionResult{}, fmt.Errorf("persist delivery: %w", err)
	}
	return TransitionResult{Request: r}, nil
}

// Fail records a confirmed execution failure for the current attempt. Re-failing the same already
// failed attempt is an idempotent success.
func (s *Store) Fail(id, dispatchID, reason string, now time.Time) (TransitionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.load(id)
	if err != nil {
		return TransitionResult{}, err
	}
	if strings.TrimSpace(dispatchID) == "" {
		return TransitionResult{}, reject(RejectInvalidRequest, "dispatch_id is required")
	}
	if len(reason) > MaxFailureReasonBytes {
		return TransitionResult{}, reject(RejectOversize, "failure reason exceeds %d bytes", MaxFailureReasonBytes)
	}
	d, err := r.dispatch(dispatchID)
	if err != nil {
		return TransitionResult{}, err
	}
	if cur := r.currentDispatch(); cur == nil || cur.ID != dispatchID {
		return TransitionResult{}, reject(RejectStaleDispatch,
			"dispatch %s is not the current attempt of request %s", dispatchID, id)
	}
	switch r.Status {
	case StatusQueued:
		if d.Status != AttemptQueued {
			return TransitionResult{}, reject(RejectInactiveDispatch,
				"attempt %s of request %s is %s, not queued", dispatchID, id, d.Status)
		}
	case StatusRunning:
		if d.Status != AttemptRunning {
			return TransitionResult{}, reject(RejectInactiveDispatch,
				"attempt %s of request %s is %s, not running", dispatchID, id, d.Status)
		}
	case StatusFailed:
		if d.Status == AttemptFailed {
			return TransitionResult{Request: r, Idempotent: true}, nil
		}
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"attempt %s of request %s is %s, not failed", dispatchID, id, d.Status)
	default:
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"request %s is %s; only queued or running requests accept failures", id, r.Status)
	}
	r.Status = StatusFailed
	r.Error = reason
	d.Status = AttemptFailed
	d.FailureReason = reason
	d.SettledAt = now.Format(time.RFC3339)
	if err := s.save(&r, now); err != nil {
		return TransitionResult{}, fmt.Errorf("persist failure: %w", err)
	}
	return TransitionResult{Request: r}, nil
}

// Cancel withdraws a request while it is queued or running; later results are rejected because the
// request is no longer in a state that accepts them. A second cancel is an idempotent success.
func (s *Store) Cancel(id string, now time.Time) (TransitionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.load(id)
	if err != nil {
		return TransitionResult{}, err
	}
	switch r.Status {
	case StatusQueued, StatusRunning:
	case StatusCancelled:
		return TransitionResult{Request: r, Idempotent: true}, nil
	default:
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"request %s is %s; only queued or running requests can be cancelled", id, r.Status)
	}
	if d := r.currentDispatch(); d != nil {
		d.Status = AttemptCancelled
		d.SettledAt = now.Format(time.RFC3339)
	}
	r.Status = StatusCancelled
	r.Error = ""
	if err := s.save(&r, now); err != nil {
		return TransitionResult{}, fmt.Errorf("persist cancellation: %w", err)
	}
	return TransitionResult{Request: r}, nil
}

// Retry starts a new attempt: the request returns to queued with a fresh dispatch id, and the old
// attempt stays in the record with its terminal state. It is available after a failure, after an
// update conflict (stage 3), or from a running request whose agent stopped responding (the run is
// superseded: its attempt is closed as cancelled rather than left dangling). Completed, queued, and
// cancelled requests are not retried — the user starts a new request for those.
func (s *Store) Retry(id string, now time.Time) (TransitionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.load(id)
	if err != nil {
		return TransitionResult{}, err
	}
	switch r.Status {
	case StatusRunning:
		if d := r.currentDispatch(); d != nil {
			d.Status = AttemptCancelled // superseded by the retry, not an agent failure
			d.SettledAt = now.Format(time.RFC3339)
		}
	case StatusFailed, StatusConflict:
		// The old attempt keeps its terminal state; the history is preserved.
	default:
		return TransitionResult{}, reject(RejectInactiveDispatch,
			"request %s is %s; retry is available after a failure, a conflict, or a stalled run", id, r.Status)
	}
	dispatchID, err := genID("d", now)
	if err != nil {
		return TransitionResult{}, err
	}
	r.Attempts = append(r.Attempts, Dispatch{
		ID:        dispatchID,
		Status:    AttemptQueued,
		CreatedAt: now.Format(time.RFC3339),
	})
	r.Status = StatusQueued
	r.Error = ""
	if err := s.save(&r, now); err != nil {
		return TransitionResult{}, fmt.Errorf("persist retry: %w", err)
	}
	return TransitionResult{Request: r}, nil
}

// Get returns one stored request. RejectUnknownRequest when the file does not exist.
func (s *Store) Get(id string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

// List returns requests newest first, up to limit (bounded by maxListLimit), with a cursor for the
// next page. The cursor is the id of the last returned request; the next call resumes after it. An
// empty result with an empty cursor means there is no next page. Listing reads the requests directory
// directly — the spec defers an index until directory reads become a measured problem.
func (s *Store) List(limit int, cursor string) ([]Request, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Request{}, "", nil
		}
		return nil, "", fmt.Errorf("list requests: %w", err)
	}
	requests := make([]Request, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue // request files only; temp files (hidden, .tmp) never surface here
		}
		r, err := s.load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return nil, "", err
		}
		requests = append(requests, r)
	}
	// Newest first. Request ids embed unix milliseconds, so the id order is the creation order within
	// one process clock; the timestamp tiebreak keeps hand-edited files in a stable place.
	slices.SortFunc(requests, func(a, b Request) int {
		if a.CreatedAt != b.CreatedAt {
			return strings.Compare(b.CreatedAt, a.CreatedAt)
		}
		return strings.Compare(b.ID, a.ID)
	})
	if cursor != "" {
		start := -1
		for i, r := range requests {
			if r.ID == cursor {
				start = i + 1
				break
			}
		}
		if start == -1 {
			return []Request{}, "", nil // a cursor this vault does not know: no further page
		}
		requests = requests[start:]
	}
	next := ""
	if len(requests) > limit {
		next = requests[limit-1].ID
		requests = requests[:limit]
	}
	return requests, next, nil
}

// dispatch resolves an attempt by id within a request, or RejectUnknownDispatch.
func (r *Request) dispatch(id string) (*Dispatch, error) {
	for i := range r.Attempts {
		if r.Attempts[i].ID == id {
			return &r.Attempts[i], nil
		}
	}
	return nil, reject(RejectUnknownDispatch, "dispatch %q is not an attempt of request %s", id, r.ID)
}

// currentDispatch is the request's latest attempt: the only one that can be claimed or settled.
func (r *Request) currentDispatch() *Dispatch {
	if len(r.Attempts) == 0 {
		return nil
	}
	return &r.Attempts[len(r.Attempts)-1]
}

// findByClientRequestID scans the requests directory for a request carrying key. Files that cannot be
// loaded (corrupt or from a newer schema) are skipped so a broken file never blocks new creates; the
// read surfaces (Get, List) report it instead.
func (s *Store) findByClientRequestID(key string) (Request, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Request{}, nil
		}
		return Request{}, fmt.Errorf("scan requests for client_request_id: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		r, err := s.load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		if r.ClientRequestID == key {
			return r, nil
		}
	}
	return Request{}, nil
}

// load reads, parses, and validates one request file. A missing file is RejectUnknownRequest; a file
// that is not valid JSON or carries an unsupported version is refused rather than half-read.
func (s *Store) load(id string) (Request, error) {
	if strings.TrimSpace(id) == "" {
		return Request{}, reject(RejectUnknownRequest, "request id is required")
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Request{}, reject(RejectUnknownRequest, "no request %q", id)
		}
		return Request{}, fmt.Errorf("read request %s: %w", id, err)
	}
	var r Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return Request{}, fmt.Errorf("request %s is not valid JSON: %w", id, err)
	}
	if r.Version < 1 || r.Version > CurrentVersion {
		return Request{}, fmt.Errorf("request %s has unsupported version %d (this build reads 1..%d)",
			id, r.Version, CurrentVersion)
	}
	if !ValidStatus(r.Status) {
		return Request{}, fmt.Errorf("request %s has invalid status %q", id, r.Status)
	}
	for _, d := range r.Attempts {
		if !ValidDispatchStatus(d.Status) {
			return Request{}, fmt.Errorf("request %s has invalid attempt status %q", id, d.Status)
		}
	}
	return r, nil
}

// save replaces a request file atomically: the JSON is written to a hidden temp file in the same
// directory, synced, and renamed over the target, so a crash never leaves a torn request file and a
// concurrent reader sees either the old or the new version.
func (s *Store) save(r *Request, now time.Time) error {
	r.UpdatedAt = now.Format(time.RFC3339)
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode request %s: %w", r.ID, err)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create requests dir: %w", err)
	}
	path := filepath.Join(s.dir, r.ID+".json")
	if err := writeFileAtomic(path, append(out, '\n')); err != nil {
		return fmt.Errorf("write request %s: %w", r.ID, err)
	}
	return nil
}

// writeFileAtomic writes data to path via a same-directory temp file and rename. The temp file is
// hidden (dot prefix) so directory scans of the requests dir never treat it as a request.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename moved it
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
