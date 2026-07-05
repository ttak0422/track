// track-fetch-rss converts an RSS 2.0 / Atom feed into Canonical Data Model event JSONL — the first
// track-fetch-* tool (see docs/spec/fetch.md for the contract it implements). It is independent of
// the track CLI: data goes to stdout (or --out), diagnostics to stderr, and every record is validated
// against the event kind before anything is written.
//
// Usage:
//
//	track-fetch-rss --url <feed URL or file path> [--out <file>] [--entity <s>] [--timeout <dur>]
//
// With --out the JSONL is written to the file (conventionally the vault's data/ directory, which the
// web workspace watches for live re-renders) and a JSON summary is printed to stdout, matching the
// track CLI's result style.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/fetch/rss"
	"github.com/ttak0422/track/internal/track/dataset"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-rss", flag.ContinueOnError)
	fs.SetOutput(stderr)
	url := fs.String("url", "", "feed URL (http/https), or a local file path for testing")
	out := fs.String("out", "", "write JSONL to this file instead of stdout (prints a JSON summary)")
	entity := fs.String("entity", "", "entity value stamped on every event (defaults to the feed title)")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" {
		fmt.Fprintln(stderr, "track-fetch-rss: --url is required")
		fs.Usage()
		return 2
	}

	body, err := open(*url, *timeout)
	if err != nil {
		return fail(stderr, err)
	}
	defer body.Close()

	feed, skipped, err := rss.Parse(body)
	if err != nil {
		return fail(stderr, err)
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "track-fetch-rss: skipped %d entr%s missing a title or a parsable date\n",
			skipped, plural(skipped, "y", "ies"))
	}
	ent := *entity
	if ent == "" {
		ent = feed.Title
	}
	records, err := rss.Events(feed.Items, ent)
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

	if *out == "" {
		fmt.Fprint(stdout, jsonl.String())
		return 0
	}
	if err := os.WriteFile(*out, []byte(jsonl.String()), 0o644); err != nil {
		return fail(stderr, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"path": *out, "kind": string(dataset.KindEvent), "records": len(records), "skipped": skipped,
	})
	fmt.Fprintln(stdout, string(summary))
	return 0
}

// open returns the feed body: an HTTP response for a URL, a file otherwise (which keeps the tool
// testable and lets saved feeds be replayed).
func open(source string, timeout time.Duration) (io.ReadCloser, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return os.Open(source)
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "track-fetch-rss/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: HTTP %s", source, resp.Status)
	}
	return resp.Body, nil
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "track-fetch-rss: %v\n", err)
	return 1
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
