// Package calendarevent owns single calendar events independently of note activity and tasks.
package calendarevent

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Native bundles must not depend on the host's zoneinfo installation.
	"unicode"
	"unicode/utf8"
)

const (
	Version         = 1
	MaxRequestBytes = 64 << 10
	maxRecordBytes  = 128 << 10
)

var (
	ErrValidation = errors.New("invalid calendar event")
	ErrConflict   = errors.New("calendar event changed")
	ErrNotFound   = errors.New("calendar event not found")
	ErrCorrupt    = errors.New("corrupt calendar event")
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Fields is the complete editable event payload. End is always exclusive; dates are never instants.
type Fields struct {
	Title       string `json:"title"`
	AllDay      bool   `json:"all_day"`
	Start       string `json:"start"`
	End         string `json:"end"`
	TimeZone    string `json:"time_zone"`
	Description string `json:"description"`
	Location    string `json:"location"`
	URL         string `json:"url"`
	NoteID      string `json:"note_id"`
	Status      string `json:"status"`
	Deleted     bool   `json:"deleted"`
}

type Event struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Fields
	UpdatedAt string `json:"updated_at"`
}

type WriteRequest struct {
	Fields
	ExpectedRevision int64  `json:"expected_revision"`
	OperationID      string `json:"operation_id"`
}

func validID(id string) bool { return uuidPattern.MatchString(id) }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Normalize interprets wall times once at the write boundary and persists unambiguous timestamps.
func Normalize(f Fields) (Fields, error) {
	f.Title = strings.TrimSpace(f.Title)
	f.TimeZone = strings.TrimSpace(f.TimeZone)
	f.URL = strings.TrimSpace(f.URL)
	if f.Title == "" {
		return f, invalid("title is required")
	}
	for _, field := range []struct {
		name, value string
		max         int
		multiline   bool
	}{
		{"title", f.Title, 500, false}, {"description", f.Description, 32768, true},
		{"location", f.Location, 2048, false}, {"url", f.URL, 4096, false},
		{"time_zone", f.TimeZone, 128, false}, {"note_id", f.NoteID, 20, false},
	} {
		if !utf8.ValidString(field.value) || len(field.value) > field.max {
			return f, invalid("%s is invalid or too long (maximum %d bytes)", field.name, field.max)
		}
		for _, r := range field.value {
			if unicode.IsControl(r) && !(field.multiline && (r == '\n' || r == '\r' || r == '\t')) {
				return f, invalid("%s contains a control character", field.name)
			}
		}
	}
	if f.Status == "" {
		f.Status = "confirmed"
	}
	if f.Status != "confirmed" && f.Status != "cancelled" {
		return f, invalid("status must be confirmed or cancelled")
	}
	if f.NoteID != "" {
		id, err := strconv.ParseInt(f.NoteID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != f.NoteID {
			return f, invalid("note_id must be a positive local note ID")
		}
	}
	if f.URL != "" {
		u, err := url.Parse(f.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return f, invalid("url must be an http or https URL without credentials")
		}
	}
	if f.AllDay {
		if f.TimeZone != "" {
			return f, invalid("all-day events use civil dates and must not set time_zone")
		}
		start, err := time.Parse(time.DateOnly, f.Start)
		if err != nil || start.Year() < 1 || start.Format(time.DateOnly) != f.Start {
			return f, invalid("start must be a real YYYY-MM-DD date")
		}
		end, err := time.Parse(time.DateOnly, f.End)
		if err != nil || end.Year() < 1 || end.Format(time.DateOnly) != f.End {
			return f, invalid("end must be a real YYYY-MM-DD date")
		}
		if !end.After(start) {
			return f, invalid("end must be after start (the end date is exclusive)")
		}
		return f, nil
	}
	if f.TimeZone == "" || f.TimeZone == "Local" {
		return f, invalid("time_zone must be an explicit IANA time zone")
	}
	if !eventZones[f.TimeZone] {
		return f, invalid("unknown IANA time_zone %q (host-specific and placeholder zones are not supported)", f.TimeZone)
	}
	loc, err := time.LoadLocation(f.TimeZone)
	if err != nil {
		return f, invalid("unknown IANA time_zone %q", f.TimeZone)
	}
	start, err := parseTime(f.Start, loc)
	if err != nil {
		return f, invalid("start: %v", err)
	}
	end, err := parseTime(f.End, loc)
	if err != nil {
		return f, invalid("end: %v", err)
	}
	if !end.After(start) {
		return f, invalid("end must be after start")
	}
	f.Start, f.End = start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
	return f, nil
}

func parseTime(value string, loc *time.Location) (time.Time, error) {
	if explicit, err := time.Parse(time.RFC3339Nano, value); err == nil {
		local := explicit.In(loc)
		_, supplied := explicit.Zone()
		_, actual := local.Zone()
		if supplied != actual || local.Year() < 1 || local.Year() > 9999 {
			return time.Time{}, fmt.Errorf("the explicit UTC offset must match %s at that instant", loc)
		}
		return local, nil
	}
	var wall time.Time
	var err error
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		wall, err = time.Parse(layout, value)
		if err == nil {
			break
		}
	}
	if err != nil || wall.Year() < 1 {
		return time.Time{}, errors.New("use a local date and time or RFC3339 timestamp")
	}
	// Collect the zone offsets around the wall clock, then test their candidate instants. Unlike
	// time.Date in a location, this detects both repeated and nonexistent times, including day skips.
	offsets := map[int]bool{}
	limit := wall.Add(48 * time.Hour)
	for cursor := wall.Add(-48 * time.Hour); !cursor.After(limit); {
		local := cursor.In(loc)
		_, offset := local.Zone()
		offsets[offset] = true
		_, end := local.ZoneBounds()
		if end.IsZero() || end.After(limit) || !end.After(cursor) {
			break
		}
		cursor = end
	}
	var matches []time.Time
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second).In(loc)
		if candidate.Format("2006-01-02T15:04:05") == wall.Format("2006-01-02T15:04:05") {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 1:
		_, offset := matches[0].Zone()
		if offset%60 != 0 {
			return time.Time{}, errors.New("the historical time-zone offset cannot be represented by RFC3339")
		}
		return matches[0], nil
	case 0:
		return time.Time{}, fmt.Errorf("this local time does not exist in %s; choose another time", loc)
	default:
		return time.Time{}, fmt.Errorf("this local time is ambiguous in %s; include an explicit UTC offset", loc)
	}
}
