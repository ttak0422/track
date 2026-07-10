package cli

import (
	"flag"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/task"
)

// cmdTask routes `track task <sub>`; set is the only subcommand today.
func cmdTask(args []string) int {
	if len(args) == 0 {
		return fail("usage: track task set (--id N | --title S | --path P) --line N --state NAME")
	}
	switch args[0] {
	case "set":
		return cmdTaskSet(args[1:])
	default:
		return fail("unknown task subcommand %q (expected: set)", args[0])
	}
}

// cmdTaskSet moves the task on one line of a note into a named state. Entering a done-family state
// stamps a [done:date] token on the line; every transition is appended to the note's sidecar task
// log, and progress cookies on parent headings/list items are recomputed. The note is reindexed so
// `track tasks` reflects the change immediately.
func cmdTaskSet(args []string) int {
	fs := flag.NewFlagSet("task set", flag.ContinueOnError)
	id := fs.Int64("id", 0, "note id")
	title := fs.String("title", "", "note title (alternative to --id)")
	path := fs.String("path", "", "note path (alternative to --id)")
	line := fs.Int("line", 0, "1-based line number of the task")
	state := fs.String("state", "", "target state name (e.g. TODO, DOING, DONE)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	if *line <= 0 {
		return fail("--line is required and must be positive")
	}
	if strings.TrimSpace(*state) == "" {
		return fail("--state is required")
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

	tr, err := note.ApplyTaskState(cfg, notePath, *line, strings.TrimSpace(*state), time.Now())
	if err != nil {
		return fail("%v", err)
	}
	if err := index.New(cfg, s).One(notePath); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{
		"id":        noteID,
		"path":      notePath,
		"line":      tr.Line,
		"from":      tr.From,
		"state":     tr.To,
		"done":      tr.Done,
		"completed": tr.Completed,
		"changed":   tr.Changed,
		"text":      tr.Text,
	})
}

// cmdTasks lists indexed tasks as JSON, across the vault or scoped to one note, with state, deadline
// and priority filters. Dates in task tokens are plain YYYY-MM-DD, so the filters compare dates
// lexically regardless of the vault's display date format.
func cmdTasks(args []string) int {
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	id := fs.Int64("id", 0, "limit to one note by id")
	title := fs.String("title", "", "limit to one note by title")
	path := fs.String("path", "", "limit to one note by path")
	states := fs.String("state", "", "comma-separated state names to keep (e.g. TODO,DOING)")
	due := fs.String("due", "", "keep open tasks due on or before this date (YYYY-MM-DD)")
	overdue := fs.Bool("overdue", false, "keep open tasks whose deadline has passed")
	sortKey := fs.String("sort", "", "sort order: priority (default: note, line)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	switch *sortKey {
	case "", "priority":
	default:
		return fail("--sort must be priority")
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Self-heal before reading, like search/agenda: tasks may have been edited by another process.
	if _, err := index.New(cfg, s).RefreshIfStale(); err != nil {
		return fail("refresh index: %v", err)
	}

	filter := store.TaskFilter{ByPriority: *sortKey == "priority"}
	if *id != 0 || strings.TrimSpace(*title) != "" || strings.TrimSpace(*path) != "" {
		notePath, err := resolveNotePath(cfg, s, *id, strings.TrimSpace(*title), strings.TrimSpace(*path))
		if err != nil {
			return fail("%v", err)
		}
		noteID, err := note.IDFromPath(notePath)
		if err != nil {
			return fail("invalid note path: %v", err)
		}
		filter.NoteID = noteID
	}
	for _, st := range strings.Split(*states, ",") {
		st = strings.TrimSpace(st)
		if st == "" {
			continue
		}
		if _, ok := task.StateNamed(cfg.TaskStates, st); !ok {
			return fail("unknown task state %q", st)
		}
		filter.States = append(filter.States, st)
	}
	if *due != "" {
		filter.DueBy = *due
	}
	if *overdue {
		filter.OverdueBefore = time.Now().Format("2006-01-02")
	}

	rows, err := s.Tasks(filter)
	if err != nil {
		return fail("tasks: %v", err)
	}
	if rows == nil {
		rows = []store.TaskRow{}
	}
	for i := range rows {
		rows[i].Path = cfg.PathForKind(rows[i].FileKind, rows[i].NoteID)
	}
	return emit(map[string]any{"tasks": rows})
}
