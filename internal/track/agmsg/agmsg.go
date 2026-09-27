// Package agmsg is track's agmsg connection for the agent request gateway
// (docs/spec/live-agent-requests.md). It is the one stage-2 connection implementation: a small JSON
// envelope — protocol version, request_id, dispatch_id, agent_id — is handed to a configured send.sh
// through exec.CommandContext with a plain argument array, never a shell string and never an
// executable or path supplied by a browser.
//
// The connection's own vocabulary (team, sender, recipient, message ids, read state) stays inside
// this package and the machine config: the common request model and the web API never see it. The
// instruction and context are not in the envelope — they come back with the claim response — so a
// large note never travels as a command-line argument.
package agmsg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ttak0422/track/internal/track/request"
)

// ProtocolVersion is the envelope schema version. The request gateway and the receiving agent agree
// on it; a future change bumps it so old envelopes stay distinguishable from new ones.
const ProtocolVersion = 1

// Envelope is the message body handed to send.sh: the connection identity plus the request+dispatch
// dual key the agent needs to report back through the common claim/result/fail API. The envelope is
// deliberately small — the claim response carries the instruction and context.
type Envelope struct {
	Protocol   int    `json:"protocol"`
	RequestID  string `json:"request_id"`
	DispatchID string `json:"dispatch_id"`
	AgentID    string `json:"agent_id"`
}

// SendConfig is the resolved agmsg connection settings for one agent. It comes from the machine
// config (config.AgentConfig.Agmsg), never from a browser or a request body.
type SendConfig struct {
	// Script is the agmsg send.sh path. It is invoked directly, as an executable, with a plain
	// argument array (send.sh <team> <sender> <recipient> <message>).
	Script string
	Team   string
	Sender string
	// Recipient is the agmsg destination name for this agent, resolved from the connection config —
	// deliberately distinct from the agent id.
	Recipient string
}

// Outcome is a send's normalized delivery result: the connection-agnostic sent/failed/unknown set of
// docs/spec/live-agent-requests.md. Note is a short human-readable detail for the attempt record.
type Outcome struct {
	Delivery request.DeliveryStatus
	Note     string
}

// maxNoteBytes bounds the note recorded on the attempt, so a chatty send.sh cannot bloat the request
// file. The note is diagnostics, not content.
const maxNoteBytes = 512

// Send delivers an envelope through a configured send.sh and normalizes the outcome:
//
//   - exit 0 is DeliverySent;
//   - a non-zero exit is DeliveryFailed — send.sh refused to send, which is a confirmed delivery
//     failure, not a proof either way;
//   - a context deadline (or cancellation) is DeliveryUnknown — a timeout is not evidence that the
//     message was not delivered, so the attempt stays queued and may be re-attempted under the same
//     dispatch id.
//
// A successful send is never read as an execution start: the caller records the outcome on the
// attempt, and only a claim (through the common API) confirms the run.
func Send(ctx context.Context, cfg SendConfig, env Envelope) Outcome {
	if strings.TrimSpace(cfg.Script) == "" {
		return Outcome{Delivery: request.DeliveryFailed, Note: "no agmsg send_script configured for this agent"}
	}
	body, err := json.Marshal(env)
	if err != nil {
		return Outcome{Delivery: request.DeliveryFailed, Note: "encode envelope: " + err.Error()}
	}
	// Plain argument array: send.sh <team> <from> <to> <message>. Nothing here is ever assembled
	// into a shell string, so no argument — however the config or the request ids spell it — can
	// reach a shell. The message body is the small JSON envelope; the note's instruction and context
	// are fetched by the agent through the claim response instead of riding on the command line.
	cmd := exec.CommandContext(ctx, cfg.Script, cfg.Team, cfg.Sender, cfg.Recipient, string(body))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		switch {
		case ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled:
			return Outcome{Delivery: request.DeliveryUnknown, Note: "send timed out; delivery outcome unknown"}
		default:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return Outcome{Delivery: request.DeliveryFailed, Note: clipNote(fmt.Sprintf("send.sh exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr.String())))}
			}
			return Outcome{Delivery: request.DeliveryFailed, Note: clipNote("run send.sh: " + err.Error())}
		}
	}
	return Outcome{Delivery: request.DeliverySent, Note: "sent"}
}

// clipNote bounds a delivery note to maxNoteBytes, preserving the start of the diagnostic.
func clipNote(s string) string {
	if len(s) <= maxNoteBytes {
		return s
	}
	return s[:maxNoteBytes] + "…"
}
