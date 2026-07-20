// track-fetch-web clips a web page into Canonical Data Model event JSONL — a track-fetch-* tool
// (see docs/spec/fetch.md for the contract). It fetches the page, extracts the readable main
// content with a compact readability heuristic, and emits one event record carrying the title,
// source URL, timestamp, the content converted to Markdown, and the lead image. It is independent
// of the track CLI: data goes to stdout (or --out), diagnostics to stderr, and the record is
// validated against the event kind before anything is written.
//
// Usage:
//
//	track-fetch-web [--url] <page URL or file path> [--out <file>] [--note] [--timeout <dur>]
//
// With --note the tool prints a ready-to-pipe Markdown note body instead of JSONL, so a page clips
// straight into a note:
//
//	track-fetch-web --note https://example.com/essay | track new --title "An essay"
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/ttak0422/track/internal/fetch/web"
	"github.com/ttak0422/track/internal/track/dataset"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("track-fetch-web", flag.ContinueOnError)
	fs.SetOutput(stderr)
	urlFlag := fs.String("url", "", "page URL (http/https), or a local file path for testing; a bare argument works too")
	out := fs.String("out", "", "write JSONL to this file instead of stdout (prints a JSON summary)")
	note := fs.Bool("note", false, "print a ready-to-pipe Markdown note body instead of JSONL")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	source := strings.TrimSpace(*urlFlag)
	if source == "" && fs.NArg() == 1 {
		source = strings.TrimSpace(fs.Arg(0))
	}
	if source == "" || fs.NArg() > 1 {
		fmt.Fprintln(stderr, "track-fetch-web: exactly one page URL (or --url) is required")
		fs.Usage()
		return 2
	}

	body, pageURL, err := open(source, *timeout)
	if err != nil {
		return fail(stderr, err)
	}
	defer body.Close()

	page, err := web.Extract(body, pageURL)
	if err != nil {
		return fail(stderr, err)
	}
	if page.Markdown == "" {
		fmt.Fprintln(stderr, "track-fetch-web: no readable content found; emitting metadata only")
	}
	sourceURL := ""
	if pageURL != nil {
		sourceURL = source
	}
	now := time.Now()

	if *note {
		fmt.Fprint(stdout, web.NoteBody(page, sourceURL, now))
		return 0
	}

	rec, err := web.Record(page, sourceURL, now)
	if err != nil {
		return fail(stderr, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fail(stderr, err)
	}
	jsonl := string(line) + "\n"

	if *out == "" {
		fmt.Fprint(stdout, jsonl)
		return 0
	}
	if err := os.WriteFile(*out, []byte(jsonl), 0o644); err != nil {
		return fail(stderr, err)
	}
	summary, _ := json.Marshal(map[string]any{
		"path": *out, "kind": string(dataset.KindEvent), "records": 1, "title": rec["title"],
	})
	fmt.Fprintln(stdout, string(summary))
	return 0
}

// open returns the page body and its URL: an HTTP response for a URL (with the base for resolving
// relative links), a file otherwise (which keeps the tool testable and lets saved pages be
// replayed; file input has no base, so relative references are dropped).
func open(source string, timeout time.Duration) (io.ReadCloser, *url.URL, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		f, err := os.Open(source)
		return f, nil, err
	}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, nil, err
	}
	// A realistic UA and HTML Accept header keep sites from serving an empty or bot page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; track/0.1; +https://github.com/ttak0422/track)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := newGuardedClient(timeout).Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("fetch %s: HTTP %s", source, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("fetch %s: unsupported content type %q (expected HTML)", source, ct)
	}
	// resp.Request.URL is the final URL after redirects; relative links resolve against it.
	return resp.Body, resp.Request.URL, nil
}

// newGuardedClient is the SSRF-guarded HTTP client, mirroring the engine's web-workspace OGP
// fetcher (internal/track/webui): the dial control sees the resolved ip:port, so it catches both
// direct private targets and DNS names (including redirect hops) that resolve to private
// addresses. Fetch tools stay independent of the engine (docs/spec/fetch.md), hence the local
// copy. Pages on the local network can still be clipped by saving them to a file first.
func newGuardedClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("unresolved address %q", address)
			}
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
				ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
				return fmt.Errorf("refusing to fetch non-public address %s", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "track-fetch-web: %v\n", err)
	return 1
}
