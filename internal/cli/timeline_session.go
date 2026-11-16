package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/timeline"
)

var sessionLocalID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,47}$`)

func timelineRun(agent, session string) string {
	sum := sha256.Sum256([]byte(agent + "\x00" + session))
	return agent + "-" + hex.EncodeToString(sum[:16])
}

// Explicit binding only: never infer a vault/project from an incoming hook's cwd.
func cmdTimelineSession(args []string) int {
	if len(args) == 0 {
		return fail("usage: track timeline-session init|start|show|task|report|hook [flags]")
	}
	action := args[0]
	hook := action == "hook"
	if hook && os.Getenv("TRACK_TIMELINE_ENABLED") != "1" {
		return 0
	}
	fs := flag.NewFlagSet("timeline-session "+action, flag.ContinueOnError)
	project := fs.String("project", os.Getenv("TRACK_TIMELINE_PROJECT"), "explicit timeline project")
	agent := fs.String("agent", "", "claude or codex")
	session := fs.String("session", "", "stable session id; retain across resume/compaction")
	run := fs.String("run", "", "opaque run id returned by start/hook")
	title := fs.String("title", "", "project/task title")
	taskID := fs.String("task", "", "session-local task id")
	parent := fs.String("parent", "", "session-local parent task id")
	status := fs.String("status", "", "pending, active, paused, blocked, done, retired")
	detail := fs.String("detail", "", "explicit non-sensitive task detail")
	done := fs.Float64("done", 0, "estimated minutes spent")
	remaining := fs.Float64("remaining", 0, "estimated minutes remaining")
	weight := fs.Float64("weight", 1, "relative task weight")
	event := fs.String("event", "", "stable report id for retry")
	summary := fs.String("summary", "", "explicit authored report")
	uncertainty := fs.String("uncertainty", "", "unresolved issues")
	if code, ok := parseArgs(fs, args[1:]); !ok {
		return code
	}
	if fs.NArg() != 0 {
		return fail("unexpected positional arguments")
	}
	finish := func(err error) int {
		if hook {
			fmt.Fprintln(os.Stderr, "track timeline hook skipped: binding or record unavailable")
			return 0
		}
		return fail("timeline session: %v", err)
	}
	if strings.TrimSpace(os.Getenv("TRACK_VAULT")) == "" || *project == "" {
		return finish(errors.New("explicit TRACK_VAULT (or --vault) and --project are required"))
	}
	if hook {
		var input struct {
			Session string `json:"session_id"`
			Event   string `json:"hook_event_name"`
		}
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, (64<<10)+1))
		if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &input) != nil {
			return finish(errors.New("invalid hook input"))
		}
		if input.Event != "SessionStart" {
			return 0
		}
		*session = input.Session
		action = "start"
	}
	switch action {
	case "init", "start", "show", "task", "report":
	default:
		return finish(errors.New("unknown action"))
	}
	if action != "init" && action != "show" {
		if *run != "" {
			if !regexp.MustCompile(`^(claude|codex)-[0-9a-f]{32}$`).MatchString(*run) || *session != "" {
				return finish(errors.New("invalid --run or combined --session"))
			}
		} else {
			if (*agent != "claude" && *agent != "codex") || strings.TrimSpace(*session) == "" || len(*session) > 512 {
				return finish(errors.New("--agent claude|codex and --session are required"))
			}
			*run = timelineRun(*agent, *session)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	store := timeline.NewStore(os.Getenv("TRACK_VAULT"))
	snap, err := store.Read(ctx, *project)
	if err != nil && !(action == "init" && errors.Is(err, timeline.ErrNotFound)) {
		return finish(err)
	}
	var kind, eventID, source string
	var payload any
	source = *run
	switch action {
	case "init":
		if *title == "" {
			return finish(errors.New("--title is required"))
		}
		if err == nil {
			if snap.Title != *title {
				return finish(errors.New("project exists with another title"))
			}
			return emit(snap)
		}
		source = "session-cli"
		eventID = "create"
		kind = "project.create"
		payload = timeline.Project{Title: *title, Mode: "serial"}
	case "show":
		return emit(snap)
	case "start":
		eventID = "start"
		kind = "discussion.add"
		payload = timeline.Discussion{ID: *run, TargetType: "project", TargetID: *project, Author: *run, Text: "Session attached. Lifecycle does not imply task completion."}
	case "task":
		if !sessionLocalID.MatchString(*taskID) || (*parent != "" && !sessionLocalID.MatchString(*parent)) {
			return finish(errors.New("--task/--parent must be local ids, at most 48 ASCII characters"))
		}
		id := *run + "-" + *taskID
		task := timeline.Task{ID: id, Title: *title, Status: "pending", Owner: *run, Weight: 1, Mode: "serial"}
		for _, t := range snap.Tasks {
			if t.ID == id {
				if t.Owner != *run {
					return finish(errors.New("task owner changed; reconcile through timeline-project"))
				}
				task = t
			}
		}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "title":
				task.Title = *title
			case "status":
				task.Status = *status
			case "detail":
				task.Detail = *detail
			case "done":
				task.DoneMinutes = *done
			case "remaining":
				task.RemainingMinutes = *remaining
			case "weight":
				task.Weight = *weight
			case "parent":
				task.ParentID = ""
				if *parent != "" {
					task.ParentID = *run + "-" + *parent
				}
			}
		})
		if task.Weight == 0 {
			task.Weight = 1
		}
		for _, t := range snap.Tasks {
			if t.ID == id && reflect.DeepEqual(t, task) {
				return emit(snap)
			}
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return finish(err)
		}
		eventID = hex.EncodeToString(nonce[:])
		kind = "task.upsert"
		payload = task
	case "report":
		if !sessionLocalID.MatchString(*event) || strings.TrimSpace(*summary) == "" {
			return finish(errors.New("--event and --summary are required"))
		}
		id := *run + "-" + *event
		// Stable retry identity without regenerating provenance against a newer revision.
		for _, r := range snap.Reports {
			if r.ID == id {
				if r.Summary != *summary || r.Uncertainty != *uncertainty {
					return finish(timeline.ErrDuplicate)
				}
				return emit(snap)
			}
		}
		seq := []uint64{}
		start, end := "", ""
		for _, e := range snap.Events {
			if e.Source == *run {
				seq = append(seq, e.Sequence)
				if start == "" {
					start = e.At
				}
				end = e.At
			}
		}
		if len(seq) == 0 {
			return finish(errors.New("start the session before reporting"))
		}
		eventID = "report-" + *event
		kind = "report.add"
		payload = timeline.Report{ID: id, Author: *run, Summary: *summary, Uncertainty: *uncertainty, PeriodStart: start, PeriodEnd: end, EventSequences: seq}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return finish(err)
	}
	request := timeline.Request{Source: source, EventID: eventID, ExpectedRevision: snap.Revision, Kind: kind, Payload: raw}
	result, err := store.Apply(ctx, *project, request)
	// An attachment is immutable and independent of task state. Only this safe
	// append can be rebased automatically; task patches require reconciliation.
	for retry := 0; action == "start" && errors.Is(err, timeline.ErrConflict) && retry < 2; retry++ {
		latest, readErr := store.Read(ctx, *project)
		if readErr != nil {
			err = readErr
			break
		}
		request.ExpectedRevision = latest.Revision
		result, err = store.Apply(ctx, *project, request)
	}
	if err != nil {
		return finish(err)
	}
	if action == "start" {
		if hook {
			return emit(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart", "additionalContext": fmt.Sprintf("Track timeline run: %s. Use track-timeline skill with --run %s. Project binding is supplied by TRACK_TIMELINE_PROJECT and TRACK_VAULT. Read timeline-session show before updates. Never infer task completion from session lifecycle.", *run, *run)}})
		}
		return emit(map[string]any{"run": *run, "project_id": *project, "revision": result.Revision})
	}
	return emit(result)
}
