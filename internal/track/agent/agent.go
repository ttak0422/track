// Package agent reads the state of running Claude Code sessions out of ~/.claude — the session
// records and transcripts Claude Code writes for itself — and answers two questions: which sessions
// are alive right now, and what did one session say most recently. Nothing is hooked or scraped:
// the records are the source, and track only adds its own liveness check (ADR 0072), because the
// sessions directory also holds dead and reused records.
//
// The package is deliberately independent of the vault machinery: agent state is not a vault asset,
// so every function here takes the home directory to look under and needs no config or index.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Session is one Claude Code session as recorded in ~/.claude/sessions/<pid>.json. The record is
// decoded loosely: json.Unmarshal ignores unknown fields, because Claude Code adds new ones between
// releases and a strict decode would turn a forward-compatible record into a dropped session.
//
// The JSON tags are the wire contract `track agent ls` promises: only WaitingFor is omitempty,
// because it is present just when status is "waiting".
type Session struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	StartedAt       int64  `json:"startedAt"`
	ProcStart       string `json:"procStart"`
	Version         string `json:"version"`
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	UpdatedAt       int64  `json:"updatedAt"`
	Status          string `json:"status"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	WaitingFor      string `json:"waitingFor,omitempty"`
}

// List returns the live Claude Code sessions under ~/.claude/sessions, alive-checked. A missing
// ~/.claude (or a missing sessions dir) is an empty list, not an error — a machine that never ran
// Claude Code is a perfectly ordinary "no sessions" answer. Dead records, reused pids, and
// non-session processes are dropped silently; only an unreadable sessions directory is an error.
func List(home string) ([]Session, error) {
	return listSessions(home, processProbe{ps: psProbe})
}

// listSessions is the testable core of List: the process-inspection half is the probe, so tests
// can run the whole liveness decision tree without spawning ps.
func listSessions(home string, probe processProbe) ([]Session, error) {
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Session{}, nil
		}
		return nil, err
	}
	sessions := []Session{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // an unreadable record (partial write, permissions) is not worth an error
		}
		var s Session
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if !alive(s, probe) {
			continue
		}
		sessions = append(sessions, s)
	}
	slices.SortFunc(sessions, func(a, b Session) int { return a.PID - b.PID })
	return sessions, nil
}

// processProbe is the one seam into the live process table, swapped in tests. ps reads a
// process's command line and start time with one `ps -o command=,lstart=` call, or returns an
// error when the process cannot be inspected.
type processProbe struct {
	ps func(pid int) (cmdline, lstart string, err error)
}

// psProbe is the real probe. TZ=UTC is set so lstart comes out in UTC — Claude Code writes
// procStart in UTC, and a raw comparison against the local-time default would be off by the local
// offset and drop every session (ADR 0072).
func psProbe(pid int) (cmdline, lstart string, err error) {
	cmd := exec.Command("ps", "-o", "command=,lstart=", "-p", strconv.Itoa(pid))
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TZ=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	line := strings.TrimRight(string(out), " \t\r\n")
	// lstart is the fixed-width tail of the line: `%a %b %e %H:%M:%S %Y` with a space-padded day,
	// always 24 characters. ps pads both columns with spaces, so the trailing padding is trimmed
	// first; the command line is everything before the lstart and may itself contain spaces.
	if len(line) < 24 {
		return "", "", fmt.Errorf("ps output too short for pid %d: %q", pid, line)
	}
	return strings.TrimSpace(line[:len(line)-24]), line[len(line)-24:], nil
}

// alive reports whether a session record's pid is a process running a Claude Code session right
// now. Every check has to pass; a record that fails any of them is dropped silently (never an
// error), which is what keeps dead sessions and reused pids out of the listing.
func alive(s Session, probe processProbe) bool {
	// 1. The pid must name a live process. kill(pid, 0) only probes existence: ESRCH is dead;
	// EPERM (exists, not ours) passes here and ps decides next.
	if err := syscall.Kill(s.PID, 0); err != nil {
		if !errors.Is(err, syscall.EPERM) {
			return false
		}
	}
	// 2+3. The process must be a session (not the daemon), and its start time must match the
	// record's. Both facts come from the same ps call, so a pid reused by non-session
	// infrastructure is caught by the command line and a reused pid running the same program is
	// caught by the start time.
	cmdline, lstart, err := probe.ps(s.PID)
	if err != nil {
		return false
	}
	if isNonSession(cmdline) {
		return false
	}
	if s.ProcStart != "" {
		return sameStart(s.ProcStart, lstart)
	}
	return true
}

// isNonSession reports whether a process command line is Claude Code infrastructure that can never
// run a session. The daemon is the only such process: this package iterates session records, not
// processes, so a `claude bg-spare` process that a record names IS the background session itself
// (kind=bg) and must be kept. Only the first program token and first argument are trusted: ps
// truncates the command column to 16 characters on macOS when it is combined with another column,
// so nothing beyond `claude daemon` would survive the cut anyway. A pid reused by other
// infrastructure is caught by the procStart↔lstart check in alive, not here.
func isNonSession(cmdline string) bool {
	fields := strings.Fields(cmdline)
	return len(fields) >= 2 && filepath.Base(fields[0]) == "claude" && fields[1] == "daemon"
}

// startLayout is the fixed layout of Claude Code's procStart and of ps lstart under TZ=UTC:
// `Mon Jan  2 15:04:05 2006`, with a space-padded day.
const startLayout = "Mon Jan 2 15:04:05 2006"

// sameStart reports whether procStart (the record's, written in UTC) and lstart (ps under TZ=UTC)
// name the same instant. An unparseable value is a mismatch: when the record carries a procStart it
// must be confirmable, and a pid whose process we cannot place is not a live session.
func sameStart(procStart, lstart string) bool {
	a, err := time.ParseInLocation(startLayout, procStart, time.UTC)
	if err != nil {
		return false
	}
	b, err := time.ParseInLocation(startLayout, lstart, time.UTC)
	if err != nil {
		return false
	}
	return a.Equal(b)
}

// PR is the pull request a session created, from the transcript's pr-link record.
type PR struct {
	Number     int    `json:"number"`
	URL        string `json:"url,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// Block is one content block of a user or assistant message. The three kinds track folds carry
// their payloads — text, tool_use name/input, tool_result content — and every other kind keeps
// only its type, so the conversation shape survives without dragging multi-megabyte image payloads
// across the wire.
type Block struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Name    string         `json:"name,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Content any            `json:"content,omitempty"`
}

// Message is one user or assistant entry of a session transcript, as the wire contract for
// `track agent log` promises.
type Message struct {
	Type       string  `json:"type"`
	UUID       string  `json:"uuid,omitempty"`
	ParentUUID string  `json:"parentUuid,omitempty"`
	Timestamp  string  `json:"timestamp,omitempty"`
	CWD        string  `json:"cwd,omitempty"`
	GitBranch  string  `json:"gitBranch,omitempty"`
	Message    []Block `json:"message"`
}

// Transcript is the answer of `track agent log <id>`: the tail of a session's transcript, its
// latest AI title, and the PR it created. AITitle and PR are empty when the backward scan did not
// reach them.
type Transcript struct {
	SessionID string    `json:"sessionId"`
	AITitle   string    `json:"aiTitle,omitempty"`
	PR        *PR       `json:"pr,omitempty"`
	Messages  []Message `json:"messages"`
}

// Log returns the tail of a session's transcript. The transcript is found by globbing
// ~/.claude/projects/*/<sessionID>.jsonl — the cwd-slug directory name is deliberately not
// reconstructed, because the slug rules are fiddly and a session lives in exactly one project
// directory. The file is read backward in chunks, never whole, so a multi-hundred-KB transcript
// costs only the read of its tail.
func Log(home, sessionID string, tail int) (Transcript, error) {
	if tail <= 0 {
		return Transcript{}, fmt.Errorf("tail must be positive, got %d", tail)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	if err != nil {
		return Transcript{}, err
	}
	if len(matches) == 0 {
		return Transcript{}, fmt.Errorf("no transcript found for session %s", sessionID)
	}
	messages, aiTitle, pr, err := scanTranscriptTail(matches[0], tail)
	if err != nil {
		return Transcript{}, err
	}
	return Transcript{SessionID: sessionID, AITitle: aiTitle, PR: pr, Messages: messages}, nil
}

// transcriptCollector gathers the tail scan's output: the last `need` user/assistant messages
// (newest first) plus the newest ai-title and pr-link lines it passed. add processes one line and
// reports whether the scan can stop.
type transcriptCollector struct {
	need     int
	messages []Message
	aiTitle  string
	pr       *PR
}

// transcriptRecord is the subset of a transcript line this package reads. Everything else —
// including entirely unknown types and fields — is ignored.
type transcriptRecord struct {
	Type         string             `json:"type"`
	UUID         string             `json:"uuid"`
	ParentUUID   string             `json:"parentUuid"`
	Timestamp    string             `json:"timestamp"`
	CWD          string             `json:"cwd"`
	GitBranch    string             `json:"gitBranch"`
	AITitle      string             `json:"aiTitle"`
	PRNumber     int                `json:"prNumber"`
	PRURL        string             `json:"prUrl"`
	PRRepository string             `json:"prRepository"`
	Message      *transcriptMessage `json:"message"`
}

type transcriptMessage struct {
	Content json.RawMessage `json:"content"`
}

// add decodes one transcript line and collects what it carries. The ai-title and pr-link set is
// the newest one seen so far, which — because the scan runs newest-first — is the latest in the
// file. add reports true once `need` user/assistant messages are collected; the scan then stops
// and older lines are never read.
func (c *transcriptCollector) add(line string) bool {
	var rec transcriptRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return false
	}
	switch rec.Type {
	case "user", "assistant":
		c.messages = append(c.messages, Message{
			Type:       rec.Type,
			UUID:       rec.UUID,
			ParentUUID: rec.ParentUUID,
			Timestamp:  rec.Timestamp,
			CWD:        rec.CWD,
			GitBranch:  rec.GitBranch,
			Message:    contentBlocks(rec.Message),
		})
		return len(c.messages) >= c.need
	case "ai-title":
		if c.aiTitle == "" {
			c.aiTitle = rec.AITitle
		}
	case "pr-link":
		if c.pr == nil {
			c.pr = &PR{Number: rec.PRNumber, URL: rec.PRURL, Repository: rec.PRRepository}
		}
	}
	return false
}

// contentBlocks turns a message's content (a block array, or a bare string for some user records)
// into the wire Block list.
func contentBlocks(msg *transcriptMessage) []Block {
	if msg == nil {
		return []Block{}
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(msg.Content, &arr); err == nil {
		blocks := make([]Block, 0, len(arr))
		for _, raw := range arr {
			blocks = append(blocks, parseBlock(raw))
		}
		return blocks
	}
	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		return []Block{{Type: "text", Text: text}}
	}
	return []Block{}
}

// parseBlock keeps a content block's type and the payload of the three kinds the wire contract
// names; any other kind (image, thinking, …) comes through as type-only.
func parseBlock(raw json.RawMessage) Block {
	var b struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Input   map[string]any  `json:"input"`
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(raw, &b)
	out := Block{Type: b.Type, Text: b.Text, Name: b.Name, Input: b.Input}
	if len(b.Content) > 0 {
		var v any
		if err := json.Unmarshal(b.Content, &v); err == nil {
			out.Content = v
		}
	}
	return out
}

// scanTranscriptTail reads a transcript jsonl file backward in chunks until `tail` user/assistant
// messages are collected (or the file start is reached), returning them chronologically together
// with the newest ai-title and pr-link found along the way. Unknown and unparseable lines are
// skipped.
func scanTranscriptTail(path string, tail int) ([]Message, string, *PR, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", nil, err
	}
	defer f.Close()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, "", nil, err
	}
	const chunkSize = 64 * 1024
	c := &transcriptCollector{need: tail}
	buf := ""
	pos := size
	for pos > 0 {
		n := int64(chunkSize)
		if n > pos {
			n = pos
		}
		pos -= n
		raw := make([]byte, int(n))
		if _, err := f.ReadAt(raw, pos); err != nil {
			return nil, "", nil, err
		}
		buf = string(raw) + buf
		for {
			i := strings.LastIndexByte(buf, '\n')
			if i < 0 {
				break // the rest of buf is one partial first line; the outer loop reads further back
			}
			line := buf[i+1:]
			buf = buf[:i]
			if line == "" {
				continue
			}
			if c.add(line) {
				messages := c.messages
				slices.Reverse(messages)
				return messages, c.aiTitle, c.pr, nil
			}
		}
	}
	if buf != "" {
		c.add(buf) // the first line, when the file does not end with a newline
	}
	messages := c.messages
	slices.Reverse(messages)
	return messages, c.aiTitle, c.pr, nil
}
