// Package task models task lines in note bodies: Markdown checkbox items whose box character carries a
// named state (Obsidian-style custom checkboxes), plus inline bracket tokens for priority ([#A]),
// scheduled/deadline dates ([sched:YYYY-MM-DD] / [due:YYYY-MM-DD]) and the auto-written completion
// stamp ([done:YYYY-MM-DD]). It also recomputes progress cookies ([2/5] or [40%]) on parent headings
// and parent list items. The package is pure string manipulation so every surface (CLI, web server,
// static export, indexer) shares one parser and one mutation path.
package task

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// dateLayout is the fixed calendar-date layout of task tokens. It is intentionally independent of the
// vault's configurable date_format so task lines stay portable across vaults.
const dateLayout = "2006-01-02"

// State is one named task state. Char is the single character written inside the checkbox brackets;
// Done marks the state as completion-family (entering it stamps a [done:...] token on the line).
type State struct {
	Name string `json:"name"`
	Char string `json:"char"`
	Done bool   `json:"done"`
}

// states is the task state set, fixed for every vault. It is not configurable: the markers are single
// characters, so two vaults that spelled them differently would give the same pasted line two
// meanings, and every surface that renders a checkbox — the CLI, the web workspace's Markdown
// renderer and task board, the static export, the Neovim conceal rules — has to know the set to draw
// it. A fixed set is what lets those agree without shipping the vocabulary between them.
var states = []State{
	{Name: "TODO", Char: " "},
	{Name: "DOING", Char: "/"},
	{Name: "WAITING", Char: "?"},
	{Name: "DONE", Char: "x", Done: true},
	{Name: "CANCELLED", Char: "-", Done: true},
}

// States returns the task state set in board order (not-done first, then the done family). The slice
// is a copy, so a caller sorting or filtering it cannot reorder everyone else's states.
func States() []State {
	return slices.Clone(states)
}

// Task is one parsed task line.
type Task struct {
	Line      int    `json:"line"`
	State     string `json:"state"`
	Done      bool   `json:"done"`
	Priority  string `json:"priority,omitempty"`
	Scheduled string `json:"scheduled,omitempty"`
	Due       string `json:"due,omitempty"`
	Completed string `json:"completed,omitempty"`
	Text      string `json:"text"`
	Indent    int    `json:"-"`
}

// Set is the wire shape shared by the live /api/tasks endpoint and the static-site bundle: the note's
// task items. The state set is not carried with them — it is fixed (see States), so the client holds
// its own copy of the same five states rather than being told them once per note.
type Set struct {
	Items []Task `json:"items"`
}

// NewSet parses body into the shared wire shape. Items is never nil so it marshals as [].
func NewSet(body string) Set {
	items := Parse(body)
	if items == nil {
		items = []Task{}
	}
	return Set{Items: items}
}

// LogEntry is one recorded state transition in a note's sidecar task log.
type LogEntry struct {
	At   string `yaml:"at" json:"at"`
	Line int    `yaml:"line" json:"line"`
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	Text string `yaml:"text,omitempty" json:"text,omitempty"`
}

// Transition reports a SetState mutation.
type Transition struct {
	Line      int    `json:"line"`
	From      string `json:"from"`
	To        string `json:"to"`
	Done      bool   `json:"done"`
	Completed string `json:"completed,omitempty"`
	Text      string `json:"text"`
	Changed   bool   `json:"changed"`
}

var (
	// lineRE matches a task list item: indent, list marker (-, *, +, or "1."), the [c] box with a single
	// state character, and the rest (empty or starting with whitespace, per GFM). Groups: 1 indent,
	// 2 marker, 3 state char, 4 rest.
	lineRE = regexp.MustCompile(`^(\s*)((?:[-*+]|\d+\.)\s+)\[(.)\](\s.*|)$`)

	priorityRE  = regexp.MustCompile(`\[#([A-Za-z])\]`)
	schedRE     = regexp.MustCompile(`\[sched:(\d{4}-\d{2}-\d{2})\]`)
	dueRE       = regexp.MustCompile(`\[due:(\d{4}-\d{2}-\d{2})\]`)
	completedRE = regexp.MustCompile(`\[done:(\d{4}-\d{2}-\d{2})\]`)
	// completedTokenRE also eats the spacing before the stamp, so removing it leaves no gap.
	completedTokenRE = regexp.MustCompile(`\s*\[done:\d{4}-\d{2}-\d{2}\]`)

	cookieRE  = regexp.MustCompile(`\[\d+/\d+\]|\[\d+%\]`)
	headingRE = regexp.MustCompile(`^(#{1,6})\s`)
	listRE    = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+\.)\s`)
)

// Parse returns every task line in body, in order. Line numbers are 1-based over body's lines.
// Lines inside fenced code blocks are skipped: notation shown as a code example is not a task.
func Parse(body string) []Task {
	lines := strings.Split(body, "\n")
	mask := fenced(lines)
	var out []Task
	for i, line := range lines {
		if mask[i] {
			continue
		}
		t, ok := parseLine(line)
		if !ok {
			continue
		}
		t.Line = i + 1
		out = append(out, t)
	}
	return out
}

// At returns the task on the given 1-based line of body, if that line is a task.
func At(body string, line int) (Task, bool) {
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if line < 1 || line > len(lines) {
		return Task{}, false
	}
	if fenced(lines)[line-1] {
		return Task{}, false
	}
	t, ok := parseLine(lines[line-1])
	if !ok {
		return Task{}, false
	}
	t.Line = line
	return t, true
}

// fenced reports which lines sit inside a fenced code block (delimiters included), mirroring the
// static export's code-mask fence rules, so a task line quoted as a code example never parses as a
// real task, counts toward a progress cookie, or accepts a state change.
// ponytail: fences only; a 4-space indented code block still parses — tighten if it ever bites.
func fenced(lines []string) []bool {
	mask := make([]bool, len(lines))
	inFence := false
	var fenceChar byte
	var fenceLen int
	for i, line := range lines {
		if inFence {
			mask[i] = true
			if c, l, rest, ok := fenceInfo(line); ok && c == fenceChar && l >= fenceLen && strings.TrimSpace(line[rest:]) == "" {
				inFence = false
			}
			continue
		}
		if c, l, _, ok := fenceInfo(line); ok {
			mask[i] = true
			inFence = true
			fenceChar, fenceLen = c, l
		}
	}
	return mask
}

// fenceInfo reports whether line opens or closes a code fence: a run of at least three "`" or "~"
// after up to three leading spaces. It returns the fence character, the run length, and the offset
// just past the run (where a closing fence must hold only trailing whitespace).
func fenceInfo(line string) (char byte, length, rest int, ok bool) {
	k := 0
	for k < len(line) && k < 3 && line[k] == ' ' {
		k++
	}
	if k >= len(line) || (line[k] != '`' && line[k] != '~') {
		return 0, 0, 0, false
	}
	c := line[k]
	start := k
	for k < len(line) && line[k] == c {
		k++
	}
	if k-start < 3 {
		return 0, 0, 0, false
	}
	return c, k - start, k, true
}

func parseLine(line string) (Task, bool) {
	m := lineRE.FindStringSubmatch(line)
	if m == nil {
		return Task{}, false
	}
	st, ok := stateForChar(m[3])
	if !ok {
		return Task{}, false
	}
	rest := m[4]
	t := Task{State: st.Name, Done: st.Done, Indent: len(m[1])}
	if pm := priorityRE.FindStringSubmatch(rest); pm != nil {
		t.Priority = strings.ToUpper(pm[1])
	}
	if sm := schedRE.FindStringSubmatch(rest); sm != nil {
		t.Scheduled = sm[1]
	}
	if dm := dueRE.FindStringSubmatch(rest); dm != nil {
		t.Due = dm[1]
	}
	if cm := completedRE.FindStringSubmatch(rest); cm != nil {
		t.Completed = cm[1]
	}
	t.Text = displayText(rest)
	return t, true
}

// displayText strips the metadata tokens (priority, dates, completion stamp, progress cookie) and
// collapses whitespace, leaving the human text of the task for listings and board cards.
func displayText(rest string) string {
	for _, re := range []*regexp.Regexp{priorityRE, schedRE, dueRE, completedRE, cookieRE} {
		rest = re.ReplaceAllString(rest, " ")
	}
	return strings.Join(strings.Fields(rest), " ")
}

func stateForChar(char string) (State, bool) {
	for _, st := range states {
		if st.Char == char || strings.EqualFold(st.Char, char) {
			return st, true
		}
	}
	return State{}, false
}

// StateNamed resolves a state by name, case-insensitively.
func StateNamed(name string) (State, bool) {
	for _, st := range states {
		if strings.EqualFold(st.Name, name) {
			return st, true
		}
	}
	return State{}, false
}

// FirstStates returns the first non-done and the first done-family state of the set, the pair the
// legacy check/uncheck toggle maps onto.
func FirstStates() (todo State, done State) {
	foundTodo, foundDone := false, false
	for _, st := range states {
		if st.Done && !foundDone {
			done, foundDone = st, true
		}
		if !st.Done && !foundTodo {
			todo, foundTodo = st, true
		}
	}
	return todo, done
}

// SetState rewrites the task on the given 1-based line of body to the named target state. Entering a
// done-family state from a not-done one stamps a [done:date] token on the line; leaving the done
// family removes it. Progress cookies on parent headings/list items are recomputed over the whole
// body. The returned body preserves the presence of a trailing newline.
func SetState(body string, line int, target string, now time.Time) (string, Transition, error) {
	to, ok := StateNamed(target)
	if !ok {
		names := make([]string, len(states))
		for i, st := range states {
			names[i] = st.Name
		}
		return "", Transition{}, fmt.Errorf("unknown task state %q (states: %s)", target, strings.Join(names, ", "))
	}

	trailingNewline := strings.HasSuffix(body, "\n")
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if line < 1 || line > len(lines) {
		return "", Transition{}, fmt.Errorf("line %d is out of range (note has %d lines)", line, len(lines))
	}
	if fenced(lines)[line-1] {
		return "", Transition{}, fmt.Errorf("line %d is inside a code fence, not a task line", line)
	}
	m := lineRE.FindStringSubmatch(lines[line-1])
	if m == nil {
		return "", Transition{}, fmt.Errorf("line %d is not a task line: %q", line, lines[line-1])
	}
	from, ok := stateForChar(m[3])
	if !ok {
		return "", Transition{}, fmt.Errorf("line %d has an unknown task marker [%s]", line, m[3])
	}

	rest := m[4]
	if from.Name != to.Name {
		if to.Done && !from.Done {
			rest = completedTokenRE.ReplaceAllString(rest, "")
			rest = strings.TrimRight(rest, " \t") + " [done:" + now.Format(dateLayout) + "]"
		}
		if !to.Done && from.Done {
			rest = strings.TrimRight(completedTokenRE.ReplaceAllString(rest, ""), " \t")
		}
	}
	lines[line-1] = m[1] + m[2] + "[" + to.Char + "]" + rest
	recomputeCookies(lines)

	updated := strings.Join(lines, "\n")
	if trailingNewline {
		updated += "\n"
	}
	t, _ := parseLine(lines[line-1])
	return updated, Transition{
		Line:      line,
		From:      from.Name,
		To:        to.Name,
		Done:      to.Done,
		Completed: t.Completed,
		Text:      t.Text,
		Changed:   from.Name != to.Name,
	}, nil
}

// recomputeCookies rewrites every progress cookie ([n/m] or [p%]) in lines from the current task
// states. A cookie on a heading counts the tasks until the next heading of the same or a shallower
// level; a cookie on a list item counts the deeper-indented tasks until a sibling item or heading.
// It reports whether any line changed.
func recomputeCookies(lines []string) bool {
	changed := false
	mask := fenced(lines)
	for i, line := range lines {
		if mask[i] || !cookieRE.MatchString(line) {
			continue
		}
		var done, total int
		if hm := headingRE.FindStringSubmatch(line); hm != nil {
			level := len(hm[1])
			for j := i + 1; j < len(lines); j++ {
				if mask[j] {
					continue
				}
				if hm2 := headingRE.FindStringSubmatch(lines[j]); hm2 != nil && len(hm2[1]) <= level {
					break
				}
				countTask(lines[j], &done, &total)
			}
		} else if lm := listRE.FindStringSubmatch(line); lm != nil {
			indent := len(lm[1])
			// ponytail: the scan skips non-list text instead of modeling Markdown list termination;
			// tighten to real list blocks if stray deep-indented lists ever miscount.
			for j := i + 1; j < len(lines); j++ {
				if mask[j] {
					continue
				}
				if headingRE.MatchString(lines[j]) {
					break
				}
				if lm2 := listRE.FindStringSubmatch(lines[j]); lm2 != nil && len(lm2[1]) <= indent {
					break
				}
				countTask(lines[j], &done, &total)
			}
		} else {
			continue
		}
		if next := replaceCookie(line, done, total); next != line {
			lines[i] = next
			changed = true
		}
	}
	return changed
}

func countTask(line string, done, total *int) {
	t, ok := parseLine(line)
	if !ok {
		return
	}
	*total++
	if t.Done {
		*done++
	}
}

// replaceCookie rewrites the first cookie on line, keeping its form (fraction or percent).
func replaceCookie(line string, done, total int) string {
	replaced := false
	return cookieRE.ReplaceAllStringFunc(line, func(match string) string {
		if replaced {
			return match
		}
		replaced = true
		if strings.Contains(match, "/") {
			return fmt.Sprintf("[%d/%d]", done, total)
		}
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		return fmt.Sprintf("[%d%%]", pct)
	})
}
