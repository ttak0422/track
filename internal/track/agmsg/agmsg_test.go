package agmsg

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/request"
)

// fakeSendScript writes an executable send.sh that logs its argv to logPath (one argument per line,
// shell-quoted) and then runs body. Tests assert on the log to prove exactly what arguments the
// connection handed the script.
func fakeSendScript(t *testing.T, logPath, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "send.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + logPath + "\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake send.sh: %v", err)
	}
	return script
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read send log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func env() Envelope {
	return Envelope{
		Protocol:   ProtocolVersion,
		RequestID:  "req-1700000000000-abc",
		DispatchID: "d-1700000000000-def",
		AgentID:    "research-assistant",
	}
}

func TestSendPassesPlainArgumentsAndEnvelope(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv.log")
	script := fakeSendScript(t, log, "exit 0")

	outcome := Send(context.Background(), SendConfig{
		Script:    script,
		Team:      "team-alpha",
		Sender:    "track-web",
		Recipient: "research-agent",
	}, env())

	if outcome.Delivery != request.DeliverySent {
		t.Fatalf("delivery = %q, want sent (%s)", outcome.Delivery, outcome.Note)
	}
	args := readLog(t, log)
	if len(args) != 4 {
		t.Fatalf("send.sh received %d args, want exactly 4 (no shell wrapper): %q", len(args), args)
	}
	if args[0] != "team-alpha" || args[1] != "track-web" || args[2] != "research-agent" {
		t.Fatalf("team/sender/recipient wrong: %q", args)
	}
	// The message argument is the small JSON envelope, and only that.
	var got Envelope
	if err := json.Unmarshal([]byte(args[3]), &got); err != nil {
		t.Fatalf("message is not the envelope JSON: %v (%q)", err, args[3])
	}
	if got.Protocol != ProtocolVersion || got.RequestID != "req-1700000000000-abc" ||
		got.DispatchID != "d-1700000000000-def" || got.AgentID != "research-assistant" {
		t.Fatalf("envelope content wrong: %+v", got)
	}
}

// TestSendNeverBuildsShellStrings passes hostile values through every slot and proves they travel as
// literal argv entries: no shell is involved, so nothing can be executed or redirected by an id.
func TestSendNeverBuildsShellStrings(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv.log")
	marker := filepath.Join(t.TempDir(), "pwned")
	// The script would succeed regardless; the marker proves no injected command ran.
	script := fakeSendScript(t, log, "exit 0")

	hostile := Envelope{
		Protocol:   ProtocolVersion,
		RequestID:  "req-1; touch " + marker,
		DispatchID: "d-$(touch " + marker + ")`",
		AgentID:    "a; echo shell > " + marker + "; #",
	}
	outcome := Send(context.Background(), SendConfig{
		Script:    script,
		Team:      "team 'quoted'",
		Sender:    "from|pipe>redirect",
		Recipient: "to && rm -rf /",
	}, hostile)

	if outcome.Delivery != request.DeliverySent {
		t.Fatalf("delivery = %q, want sent (%s)", outcome.Delivery, outcome.Note)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("hostile argument reached a shell: marker file created")
	}
	args := readLog(t, log)
	if len(args) != 4 {
		t.Fatalf("send.sh received %d args, want 4: %q", len(args), args)
	}
	// The envelope arrived byte-identical through argv.
	re := &Envelope{}
	if err := json.Unmarshal([]byte(args[3]), re); err != nil {
		t.Fatalf("message not the envelope: %v", err)
	}
	if re.AgentID != hostile.AgentID || re.RequestID != hostile.RequestID || re.DispatchID != hostile.DispatchID {
		t.Fatalf("hostile ids were mangled: %+v", re)
	}
}

func TestSendFailureIsConfirmed(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv.log")
	script := fakeSendScript(t, log, "echo 'roster check failed' >&2\nexit 1")

	outcome := Send(context.Background(), SendConfig{
		Script:    script,
		Team:      "t",
		Sender:    "s",
		Recipient: "r",
	}, env())

	if outcome.Delivery != request.DeliveryFailed {
		t.Fatalf("non-zero exit must be a confirmed failure, got %q (%s)", outcome.Delivery, outcome.Note)
	}
	if !strings.Contains(outcome.Note, "exited 1") || !strings.Contains(outcome.Note, "roster check failed") {
		t.Fatalf("failure note should carry the exit code and stderr: %q", outcome.Note)
	}
}

func TestSendMissingScriptIsFailed(t *testing.T) {
	outcome := Send(context.Background(), SendConfig{Team: "t", Sender: "s", Recipient: "r"}, env())
	if outcome.Delivery != request.DeliveryFailed {
		t.Fatalf("missing script must fail, got %q", outcome.Delivery)
	}
	if !strings.Contains(outcome.Note, "send_script") {
		t.Fatalf("note should name the missing send_script: %q", outcome.Note)
	}
}

func TestSendUnrunnableScriptIsFailed(t *testing.T) {
	outcome := Send(context.Background(), SendConfig{
		Script:    filepath.Join(t.TempDir(), "does-not-exist.sh"),
		Team:      "t",
		Sender:    "s",
		Recipient: "r",
	}, env())
	if outcome.Delivery != request.DeliveryFailed {
		t.Fatalf("an unrunnable script must fail, got %q (%s)", outcome.Delivery, outcome.Note)
	}
}

func TestSendTimeoutIsUnknownNotFailed(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv.log")
	script := fakeSendScript(t, log, "sleep 30")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	outcome := Send(ctx, SendConfig{
		Script:    script,
		Team:      "t",
		Sender:    "s",
		Recipient: "r",
	}, env())

	// The script may or may not have finished before the deadline; either way the outcome is
	// unknown, never failed — a timeout is not proof the message was not delivered.
	if outcome.Delivery != request.DeliveryUnknown {
		t.Fatalf("timeout must be unknown, got %q (%s)", outcome.Delivery, outcome.Note)
	}
	if !strings.Contains(outcome.Note, "timed out") {
		t.Fatalf("note should say the send timed out: %q", outcome.Note)
	}
}
