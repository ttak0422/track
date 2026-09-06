// Package kindle converts a Kindle "My Clippings.txt" export into Canonical Data Model event
// records — the engine behind the track-fetch-kindle tool (see docs/spec/fetch.md). It is a pure
// function of its input: the vault is never touched, so parsing, dedup, ordering, and note
// rendering are deterministic and independently testable.
//
// The parser mirrors track-fetch-rss's shape (internal/fetch/rss): Parse normalizes the source,
// Events validates every record against the event kind before anything is written, and diagnostics
// (skipped/malformed blocks) are counted for the caller to report on stderr.
package kindle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// separator is the record delimiter Amazon writes between clippings. A block is everything between
// two separators (the trailing separator of the last block is optional).
const separator = "=========="

// Clipping is one normalized clipping: the book it came from, the kind of clipping, the location
// span, the text, the time Amazon recorded, and the deterministic anchor derived from all of it.
type Clipping struct {
	Book     string    // book title (entity), author suffix stripped
	Type     string    // normalized: "highlight" | "note" | "bookmark"
	Location string    // "234-235" or "123" as written in the file
	Text     string    // the clipped text, paragraphs preserved
	Time     time.Time // "Added on" timestamp; the file carries no zone, so UTC
	Anchor   string    // dedupe key: "h" + sha256(book∅type∅location∅text) hex, first 11 chars
}

// Anchor returns the deterministic dedupe key of a clipping. The key joins book, type, location,
// and text with a NUL byte (the ∅ separator in the fetch-kindle spec — NUL can never appear in
// real text) and keeps the first 11 hex chars of the SHA-256, prefixed with "h". Identical content
// always produces the identical anchor, so a `--note` regeneration reproduces the same ^block ids
// and existing [[Book#^id]] links keep resolving (ADR 0038).
func anchor(book, typ, loc, text string) string {
	sum := sha256.Sum256([]byte(book + "\x00" + typ + "\x00" + loc + "\x00" + text))
	return "h" + hex.EncodeToString(sum[:])[:11]
}

// Parse reads a "My Clippings.txt" document into normalized clippings. The reader is normalized in
// place: the UTF-8 BOM (if present) is stripped and CRLF/LF/CR line endings are unified, so the
// same clippings parse identically no matter how the file was exported or transferred. Blocks that
// cannot yield a clipping (missing header, unrecognizable date, empty text) are dropped and
// counted; the second return value is that skip count. Clippings with identical anchors
// (re-exported or hand-duplicated blocks) collapse to their first occurrence — the third return
// value counts those.
func Parse(r io.Reader) ([]Clipping, int, int, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, 0, err
	}
	s := strings.TrimPrefix(string(raw), "\ufeff") // UTF-8 BOM
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var clips []Clipping
	skipped := 0
	for _, block := range strings.Split(s, separator) {
		block = strings.TrimSpace(block)
		if block == "" {
			continue // the file's trailing separator yields an empty segment, not a malformed block
		}
		c, ok := parseBlock(block)
		if !ok {
			skipped++
			continue
		}
		clips = append(clips, c)
	}

	// Dedupe by anchor, keeping first occurrence in file order. The anchor is content-derived, so
	// merging exports or regenerating notes can never double a highlight.
	seen := make(map[string]bool, len(clips))
	dupes := 0
	uniq := clips[:0]
	for _, c := range clips {
		if seen[c.Anchor] {
			dupes++
			continue
		}
		seen[c.Anchor] = true
		uniq = append(uniq, c)
	}
	return uniq, skipped, dupes, nil
}

// parseBlock turns one separator-delimited block into a Clipping. A block looks like:
//
//	Book Title (Author)                          <- lines[0]
//	- Your Highlight at location 234-235 | Added on Tuesday, January 13, 2015 10:33:28 AM
//	                                             <- lines[2] (blank separator)
//	the highlighted text, possibly multi-line    <- lines[3:]
//
// (English and Japanese annotation forms are recognized; other locales fall back to a generic
// "highlight" type and fail only if no location or date can be found.)
func parseBlock(block string) (Clipping, bool) {
	block = strings.TrimSpace(block)
	if block == "" {
		return Clipping{}, false
	}
	lines := strings.Split(block, "\n")
	if len(lines) < 3 {
		return Clipping{}, false
	}
	book := bookTitle(strings.TrimSpace(lines[0]))
	ann := strings.TrimSpace(lines[1])
	text := strings.TrimSpace(strings.Join(lines[2:], "\n"))
	if book == "" || text == "" {
		return Clipping{}, false
	}
	typ := clipType(ann)
	loc := clipLocation(ann)
	added, ok := clipTime(ann)
	if !ok {
		return Clipping{}, false
	}
	return Clipping{
		Book:     book,
		Type:     typ,
		Location: loc,
		Text:     text,
		Time:     added,
		Anchor:   anchor(book, typ, loc, text),
	}, true
}

// bookTitle strips the trailing " (Author...)" group Amazon appends to the title line, so the
// entity is the book itself (the natural [[note]] key) rather than the display line. Both ASCII
// parens (English exports) and full-width parens (Japanese exports) are handled.
func bookTitle(line string) string {
	if strings.HasSuffix(line, ")") {
		if i := strings.LastIndex(line, " ("); i > 0 {
			return strings.TrimSpace(line[:i])
		}
	}
	if strings.HasSuffix(line, "）") {
		if i := strings.LastIndex(line, "（"); i > 0 {
			return strings.TrimSpace(line[:i])
		}
	}
	return strings.TrimSpace(line)
}

// clipType maps the annotation line to a normalized type token. The token is part of the dedupe
// key, so it must be locale-independent and stable; unknown annotations default to "highlight"
// rather than being dropped, since the type is informational beyond the anchor.
func clipType(ann string) string {
	low := strings.ToLower(ann)
	switch {
	case strings.Contains(low, "bookmark"), strings.Contains(ann, "しおり"):
		return "bookmark"
	case strings.Contains(low, "note"), strings.Contains(ann, "メモ"), strings.Contains(ann, "ノート"):
		return "note"
	default: // "highlight", "ハイライト", and anything unrecognized
		return "highlight"
	}
}

// locRe matches the location span that Amazon writes as "location 234-235", "位置 234-235", or
// "loc. 12" (case-insensitive). The capture keeps the full span as written.
var locRe = regexp.MustCompile(`(?i)(?:location|位置|loc\.?)\s*(\d+(?:-\d+)?)`)

// clipLocation extracts the location span from the annotation line, "" when the line names no
// location (such blocks still parse; an empty location simply participates in the anchor).
func clipLocation(ann string) string {
	if m := locRe.FindStringSubmatch(ann); m != nil {
		return m[1]
	}
	return ""
}

// enTimeLayout is Amazon's English "Added on ..." timestamp: weekday, month name, day, year,
// time with AM/PM. The layout carries no zone, so time.Parse returns UTC.
const enTimeLayout = "Monday, January 2, 2006 3:04:05 PM"

// jaDateRe matches the Japanese "追加日: ..." timestamp. The weekday after the day (日曜日 / (日))
// varies and is irrelevant to the instant, so it is skipped with \S*.
var jaDateRe = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日\S*\s+(\d{1,2}):(\d{2}):(\d{2})`)

// clipTime extracts the "added on" instant from the annotation line. English ("Added on ") is
// parsed with time.Parse; Japanese ("追加日:"/"追加日：") is assembled from its date parts. The
// source carries no timezone, so the instant is UTC — machine-independent and still correctly
// ordered, since every clipping shares the same interpretation.
func clipTime(ann string) (time.Time, bool) {
	if i := strings.Index(ann, "Added on "); i >= 0 {
		t, err := time.Parse(enTimeLayout, strings.TrimSpace(ann[i+len("Added on "):]))
		if err == nil {
			return t, true
		}
	}
	for _, marker := range []string{"追加日:", "追加日："} {
		if i := strings.Index(ann, marker); i >= 0 {
			if t, ok := parseJaDate(ann[i+len(marker):]); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func parseJaDate(s string) (time.Time, bool) {
	m := jaDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	var parts [6]int
	for i := 1; i <= 6; i++ {
		fmt.Sscanf(m[i], "%d", &parts[i-1])
	}
	if parts[0] < 1 || parts[1] < 1 || parts[1] > 12 || parts[2] < 1 || parts[2] > 31 ||
		parts[3] > 23 || parts[4] > 59 || parts[5] > 59 {
		return time.Time{}, false
	}
	return time.Date(parts[0], time.Month(parts[1]), parts[2], parts[3], parts[4], parts[5], 0, time.UTC), true
}

// sorted returns clips ordered by time ascending; ties break on book, anchor, and text so equal
// timestamps still yield a fully deterministic stream (the contract requires ascending order).
func sorted(clips []Clipping) []Clipping {
	out := make([]Clipping, len(clips))
	copy(out, clips)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.Before(out[j].Time)
		}
		if out[i].Book != out[j].Book {
			return out[i].Book < out[j].Book
		}
		if out[i].Anchor != out[j].Anchor {
			return out[i].Anchor < out[j].Anchor
		}
		return out[i].Text < out[j].Text
	})
	return out
}

// Events maps clippings onto canonical event records (docs/spec/fetch.md): title is the clipped
// text, entity is the book, time is the added-on instant normalized to RFC 3339 UTC, ordered
// ascending. The clipping metadata rides along as extra fields — type, location, and anchor — so
// consumers can key on the same stable id the --note anchors use. Every record is validated
// against the event kind before the slice is returned.
func Events(clips []Clipping) ([]dataset.Record, error) {
	ordered := sorted(clips)
	records := make([]dataset.Record, 0, len(ordered))
	for _, c := range ordered {
		rec := dataset.Record{
			"version": dataset.SchemaVersion,
			"time":    c.Time.Format(time.RFC3339),
			"title":   c.Text,
			"entity":  c.Book,
			"type":    c.Type,
			"anchor":  c.Anchor,
		}
		if c.Location != "" {
			rec["location"] = c.Location
		}
		records = append(records, rec)
	}
	if err := dataset.ValidateRecords(dataset.KindEvent, records); err != nil {
		return nil, err
	}
	return records, nil
}

// NoteBody renders clippings as a ready-to-pipe Markdown note body for `track new --title`: an
// `up::` property pointing at the book (ADR 0038), a provenance line, then one list item per
// clipping carrying its deterministic ^anchor. Because the anchor is content-derived, re-running
// the tool on the same export reproduces the same ids, so [[Book#^id]] quotes written once keep
// resolving after regeneration. The anchor sits on the item line and paragraph breaks become
// two-space-indented continuation lines, so the block extent (item line + more-indented
// continuation lines, ADR 0038) covers a multi-paragraph quote as one block.
func NoteBody(clips []Clipping, book string, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "up:: [[%s]]\n\n", book)
	fmt.Fprintf(&b, "Clipped %s\n\n", now.Format("2006-01-02"))
	for _, c := range sorted(clips) {
		text := paraBreakRe.ReplaceAllString(c.Text, "\n")
		lines := strings.Split(text, "\n")
		b.WriteString("- ")
		b.WriteString(lines[0])
		b.WriteString(" ^")
		b.WriteString(c.Anchor)
		for _, ln := range lines[1:] {
			b.WriteString("\n  ")
			b.WriteString(ln)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// paraBreakRe matches a blank line (which in Markdown would end a list item) so NoteBody can turn
// it into a soft continuation instead.
var paraBreakRe = regexp.MustCompile(`\n[ \t]*\n`)
