// Package journal owns opening and creating date-addressed journal notes: writing the journal file and
// its sidecar and rolling up the month/year summary journals. It is deliberately template-agnostic — the
// body to write on creation is supplied by the caller — and index-agnostic — it reports which paths need
// (re)indexing via Result.Reindex rather than indexing them itself. That keeps it free of an import cycle
// with the indexer, so the CLI, the indexer, and the web server can all reuse the same journal mechanics.
package journal

import (
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

// Options configures a journal open/create.
type Options struct {
	// CreateBody produces the journal body when the journal is created. It is called only on creation, so
	// resolving a template need not happen when an existing journal is reopened. A nil CreateBody (or one
	// returning an empty string) creates an empty journal.
	CreateBody func(name string, id int64, day time.Time) (string, error)
}

// Result describes the opened or created journal. Reindex lists the journal/summary paths that were
// created or modified and so need (re)indexing by the caller; it is empty when nothing changed.
type Result struct {
	NoteID  int64
	Path    string
	Name    string // yyyyMMdd, the journal's date-addressed name and title
	Date    string // YYYY-MM-DD
	Created bool
	Reindex []string
}

// Open opens or creates the journal for day. On creation it writes the body from opts.CreateBody and the
// sidecar. Whether created or reopened, it self-heals the month and year summary journals that link the
// day. It indexes nothing; the caller indexes Result.Reindex. day is normalized to the local start of day.
func Open(cfg *config.Config, day time.Time, opts Options) (Result, error) {
	day = startOfDay(day)
	name := day.Format(cfg.JournalDateFormat)
	noteID, err := note.IDFromName(name)
	if err != nil {
		return Result{}, err
	}
	date := day.Format(cfg.DateFormat)
	path := cfg.JournalPath(name)

	var reindex []string
	created := false
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
			return Result{}, err
		}
		body := ""
		if opts.CreateBody != nil {
			b, err := opts.CreateBody(name, noteID, day)
			if err != nil {
				return Result{}, err
			}
			body = ensureTrailingNewline(b)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return Result{}, err
		}
		if err := note.WriteMetadata(
			cfg.MetadataPath(noteID),
			note.Metadata{Title: name, Tags: []string{"journal"}, Created: date},
		); err != nil {
			return Result{}, err
		}
		reindex = append(reindex, path)
		created = true
	} else if statErr != nil {
		return Result{}, statErr
	}

	// Roll the day up into month and year summary notes: journal/<yyyyMM>.md links each day, and
	// journal/<yyyy>.md links each month. Both are idempotent, so reopening a journal self-heals the
	// summaries without duplicating entries.
	month := day.Format("200601")
	year := day.Format("2006")
	if p, err := ensureSummary(cfg, month, name, date, "journal-month"); err != nil {
		return Result{}, err
	} else if p != "" {
		reindex = append(reindex, p)
	}
	if p, err := ensureSummary(cfg, year, month, date, "journal-year"); err != nil {
		return Result{}, err
	} else if p != "" {
		reindex = append(reindex, p)
	}

	return Result{NoteID: noteID, Path: path, Name: name, Date: date, Created: created, Reindex: reindex}, nil
}

// ensureSummary makes sure the summary journal note named `name` exists and lists `childTerm` as a
// `[[childTerm]]` bullet. It is idempotent: an existing link is left untouched. It returns the summary
// path when the file changed (and so needs reindexing), or "" when nothing changed.
func ensureSummary(cfg *config.Config, name, childTerm, date, kindTag string) (string, error) {
	noteID, err := note.IDFromName(name)
	if err != nil {
		return "", err
	}
	path := cfg.JournalPath(name)

	body := ""
	exists := true
	if raw, err := os.ReadFile(path); err == nil {
		body = string(raw)
	} else if os.IsNotExist(err) {
		exists = false
	} else {
		return "", err
	}

	link := "[[" + childTerm + "]]"
	changed := false
	if !exists {
		if err := os.MkdirAll(cfg.JournalDir(), 0o755); err != nil {
			return "", err
		}
		changed = true
	}
	if !strings.Contains(body, link) {
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += "- " + link + "\n"
		changed = true
	}
	if !changed {
		return "", nil
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	if !exists {
		if err := note.WriteMetadata(
			cfg.MetadataPath(noteID),
			note.Metadata{Title: name, Tags: []string{"journal", kindTag}, Created: date},
		); err != nil {
			return "", err
		}
	}
	return path, nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func ensureTrailingNewline(body string) string {
	if body == "" || strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}
