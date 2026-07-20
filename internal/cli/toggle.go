package cli

import (
	"flag"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/task"
)

// cmdToggle flips a task-list checkbox on a single line of a note, so agents can update
// "- [ ]"/"- [x]" state without re-emitting the whole note body (where they tend to corrupt
// surrounding text). It is the two-state shorthand over the named task states (`track task set`):
// check maps to the first done-family state, uncheck to the first open state, and toggle flips
// between them — so it shares the completion stamp, sidecar transition log, and progress-cookie
// recompute with the richer command. The target note is named by one of --id/--title/--path and the
// box by --line, the 1-based line number reported by `track search`/`track export`. --state forces a
// result instead of flipping, making check/uncheck idempotent. The note is reindexed so search and
// the tasks listing stay current.
func cmdToggle(args []string) int {
	fs := flag.NewFlagSet("toggle", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	line := fs.Int("line", 0, "1-based line number of the checkbox to toggle")
	state := fs.String("state", "toggle", "resulting state: toggle, check, or uncheck")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *line <= 0 {
		return fail("--line is required and must be positive")
	}
	want := strings.ToLower(strings.TrimSpace(*state))
	switch want {
	case "toggle", "check", "uncheck":
	default:
		return fail("--state must be toggle, check, or uncheck")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	notePath, err := resolveNotePath(cfg, s, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
	if err != nil {
		return fail("%v", err)
	}
	noteID, err := note.IDFromPath(notePath)
	if err != nil {
		return fail("invalid note path: %v", err)
	}

	todo, done, err := task.FirstStates(cfg.TaskStates)
	if err != nil {
		return fail("%v", err)
	}
	target := done.Name
	if want == "uncheck" {
		target = todo.Name
	}
	if want == "toggle" {
		raw, err := os.ReadFile(notePath)
		if err != nil {
			return fail("read note: %v", err)
		}
		cur, ok := task.At(string(raw), *line, cfg.TaskStates)
		if !ok {
			return fail("line %d is not a task checkbox", *line)
		}
		if cur.Done {
			target = todo.Name
		}
	}

	tr, err := note.ApplyTaskState(cfg, notePath, *line, target, time.Now())
	if err != nil {
		return fail("%v", err)
	}
	if err := index.New(cfg, s).One(notePath); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{
		"id":      noteID,
		"path":    notePath,
		"line":    *line,
		"state":   tr.To,
		"checked": tr.Done,
		"changed": tr.Changed,
		"text":    tr.Text,
	})
}
