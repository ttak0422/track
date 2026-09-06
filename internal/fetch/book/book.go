// Package book looks books up by ISBN or title (Google Books, falling back to
// Open Library) and renders a reading-note Markdown body — the engine behind the
// track-fetch-book tool (see docs/spec/fetch.md). Following the track-fetch-*
// contract, it is a pure function of its inputs and a network call: the vault is
// never touched, so lookups are deterministic given the same API response, and the
// cover is linked rather than stored (the caller imports it with `track asset
// import`; the tool can download it into a caller-chosen directory).
//
// The package owns its HTTP access — an SSRF-guarded client like the engine's
// link-preview fetcher — and nothing else of the engine, keeping the tool
// independent of the track CLI.
package book

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// decodeJSON unmarshals an API body. A trailing newline (APIs are sloppy) is
// tolerated; anything else is an error.
func decodeJSON(body []byte, v any) error {
	return json.Unmarshal(body, v)
}

// writeFile writes data into dir under name, creating dir as needed. It is the
// only filesystem side effect of a lookup, and it is confined to a directory the
// caller chose (the vault is never touched).
func writeFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cover dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return fmt.Errorf("write cover: %w", err)
	}
	return nil
}

// Book is a normalized book record merged from a lookup: the canonical fields a
// reading note wants, with zero values when the source does not provide them.
type Book struct {
	Title         string   // main title
	Subtitle      string   // subtitle, when the edition has one
	Authors       []string // author names, in display order
	CoverURL      string   // best available cover image URL
	PublishedYear int      // publication year; 0 when unknown
	PageCount     int      // page count; 0 when unknown
	ISBN          string   // normalized digits of the queried edition's ISBN
	Publisher     string   // publisher name
	Language      string   // language code, e.g. "en", "ja"
	Description   string   // synopsis/blurb, plain text
	Source        string   // which API answered: "googlebooks" | "openlibrary"
	SourceURL     string   // canonical page for the edition
}

// isbnRe strips anything but digits; a valid ISBN is 10 or 13 digits.
var isbnRe = regexp.MustCompile(`\D`)

// NormalizeISBN reduces a user-supplied ISBN (hyphens, spaces, ISBN-10) to bare
// digits for API queries, and rejects strings that cannot be an ISBN.
func NormalizeISBN(s string) (string, error) {
	digits := isbnRe.ReplaceAllString(strings.TrimSpace(s), "")
	if len(digits) != 10 && len(digits) != 13 {
		return "", fmt.Errorf("not an ISBN (got %d digits): %q", len(digits), s)
	}
	return digits, nil
}

// ISBN10to13 converts a bare 10-digit ISBN to its 13-digit form using the EAN-13
// check digit, so callers can canonicalize search results regardless of which form
// the API returned.
func ISBN10to13(isbn10 string) string {
	if len(isbn10) != 10 {
		return isbn10
	}
	digits := "978" + isbn10[:9]
	sum := 0
	for i, r := range digits {
		w := 1
		if i%2 == 1 {
			w = 3
		}
		sum += int(r-'0') * w
	}
	return digits + fmt.Sprintf("%d", (10-sum%10)%10)
}

// year parses a publication year out of the various formats APIs publish — "2008",
// "2008-07-14", "July 2008", "2008 (Reprint)". It returns 0 when no year is found.
func year(s string) int {
	m := regexp.MustCompile(`\d{4}`).FindString(s)
	if m == "" {
		return 0
	}
	var y int
	fmt.Sscanf(m, "%d", &y)
	return y
}

// yearOf returns the publication year for NoteBody's display.
func (b Book) yearOf() int {
	return b.PublishedYear
}

// NoteBody renders the lookup as a ready-to-pipe Markdown reading-note body for
// `track new --title`: the title as the H1, the cover image (a remote URL, or an
// `assets/<file>` reference when the caller downloaded it into the vault's assets
// directory), a metadata list of whatever the lookup found, and empty Notes /
// Quotes sections to write into. Unknown fields are omitted rather than stubbed.
//
//	coverRef is either a remote URL (no download) or "assets/<file>" (the caller
//	passed --cover-dir <vault>/assets).
func NoteBody(b Book, coverRef string, fetched time.Time) string {
	var blocks []string
	var meta []string
	blocks = append(blocks, "# "+b.Title)

	if coverRef != "" {
		blocks = append(blocks, fmt.Sprintf("![cover](%s)", coverRef))
	}
	if len(b.Authors) > 0 {
		meta = append(meta, "- Author: "+strings.Join(b.Authors, ", "))
	}
	if b.Publisher != "" {
		meta = append(meta, "- Publisher: "+b.Publisher)
	}
	if b.PublishedYear > 0 {
		meta = append(meta, fmt.Sprintf("- Published: %d", b.PublishedYear))
	}
	if b.PageCount > 0 {
		meta = append(meta, fmt.Sprintf("- Pages: %d", b.PageCount))
	}
	if b.ISBN != "" {
		meta = append(meta, "- ISBN: "+b.ISBN)
	}
	if b.SourceURL != "" {
		meta = append(meta, fmt.Sprintf("- Source: [%s](%s)", b.Source, b.SourceURL))
	}
	if len(meta) > 0 {
		blocks = append(blocks, strings.Join(meta, "\n"))
	}
	if b.Description != "" {
		blocks = append(blocks, "## About\n\n"+b.Description)
	}
	blocks = append(blocks, "## Notes\n\n## Quotes\n")
	return strings.Join(blocks, "\n\n") + "\n"
}
