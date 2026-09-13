package webui

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/request"
)

// maxRequestJSONBytes bounds a request-gateway POST body at the HTTP layer. The engine's field
// limits then apply to the individual inputs, so an oversized create or result is refused outright,
// never truncated.
const maxRequestJSONBytes = 1 << 20

// handleRequests serves the request listing (GET) and creation (POST) for the addressed vault. The
// stage-1 listing reads the requests directory directly, newest first, with a limit and a cursor.
func (s *Server) handleRequests(v *vaultView, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, "":
		s.listRequests(v, w, r)
	case http.MethodPost:
		s.createRequest(v, w, r)
	default:
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listRequests(v *vaultView, w http.ResponseWriter, r *http.Request) {
	reqs, next, err := v.requestStore().List(parseLimit(r.URL.Query().Get("limit"), 20), strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"requests": reqs, "next_cursor": next})
}

// createRequest accepts a new agent request. Creation is idempotent under client_request_id: the
// same key with identical input returns the stored request (reused), the same key with different
// input is a 409. The response is 202 with the stored Request, per the request spec.
func (s *Server) createRequest(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if err := requireJSONContentType(r); err != nil {
		writeError(w, err, http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestJSONBytes)
	var in struct {
		ClientRequestID string           `json:"client_request_id"`
		ParentRequestID string           `json:"parent_request_id"`
		Intent          request.Intent   `json:"intent"`
		Instruction     string           `json:"instruction"`
		AgentID         string           `json:"agent_id"`
		Context         request.Context  `json:"context"`
		UpdateTarget    *request.NoteRef `json:"update_target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeDecodeError(w, err)
		return
	}
	// An update must name a note that exists in this vault. The engine stays index-agnostic; the
	// vault's index is what answers "does this note exist here" (the same gate noteByID applies to
	// every id-addressed endpoint, so a foreign id can never act on a same-numbered note elsewhere).
	if in.Intent == request.IntentUpdate {
		if in.UpdateTarget == nil || in.UpdateTarget.NoteID <= 0 {
			writeError(w, errors.New("update requests require an update_target note with a note_id"), http.StatusBadRequest)
			return
		}
		ref, err := v.noteByID(in.UpdateTarget.NoteID)
		if err != nil {
			writeError(w, fmt.Errorf("update target note %d does not exist in this vault", in.UpdateTarget.NoteID), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.UpdateTarget.ETag) == "" {
			writeError(w, errors.New("update requests require the target note etag"), http.StatusBadRequest)
			return
		}
		raw, err := os.ReadFile(v.cfg.PathForKind(ref.FileKind, ref.NoteID))
		if err != nil {
			writeError(w, fmt.Errorf("read update target: %w", err), http.StatusInternalServerError)
			return
		}
		currentETag := note.ContentETag(raw)
		if in.UpdateTarget.ETag != currentETag {
			writeError(w, errors.New("update target changed since it was read"), http.StatusConflict)
			return
		}
		in.UpdateTarget.Vault = v.label
		in.UpdateTarget.NoteID = ref.NoteID
		in.UpdateTarget.Title = ref.Title
		in.UpdateTarget.FileKind = ref.FileKind
		in.UpdateTarget.Body = string(raw)
	}
	if _, ok := v.cfg.Agents[in.AgentID]; !ok {
		writeError(w, fmt.Errorf("agent %q is not registered", in.AgentID), http.StatusBadRequest)
		return
	}
	res, err := v.requestStore().Create(request.NewRequest{
		ClientRequestID: in.ClientRequestID,
		ParentRequestID: in.ParentRequestID,
		Intent:          in.Intent,
		Instruction:     in.Instruction,
		AgentID:         in.AgentID,
		Context:         in.Context,
		UpdateTarget:    in.UpdateTarget,
	}, time.Now())
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]any{"request": res.Request, "reused": res.Reused})
}

// handleRequest returns one stored request with its attempts and result.
func (s *Server) handleRequest(v *vaultView, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != "" {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, errors.New("request id is required"), http.StatusBadRequest)
		return
	}
	req, err := v.requestStore().Get(id)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, req)
}

func (s *Server) handleRequestCancel(v *vaultView, w http.ResponseWriter, r *http.Request) {
	id, ok := s.requestActionID(w, r)
	if !ok {
		return
	}
	res, err := v.requestStore().Cancel(id, time.Now())
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, res.Request)
}

func (s *Server) handleRequestRetry(v *vaultView, w http.ResponseWriter, r *http.Request) {
	id, ok := s.requestActionID(w, r)
	if !ok {
		return
	}
	res, err := v.requestStore().Retry(id, time.Now())
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, res.Request)
}

// handleRequestClaim confirms the execution start of one attempt. The request id comes from the path
// and the dispatch id from the body: claim/result/fail are keyed by BOTH — the request+dispatch dual
// key the request model carries over from docs/spec/agent-state-model.md.
func (s *Server) handleRequestClaim(v *vaultView, w http.ResponseWriter, r *http.Request) {
	id, ok := s.requestActionID(w, r)
	if !ok {
		return
	}
	if !s.authorizeRequestAgent(v, w, r, id) {
		return
	}
	var in struct {
		DispatchID string `json:"dispatch_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeDecodeError(w, err)
		return
	}
	res, err := v.requestStore().Claim(id, in.DispatchID, time.Now())
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, res.Request)
}

func (s *Server) handleRequestResult(v *vaultView, w http.ResponseWriter, r *http.Request) {
	id, ok := s.requestActionID(w, r)
	if !ok {
		return
	}
	if !s.authorizeRequestAgent(v, w, r, id) {
		return
	}
	var in struct {
		DispatchID     string   `json:"dispatch_id"`
		AnswerMarkdown string   `json:"answer_markdown"`
		Sources        []string `json:"sources"`
		ProposedBody   string   `json:"proposed_body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeDecodeError(w, err)
		return
	}
	res, err := v.requestStore().Result(id, in.DispatchID, request.Result{
		AnswerMarkdown: in.AnswerMarkdown,
		Sources:        in.Sources,
		ProposedBody:   in.ProposedBody,
	}, time.Now())
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, res.Request)
}

func (s *Server) handleRequestFail(v *vaultView, w http.ResponseWriter, r *http.Request) {
	id, ok := s.requestActionID(w, r)
	if !ok {
		return
	}
	if !s.authorizeRequestAgent(v, w, r, id) {
		return
	}
	var in struct {
		DispatchID string `json:"dispatch_id"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeDecodeError(w, err)
		return
	}
	res, err := v.requestStore().Fail(id, in.DispatchID, in.Reason, time.Now())
	if err != nil {
		writeRequestError(w, err)
		return
	}
	writeJSON(w, res.Request)
}

// authorizeRequestAgent authenticates only the agent-facing report endpoints. Browser actions remain
// under the existing local Host/Origin guard, while claim/result/fail additionally require the token
// registered for the request's immutable agent_id.
func (s *Server) authorizeRequestAgent(v *vaultView, w http.ResponseWriter, r *http.Request, id string) bool {
	req, err := v.requestStore().Get(id)
	if err != nil {
		writeRequestError(w, err)
		return false
	}
	agent, ok := v.cfg.Agents[req.AgentID]
	if !ok || agent.Token == "" {
		writeError(w, errors.New("request agent is not registered for reports"), http.StatusForbidden)
		return false
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		writeError(w, errors.New("agent report authorization is required"), http.StatusUnauthorized)
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(agent.Token)) != 1 {
		writeError(w, errors.New("agent report authorization is invalid"), http.StatusUnauthorized)
		return false
	}
	return true
}

// requestActionID checks the shared conditions of the action endpoints — POST method, JSON
// Content-Type, a bounded body, and a request id in the path — writing the error response itself.
// cancel/retry reuse it for uniformity, though their bodies are empty.
func (s *Server) requestActionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return "", false
	}
	if err := requireJSONContentType(r); err != nil {
		writeError(w, err, http.StatusUnsupportedMediaType)
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestJSONBytes)
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, errors.New("request id is required"), http.StatusBadRequest)
		return "", false
	}
	return id, true
}

// requireJSONContentType enforces the browser API rule that request-gateway POSTs carry a JSON
// Content-Type.
func requireJSONContentType(r *http.Request) error {
	if ct := r.Header.Get("Content-Type"); ct == "" || !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	return nil
}

// writeDecodeError maps a JSON body decode failure, distinguishing the HTTP body cap from malformed
// JSON.
func writeDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeError(w, fmt.Errorf("request body exceeds %d bytes", maxErr.Limit), http.StatusRequestEntityTooLarge)
		return
	}
	writeError(w, fmt.Errorf("decode request: %w", err), http.StatusBadRequest)
}

// writeRequestError maps a request.Engine error to its HTTP status: unknown resources are 404,
// invalid or oversize input is a 4xx, and the conflict/stale/inactive rejections are 409.
func writeRequestError(w http.ResponseWriter, err error) {
	var re *request.Error
	if !errors.As(err, &re) {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	switch re.Reason {
	case request.RejectUnknownRequest, request.RejectUnknownDispatch:
		writeError(w, err, http.StatusNotFound)
	case request.RejectInvalidRequest:
		writeError(w, err, http.StatusBadRequest)
	case request.RejectOversize:
		writeError(w, err, http.StatusRequestEntityTooLarge)
	default: // stale_dispatch, inactive_dispatch, input_conflict, result_conflict
		writeError(w, err, http.StatusConflict)
	}
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
