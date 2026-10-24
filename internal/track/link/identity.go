package link

import (
	"strconv"
	"strings"
)

const IDFormat = "id-v1"

// ExplicitID recognizes the reserved id: target namespace only in an opted-in
// source vault. A malformed explicit target remains explicit (with id == 0), so
// callers must never fall back to resolving it as a title. Bare numbers are titles.
func ExplicitID(key, format string) (id int64, explicit bool) {
	if format != IDFormat || !strings.HasPrefix(key, "id:") {
		return 0, false
	}
	raw := strings.TrimPrefix(key, "id:")
	if raw == "" || raw[0] == '0' {
		return 0, true
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, true
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, true
	}
	return n, true
}

// IDKey formats a vault-local note identity. It is not a URL or a global ID.
func IDKey(id int64) string { return "id:" + strconv.FormatInt(id, 10) }

// NoteKey is a resolution key without an optional anchor or display alias.
func NoteKey(format string, id int64, title string) string {
	if format == IDFormat {
		return IDKey(id)
	}
	return title
}

// NoteTarget is the inner text of a generated wiki link. Titles are presentation
// only in id-v1; a label that could change the wiki grammar is safely omitted.
func NoteTarget(format string, id int64, title string) string {
	return AnchoredNoteTarget(format, id, title, "")
}

// AnchoredNoteTarget puts the heading or block suffix before the display alias.
func AnchoredNoteTarget(format string, id int64, title, anchor string) string {
	if format != IDFormat {
		return title + anchor
	}
	key := IDKey(id) + anchor
	if title != "" && !strings.ContainsAny(title, "[]|\r\n") {
		return key + "|" + title
	}
	return key
}
