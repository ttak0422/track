package timeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$`)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
func validID(s string) bool { return identifier.MatchString(s) }
func validText(s string, max int, required bool) bool {
	return len(s) <= max && (!required || strings.TrimSpace(s) != "") && !strings.ContainsRune(s, 0)
}
func mode(s string) (string, error) {
	if s == "" {
		return "serial", nil
	}
	if s != "serial" && s != "parallel" {
		return "", invalid("mode must be serial or parallel")
	}
	return s, nil
}
func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 1e9 }
func decode(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return invalid("payload: %v", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("payload must contain one JSON value")
	}
	return nil
}
func canonical(raw []byte) (json.RawMessage, error) {
	if len(raw) > 64<<10 {
		return nil, ErrLimit
	}
	// Preserve integer identifiers exactly. Decoding into float64 would round
	// note_id values above 2^53 and could collapse distinct idempotency payloads.
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, invalid("payload JSON: %v", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, invalid("payload must contain one JSON value")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, invalid("payload must be an object")
	}
	out, err := json.Marshal(value)
	return out, err
}
func empty(id string) Snapshot {
	return Snapshot{SchemaVersion: SchemaVersion, ProjectID: id, Tasks: []Task{}, Reports: []Report{}, Discussions: []Discussion{}, Controls: []Control{}, Events: []Event{}}
}
func taskIndex(s *Snapshot, id string) int {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			return i
		}
	}
	return -1
}
func reportExists(s *Snapshot, id string) bool {
	for _, r := range s.Reports {
		if r.ID == id {
			return true
		}
	}
	return false
}

func reduce(s *Snapshot, e Event) error {
	if e.Sequence != s.Revision+1 || !validID(e.Source) || !validID(e.EventID) {
		return invalid("event sequence or source identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, e.At); err != nil {
		return invalid("event timestamp")
	}
	if s.Revision == 0 && e.Kind != "project.create" {
		return ErrNotFound
	}
	switch e.Kind {
	case "project.create", "project.update":
		if e.Kind == "project.create" && s.Revision != 0 {
			return invalid("project already exists")
		}
		var p Project
		if err := decode(e.Payload, &p); err != nil {
			return err
		}
		if !validText(p.Title, 512, true) || !validText(p.Description, 16384, false) || p.NoteID < 0 {
			return invalid("project fields")
		}
		var err error
		p.Mode, err = mode(p.Mode)
		if err != nil {
			return err
		}
		s.Project = p
	case "task.upsert":
		var t Task
		if err := decode(e.Payload, &t); err != nil {
			return err
		}
		if !validID(t.ID) || (t.ParentID != "" && !validID(t.ParentID)) || !validText(t.Title, 512, true) || !validText(t.Detail, 16384, false) || (t.Owner != "" && !validID(t.Owner)) {
			return invalid("task fields")
		}
		switch t.Status {
		case "pending", "active", "paused", "blocked", "done", "retired":
		default:
			return invalid("task status")
		}
		if t.Priority != nil && (*t.Priority < 0 || *t.Priority > 3) {
			return invalid("priority must be between 0 and 3")
		}
		if !finite(t.Weight) || !finite(t.DoneMinutes) || !finite(t.RemainingMinutes) {
			return invalid("effort/weight must be finite and nonnegative")
		}
		if t.Weight == 0 {
			t.Weight = 1
		}
		var err error
		t.Mode, err = mode(t.Mode)
		if err != nil {
			return err
		}
		if t.ParentID != "" && taskIndex(s, t.ParentID) < 0 {
			return invalid("unknown parent task")
		}
		at := taskIndex(s, t.ID)
		if at < 0 {
			if len(s.Tasks) >= MaxTasks {
				return ErrLimit
			}
			s.Tasks = append(s.Tasks, t)
		} else {
			s.Tasks[at] = t
		}
		// A full ancestor walk also rejects self-parenting and cycles introduced by moving a subtree.
		seen := map[string]bool{t.ID: true}
		parent := t.ParentID
		for parent != "" {
			if seen[parent] {
				return invalid("task parent cycle")
			}
			seen[parent] = true
			i := taskIndex(s, parent)
			if i < 0 {
				return invalid("unknown parent task")
			}
			parent = s.Tasks[i].ParentID
		}
	case "report.add":
		var r Report
		if err := decode(e.Payload, &r); err != nil {
			return err
		}
		if !validID(r.ID) || reportExists(s, r.ID) || !validText(r.Author, 256, true) || !validText(r.Summary, 16384, true) || !validText(r.Uncertainty, 8192, false) || !validText(r.UserAction, 8192, false) {
			return invalid("report fields or duplicate id")
		}
		if r.TaskID != "" && taskIndex(s, r.TaskID) < 0 {
			return invalid("unknown report task")
		}
		start, a := time.Parse(time.RFC3339, r.PeriodStart)
		end, b := time.Parse(time.RFC3339, r.PeriodEnd)
		if a != nil || b != nil || end.Before(start) {
			return invalid("report period")
		}
		if len(r.EventSequences) == 0 || len(r.EventSequences) > MaxEvents {
			return invalid("report requires event provenance")
		}
		seen := map[uint64]bool{}
		for _, seq := range r.EventSequences {
			if seq == 0 || seq > s.Revision || seen[seq] {
				return invalid("unknown or duplicate report event")
			}
			seen[seq] = true
		}
		s.Reports = append(s.Reports, r)
	case "discussion.add":
		var d Discussion
		if err := decode(e.Payload, &d); err != nil {
			return err
		}
		if !validID(d.ID) || !validText(d.Author, 256, true) || !validText(d.Text, 16384, true) {
			return invalid("discussion fields")
		}
		for _, old := range s.Discussions {
			if old.ID == d.ID {
				return invalid("duplicate discussion id")
			}
		}
		switch d.TargetType {
		case "project":
			if d.TargetID != s.ProjectID {
				return invalid("unknown discussion project")
			}
		case "task":
			if taskIndex(s, d.TargetID) < 0 {
				return invalid("unknown discussion task")
			}
		case "report":
			if !reportExists(s, d.TargetID) {
				return invalid("unknown discussion report")
			}
		default:
			return invalid("discussion target type")
		}
		s.Discussions = append(s.Discussions, d)
	case "control.request":
		var c Control
		if err := decode(e.Payload, &c); err != nil {
			return err
		}
		at := taskIndex(s, c.TaskID)
		if !validID(c.ID) || at < 0 || !validID(c.Owner) || c.Owner != s.Tasks[at].Owner || (c.Action != "pause" && c.Action != "resume") || (c.Status != "" && c.Status != "requested") || c.Result != "" {
			return invalid("control request fields or task owner")
		}
		for _, old := range s.Controls {
			if old.ID == c.ID {
				return invalid("duplicate control id")
			}
		}
		c.Status = "requested"
		s.Controls = append(s.Controls, c)
	case "control.update":
		var update ControlUpdate
		if err := decode(e.Payload, &update); err != nil {
			return err
		}
		if !validText(update.Result, 8192, false) {
			return invalid("control result")
		}
		at := -1
		for i, c := range s.Controls {
			if c.ID == update.ID {
				at = i
				break
			}
		}
		if at < 0 {
			return invalid("unknown control")
		}
		c := &s.Controls[at]
		if e.Source != c.Owner {
			return invalid("control update source must match the routed owner")
		}
		allowed := (c.Status == "requested" && (update.Status == "acknowledged" || update.Status == "rejected" || update.Status == "failed")) || (c.Status == "acknowledged" && (update.Status == "completed" || update.Status == "failed"))
		if !allowed {
			return invalid("invalid control transition")
		}
		c.Status = update.Status
		c.Result = update.Result
	default:
		return invalid("unknown event kind %q", e.Kind)
	}
	s.Revision = e.Sequence
	s.Events = append(s.Events, e)
	s.Progress, s.TaskProgress = projectProgress(s)
	return nil
}

// projectProgress computes each subtree once. Parent effort is not counted again.
// Serial groups sum remaining estimates; parallel groups use the longest child
// path. Retired roots have zero completion and effort, but retain subtree counts.
// Descendants retain their own projections even beneath a retired ancestor; that
// ancestor excludes them from its own and the project's completion and effort.
// Total/Done/Retired count explicit task records, including excluded descendants.
func projectProgress(s *Snapshot) (Progress, map[string]Progress) {
	children := make(map[string][]Task, len(s.Tasks))
	for _, task := range s.Tasks {
		children[task.ParentID] = append(children[task.ParentID], task)
	}
	memo := make(map[string]Progress, len(s.Tasks))
	var subtree func(Task) Progress
	var group func(string, string) Progress
	group = func(parent, groupMode string) Progress {
		var out Progress
		var weight float64
		for _, task := range children[parent] {
			current := subtree(task)
			out.Total += current.Total
			out.Done += current.Done
			out.Retired += current.Retired
			if task.Status == "retired" {
				continue
			}
			out.Fraction += current.Fraction * task.Weight
			weight += task.Weight
			out.DoneMinutes += current.DoneMinutes
			if groupMode == "parallel" {
				out.RemainingMinutes = math.Max(out.RemainingMinutes, current.RemainingMinutes)
			} else {
				out.RemainingMinutes += current.RemainingMinutes
			}
		}
		if weight > 0 {
			out.Fraction /= weight
		}
		return out
	}
	subtree = func(task Task) Progress {
		if cached, ok := memo[task.ID]; ok {
			return cached
		}
		var out Progress
		if len(children[task.ID]) > 0 {
			out = group(task.ID, task.Mode)
		} else {
			out.DoneMinutes, out.RemainingMinutes = task.DoneMinutes, task.RemainingMinutes
			if task.Status == "done" {
				out.Fraction, out.RemainingMinutes = 1, 0
			} else if total := task.DoneMinutes + task.RemainingMinutes; total > 0 {
				out.Fraction = task.DoneMinutes / total
			}
		}
		out.Total++
		if task.Status == "done" {
			out.Done++
		}
		if task.Status == "retired" {
			out.Retired++
			out.Fraction, out.DoneMinutes, out.RemainingMinutes = 0, 0, 0
		}
		memo[task.ID] = out
		return out
	}
	return group("", s.Mode), memo
}
