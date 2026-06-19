package cli

import (
	"flag"
	"os"
	"regexp"
	"strings"

	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
)

// checkboxLine matches a Markdown task list item: optional indent, a list marker
// (-, *, +, or an ordered "1."), the "[ ]"/"[x]" box, and the trailing text.
// Group 2 is the single state rune (a space when unchecked, x/X when checked).
var checkboxLine = regexp.MustCompile(`^(\s*(?:[-*+]|\d+\.)\s+\[)([ xX])(\].*)$`)

// cmdToggle flips a task-list checkbox on a single line of a note, so agents can
// update "- [ ]"/"- [x]" state without re-emitting the whole note body (where
// they tend to corrupt surrounding text). The target note is named by one of
// --id/--title/--path and the box by --line, the 1-based line number reported by
// `track search`/`track export`. --state forces a result instead of flipping,
// making check/uncheck idempotent. The note is reindexed so search stays current.
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

	raw, err := os.ReadFile(notePath)
	if err != nil {
		return fail("read note: %v", err)
	}
	// Line numbers from search/export are 1-based over the file as written; the
	// only metadata that ever lived inline is trailing legacy footmatter, so the
	// leading checkbox lines line up with the raw file either way.
	trailingNewline := strings.HasSuffix(string(raw), "\n")
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if *line > len(lines) {
		return fail("line %d is out of range (note has %d lines)", *line, len(lines))
	}

	target := lines[*line-1]
	m := checkboxLine.FindStringSubmatch(target)
	if m == nil {
		return fail("line %d is not a task checkbox: %q", *line, target)
	}
	checked := m[2] != " "
	next := checked
	switch want {
	case "toggle":
		next = !checked
	case "check":
		next = true
	case "uncheck":
		next = false
	}

	box := " "
	if next {
		box = "x"
	}
	lines[*line-1] = m[1] + box + m[3]
	updated := strings.Join(lines, "\n")
	if trailingNewline {
		updated += "\n"
	}
	if err := os.WriteFile(notePath, []byte(updated), 0o644); err != nil {
		return fail("write note: %v", err)
	}

	if err := index.New(cfg, s).One(notePath); err != nil {
		return fail("index note: %v", err)
	}
	return emit(map[string]any{
		"id":      noteID,
		"path":    notePath,
		"line":    *line,
		"checked": next,
		"changed": next != checked,
		"text":    strings.TrimSpace(lines[*line-1]),
	})
}
