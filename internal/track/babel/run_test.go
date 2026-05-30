package babel

import (
	"os/exec"
	"testing"
	"time"
)

func shRunner(t *testing.T) *Runner {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	return NewRunner(map[string]Executor{"sh": {Command: "sh", Args: []string{"{{file}}"}}})
}

func TestRunCapturesStdoutAndSuccess(t *testing.T) {
	res, err := shRunner(t).Run(Block{Language: "sh", Body: "echo hello"}, RunOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stdout != "hello\n" || res.ExitCode != 0 || res.Status != "success" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.StartedAt == "" || res.FinishedAt == "" {
		t.Fatalf("timestamps should be set: %+v", res)
	}
}

func TestRunNonZeroExitIsFailedNotError(t *testing.T) {
	res, err := shRunner(t).Run(Block{Language: "sh", Body: "exit 3"}, RunOptions{})
	if err != nil {
		t.Fatalf("non-zero exit should not be a Go error, got %v", err)
	}
	if res.ExitCode != 3 || res.Status != "failed" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRunEvalGate(t *testing.T) {
	r := shRunner(t)
	if _, err := r.Run(Block{Language: "sh", HeaderArgs: map[string][]string{"eval": {"no"}}, Body: "echo x"}, RunOptions{}); err != ErrEvalDisabled {
		t.Fatalf("eval no: want ErrEvalDisabled, got %v", err)
	}
	if _, err := r.Run(Block{Language: "sh", HeaderArgs: map[string][]string{"eval": {"query"}}, Body: "echo x"}, RunOptions{}); err != ErrConfirmRequired {
		t.Fatalf("eval query unconfirmed: want ErrConfirmRequired, got %v", err)
	}
	res, err := r.Run(Block{Language: "sh", HeaderArgs: map[string][]string{"eval": {"query"}}, Body: "echo ok"}, RunOptions{Confirmed: true})
	if err != nil || res.Stdout != "ok\n" {
		t.Fatalf("eval query confirmed: err=%v res=%+v", err, res)
	}
}

func TestRunNoExecutor(t *testing.T) {
	if _, err := NewRunner(map[string]Executor{}).Run(Block{Language: "lua", Body: "x"}, RunOptions{}); err != ErrNoExecutor {
		t.Fatalf("want ErrNoExecutor, got %v", err)
	}
}

func TestRunStdinFallbackWithoutPlaceholder(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	// No {{file}} placeholder -> the body is fed on stdin.
	r := NewRunner(map[string]Executor{"sh": {Command: "sh", Args: []string{}}})
	res, err := r.Run(Block{Language: "sh", Body: "echo viastdin"}, RunOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stdout != "viastdin\n" {
		t.Fatalf("stdin fallback: %+v", res)
	}
}

func TestRunTimeout(t *testing.T) {
	res, err := shRunner(t).Run(Block{Language: "sh", Body: "sleep 5"}, RunOptions{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != "timeout" {
		t.Fatalf("want timeout status, got %+v", res)
	}
}
