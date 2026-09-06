// track-fetch-kindle converts a Kindle "My Clippings.txt" export into Canonical Data Model event
// JSONL — a track-fetch-* tool (see docs/spec/fetch.md for the contract). It parses the clippings
// file (BOM/CRLF-normalized, split on ==========, English and Japanese annotation lines), dedupes
// by a content-derived anchor, and emits one event record per clipping: title = the clipped text,
// entity = the book, time = the added-on instant (RFC 3339, ascending). It is independent of the
// track CLI: data goes to stdout (or --out), diagnostics to stderr, and every record is validated
// against the event kind before anything is written.
//
// Usage:
//
//	track-fetch-kindle <clippings.txt> [--out <file>] [--note] [--book <title>]
//
// With --note the tool prints a ready-to-pipe Markdown note body instead of JSONL: an `up::`
// property pointing at the book, then one list item per clipping carrying a deterministic
// ^h… anchor, so a book of highlights clips straight into a note and [[Book#^id]] quotes stay
// alive across regeneration:
//
//	track-fetch-kindle --note --book "The Pragmatic Programmer" "My Clippings.txt" \
//	  | track new --title "The Pragmatic Programmer — highlights"
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/fetch/kindle"
	"github.com/ttak0422/track/internal/track/dataset"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-kindle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write JSONL to this file instead of stdout (prints a JSON summary)")
	note := fs.Bool("note", false, "print a ready-to-pipe Markdown note body instead of JSONL")
	book := fs.String("book", "", "restrict to this book title; with --note, the note's up:: parent (required when the file holds several books)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "track-fetch-kindle: exactly one My Clippings.txt path is required")
		fs.Usage()
		return 2
	}
	path := fs.Arg(0)

	f, err := os.Open(path)
	if err != nil {
		return fail(stderr, err)
	}
	clips, skipped, dupes, err := kindle.Parse(f)
	f.Close()
	if err != nil {
		return fail(stderr, err)
	}
	if skipped > 0 || dupes > 0 {
		fmt.Fprintf(stderr, "track-fetch-kindle: skipped %d malformed block%s, deduplicated %d\n",
			skipped, plural(skipped, "", "s"), dupes)
	}

	if *note {
		return runNote(stdout, stderr, clips, *book)
	}
	return runJSONL(stdout, stderr, clips, *out)
}

// runNote renders the clippings as a note body. The note is one book's highlights — a single up::
// parent — so the file (after the optional --book filter) must hold exactly one book title.
func runNote(stdout, stderr io.Writer, clips []kindle.Clipping, book string) int {
	clips = filterBook(clips, book)
	books := map[string]bool{}
	for _, c := range clips {
		books[c.Book] = true
	}
	if len(books) != 1 {
		fmt.Fprintf(stderr, "track-fetch-kindle: --note needs exactly one book (found %d); pass --book <title>\n", len(books))
		return 2
	}
	title := ""
	for b := range books {
		title = b
	}
	fmt.Fprint(stdout, kindle.NoteBody(clips, title, time.Now()))
	return 0
}

func runJSONL(stdout, stderr io.Writer, clips []kindle.Clipping, out string) int {
	records, err := kindle.Events(clips)
	if err != nil {
		return fail(stderr, err)
	}
	var jsonl strings.Builder
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			return fail(stderr, err)
		}
		jsonl.Write(line)
		jsonl.WriteByte('\n')
	}

	if out == "" {
		fmt.Fprint(stdout, jsonl.String())
		return 0
	}
	if err := os.WriteFile(out, []byte(jsonl.String()), 0o644); err != nil {
		return fail(stderr, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"path": out, "kind": string(dataset.KindEvent), "records": len(records),
		"skipped": len(clips) - len(records),
	})
	fmt.Fprintln(stdout, string(summary))
	return 0
}

// filterBook restricts clippings to one book title (exact match); an empty filter keeps everything.
func filterBook(clips []kindle.Clipping, book string) []kindle.Clipping {
	book = strings.TrimSpace(book)
	if book == "" {
		return clips
	}
	out := clips[:0]
	for _, c := range clips {
		if c.Book == book {
			out = append(out, c)
		}
	}
	return out
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "track-fetch-kindle: %v\n", err)
	return 1
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
