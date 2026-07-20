package babel

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Executor maps a language to the command that runs a source block.
// "{{file}}" in Args is replaced with the path of a temp file holding the block body; if no arg
// contains it, the body is fed to the command on stdin instead.
type Executor struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// filePlaceholder is substituted with the temp script path in an Executor's Args.
const filePlaceholder = "{{file}}"

// Run errors. Evaluation is gated by the block's :eval header argument so that running a block is
// always an explicit, policy-checked action rather than a side effect of opening a note.
var (
	ErrEvalDisabled    = errors.New("babel: evaluation disabled by :eval no")
	ErrConfirmRequired = errors.New("babel: :eval query requires explicit confirmation")
	ErrNoExecutor      = errors.New("babel: no executor configured for language")
)

// Runner executes source blocks using configured language executors.
type Runner struct {
	languages map[string]Executor
}

// NewRunner builds a Runner from a language -> executor map (typically config.BabelLanguages).
func NewRunner(languages map[string]Executor) *Runner {
	return &Runner{languages: languages}
}

// RunOptions controls a single execution.
type RunOptions struct {
	Dir       string            // working directory for the process; caller resolves and validates :dir
	Confirmed bool              // set when the user explicitly confirmed; required for :eval query
	Timeout   time.Duration     // hard limit on the process; 0 means no limit
	Vars      map[string]string // resolved :var / --var inputs, exported to the process environment
}

// Run evaluates a block subject to its :eval policy and returns the captured result.
// Gate and configuration problems return an error; a process that runs but exits non-zero is a
// RunResult with a "failed" status and the exit code, not an error.
func (r *Runner) Run(b Block, opts RunOptions) (RunResult, error) {
	switch firstValue(b.HeaderArgs, "eval") {
	case "no", "never":
		return RunResult{}, ErrEvalDisabled
	case "query":
		if !opts.Confirmed {
			return RunResult{}, ErrConfirmRequired
		}
	}

	ex, ok := r.languages[b.Language]
	if !ok {
		return RunResult{}, ErrNoExecutor
	}

	file, cleanup, err := writeTempScript(b)
	if err != nil {
		return RunResult{}, err
	}
	defer cleanup()

	args := make([]string, len(ex.Args))
	usesFile := false
	for i, a := range ex.Args {
		if strings.Contains(a, filePlaceholder) {
			usesFile = true
		}
		args[i] = strings.ReplaceAll(a, filePlaceholder, file)
	}

	ctx := context.Background()
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, ex.Command, args...)
	cmd.Dir = opts.Dir
	if len(opts.Vars) > 0 {
		// Variables reach the block as environment entries: the one mechanism every configured
		// executor shares, since track never generates language-specific assignment code.
		env := os.Environ()
		for _, k := range sortedVarKeys(opts.Vars) {
			env = append(env, k+"="+opts.Vars[k])
		}
		cmd.Env = env
	}
	if !usesFile {
		cmd.Stdin = strings.NewReader(b.Body)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	started := time.Now().UTC()
	runErr := cmd.Run()
	finished := time.Now().UTC()

	res := RunResult{
		StartedAt:  started.Format(time.RFC3339),
		FinishedAt: finished.Format(time.RFC3339),
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitCode:   cmd.ProcessState.ExitCode(),
	}

	switch {
	case runErr == nil:
		res.Status = "success"
	case ctx.Err() == context.DeadlineExceeded:
		res.Status = "timeout"
	default:
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.Status = "failed"
		} else {
			// The process never ran (e.g. command not found); surface it as an error.
			return RunResult{}, runErr
		}
	}
	return res, nil
}

// sortedVarKeys returns the map's keys in a stable order so the injected environment is deterministic.
func sortedVarKeys(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeTempScript writes the block body to a temp file and returns its path and a cleanup func.
func writeTempScript(b Block) (string, func(), error) {
	f, err := os.CreateTemp("", "track-babel-*")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := f.WriteString(b.Body); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}
