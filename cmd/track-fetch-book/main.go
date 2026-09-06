// track-fetch-book looks a book up by ISBN or title and prints a reading-note
// Markdown body — a track-fetch-* tool (see docs/spec/fetch.md). Google Books is
// the primary source, Open Library the fallback (which also answers when Google is
// rate-limited); the vault is never touched. The cover is linked, not stored: the
// body embeds the remote cover URL, or — with --cover-dir pointing at a directory
// like the vault's assets/ — the downloaded file referenced as assets/<file>, ready
// for `track asset import` and the cover-image linkage.
//
// Usage:
//
//	track-fetch-book --isbn <isbn> [--cover-dir <dir>] [--out <file>] [--timeout <dur>]
//	track-fetch-book --title <title> [--index N] [--cover-dir <dir>] [--out <file>]
//
// The note body goes to stdout by default, so it pipes straight into track:
//
//	track-fetch-book --isbn 9780132350884 | track new --title "Clean Code"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/fetch/book"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-book", flag.ContinueOnError)
	fs.SetOutput(stderr)
	isbn := fs.String("isbn", "", "look up this ISBN (10 or 13 digits, hyphens allowed)")
	title := fs.String("title", "", "search for this title; picks the best match (--index to choose)")
	index := fs.Int("index", 0, "with --title, which candidate to render (see the list on stderr)")
	coverDir := fs.String("cover-dir", "", "download the cover into this directory and reference it as assets/<file> (e.g. the vault's assets/)")
	out := fs.String("out", "", "write the note body to this file instead of stdout (prints a JSON summary)")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*isbn == "") == (*title == "") {
		fmt.Fprintln(stderr, "track-fetch-book: exactly one of --isbn or --title is required")
		fs.Usage()
		return 2
	}
	if *index < 0 {
		fmt.Fprintln(stderr, "track-fetch-book: --index must be >= 0")
		return 2
	}

	ctx := context.Background()
	client := book.NewClient(*timeout)

	b, err := resolve(ctx, client, *isbn, *title, *index, stderr)
	if err != nil {
		return fail(stderr, err)
	}

	coverRef := b.CoverURL
	if *coverDir != "" {
		ref, err := client.FetchCover(ctx, *coverDir, b)
		if err != nil {
			fmt.Fprintf(stderr, "track-fetch-book: cover download failed (%v); embedding the remote URL\n", err)
		} else if ref != "" {
			coverRef = ref
		}
	}

	body := book.NoteBody(b, coverRef, time.Now())

	if *out == "" {
		fmt.Fprint(stdout, body)
		return 0
	}
	if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
		return fail(stderr, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"path": *out, "title": b.Title, "authors": b.Authors,
		"isbn": b.ISBN, "cover": coverRef, "source": b.Source,
	})
	fmt.Fprintln(stdout, string(summary))
	return 0
}

// resolve runs the ISBN lookup or the title search and returns the book to render.
// For a title search the candidates are listed on stderr ([N] …) so the caller can
// re-run with --index; the empty/not-found cases are errors.
func resolve(ctx context.Context, client *book.Client, isbn, title string, index int, stderr io.Writer) (book.Book, error) {
	if isbn != "" {
		b, err := client.LookupISBN(ctx, isbn)
		if err != nil {
			return book.Book{}, err
		}
		if b.Title == "" {
			return book.Book{}, fmt.Errorf("no edition found for ISBN %s", isbn)
		}
		return b, nil
	}
	candidates, err := client.SearchTitle(ctx, title)
	if err != nil {
		return book.Book{}, err
	}
	if len(candidates) == 0 {
		return book.Book{}, fmt.Errorf("no results for title %q", title)
	}
	for i, c := range candidates {
		meta := strings.TrimSpace(strings.Join([]string{
			joinAuthors(c.Authors), yearS(c.PublishedYear), pagesS(c.PageCount),
		}, " · "))
		fmt.Fprintf(stderr, "[%d] %s%s\n", i, c.Title, metaSuffix(meta))
	}
	if index >= len(candidates) {
		return book.Book{}, fmt.Errorf("--index %d out of range (found %d candidates)", index, len(candidates))
	}
	return candidates[index], nil
}

func joinAuthors(a []string) string { return strings.Join(a, ", ") }

func yearS(y int) string {
	if y > 0 {
		return fmt.Sprintf("%d", y)
	}
	return ""
}

func pagesS(p int) string {
	if p > 0 {
		return fmt.Sprintf("%d pages", p)
	}
	return ""
}

func metaSuffix(meta string) string {
	if meta == "" {
		return ""
	}
	return " — " + meta
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "track-fetch-book: %v\n", err)
	return 1
}
