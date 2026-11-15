package task

import "strings"

// Timeline is a read-only projection of one note and its durable sidecar history.
// Lines identify locations in this snapshot, not stable task identities across edits.
type Timeline struct {
	SchemaVersion int            `json:"schema_version"`
	NoteID        int64          `json:"note_id"`
	Title         string         `json:"title"`
	Items         []TimelineItem `json:"items"`
	Progress      Progress       `json:"progress"`
	History       []LogEntry     `json:"history"`
}

// TimelineItem relates a checkbox to its nearest enclosing checkbox in the same list.
type TimelineItem struct {
	Task
	ParentLine int `json:"parent_line,omitempty"`
	Depth      int `json:"depth"`
}

// Progress counts explicit checkboxes, not estimates or time spent. Cancelled tasks
// are separate from completed work; parent checkboxes are counted like any other task.
type Progress struct {
	Total     int `json:"total"`
	Done      int `json:"done"`
	Cancelled int `json:"cancelled"`
	Open      int `json:"open"`
}

// NewTimeline never mutates tasks or history. History remains in recorded order,
// including entries for deleted/moved tasks; historical line numbers cannot safely
// be joined to the current tree. Unindented content ends a nested list context.
func NewTimeline(id int64, title, body string, history []LogEntry) Timeline {
	result := Timeline{SchemaVersion: 1, NoteID: id, Title: title,
		Items: []TimelineItem{}, History: append([]LogEntry{}, history...)}
	lines := strings.Split(body, "\n")
	stack := []TimelineItem{}
	previousLine := 0
	for _, item := range Parse(body) {
		for _, between := range lines[previousLine : item.Line-1] {
			if strings.TrimSpace(between) == "" {
				continue
			}
			indent := len(between) - len(strings.TrimLeft(between, " \t"))
			for len(stack) > 0 && stack[len(stack)-1].Indent >= indent {
				stack = stack[:len(stack)-1]
			}
		}
		for len(stack) > 0 && stack[len(stack)-1].Indent >= item.Indent {
			stack = stack[:len(stack)-1]
		}
		node := TimelineItem{Task: item, Depth: len(stack)}
		if len(stack) > 0 {
			node.ParentLine = stack[len(stack)-1].Line
		}
		result.Items = append(result.Items, node)
		stack = append(stack, node)
		previousLine = item.Line
		result.Progress.Total++
		switch item.State {
		case "DONE":
			result.Progress.Done++
		case "CANCELLED":
			result.Progress.Cancelled++
		default:
			result.Progress.Open++
		}
	}
	return result
}
