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
//	track-fetch-web --snapshot-dir <dir> <page URL> [--timeout <dur>]
//
// With --note the tool prints a ready-to-pipe Markdown note body instead of JSONL, so a page clips
// straight into a note:
//
//	track-fetch-web --note https://example.com/essay | track new --title "An essay"
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	return runWithHTTPClient(args, stdout, stderr, nil)
}

// runWithHTTPClient keeps the CLI testable with an in-memory HTTP transport. Production calls run,
// which always constructs the SSRF-guarded client for URL acquisition.
func runWithHTTPClient(args []string, stdout, stderr io.Writer, client *http.Client) int {
	fs := flag.NewFlagSet("track-fetch-web", flag.ContinueOnError)
	fs.SetOutput(stderr)
	urlFlag := fs.String("url", "", "page URL (http/https), or a local file path for testing; a bare argument works too")
	out := fs.String("out", "", "write JSONL to this file instead of stdout (prints a JSON summary)")
	note := fs.Bool("note", false, "print a ready-to-pipe Markdown note body instead of JSONL")
	snapshotDir := fs.String("snapshot-dir", "", "save response HTML and extracted Markdown under this directory; print a provenance manifest")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP fetch timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	source := ""
	if *urlFlag != "" {
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "track-fetch-web: use either --url or one positional source, not both")
			return 2
		}
		source = strings.TrimSpace(*urlFlag)
	} else if fs.NArg() == 1 {
		source = strings.TrimSpace(fs.Arg(0))
	}
	if source == "" || fs.NArg() > 1 {
		fmt.Fprintln(stderr, "track-fetch-web: exactly one page URL (or --url) is required")
		fs.Usage()
		return 2
	}
	if *note && *out != "" {
		fmt.Fprintln(stderr, "track-fetch-web: --note cannot be combined with --out")
		return 2
	}
	if *snapshotDir != "" && (*note || *out != "") {
		fmt.Fprintln(stderr, "track-fetch-web: --snapshot-dir cannot be combined with --note or --out")
		return 2
	}
	if *snapshotDir != "" && !isHTTPSource(source) {
		fmt.Fprintln(stderr, "track-fetch-web: --snapshot-dir requires an http(s) URL")
		return 2
	}

	original, pageURL, finalURL, lastModified, retrievedAt, err := acquire(source, *timeout, client)
	if err != nil {
		return fail(stderr, err)
	}

	page, err := web.Extract(bytes.NewReader(original), pageURL)
	if err != nil {
		return fail(stderr, err)
	}
	page = web.ApplyLastModified(page, lastModified)
	if page.Markdown == "" {
		fmt.Fprintln(stderr, "track-fetch-web: no readable content found; extracted text is empty")
	}
	sourceURL := ""
	if isHTTPSource(source) {
		sourceURL = source
	}

	if *snapshotDir != "" {
		manifest, err := web.SaveSnapshot(*snapshotDir, sourceURL, finalURL, retrievedAt, original, page)
		if err != nil {
			return fail(stderr, err)
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return fail(stderr, err)
		}
		line := append(encoded, '\n')
		n, writeErr := stdout.Write(line)
		if writeErr == nil && n != len(line) {
			writeErr = io.ErrShortWrite
		}
		if writeErr != nil {
			cleanupErr := os.RemoveAll(filepath.Dir(manifest.OriginalPath))
			if cleanupErr != nil {
				writeErr = errors.Join(writeErr, fmt.Errorf("remove incomplete snapshot: %w", cleanupErr))
			}
			return fail(stderr, writeErr)
		}
		return 0
	}

	if *note {
		fmt.Fprint(stdout, web.NoteBody(page, sourceURL, retrievedAt))
		return 0
	}

	rec, err := web.Record(page, sourceURL, retrievedAt)
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

const maxResponseBytes = 20 << 20

func isHTTPSource(source string) bool {
	parsed, err := url.Parse(source)
	return err == nil && parsed.Host != "" && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https"))
}

// acquire returns one complete response body, its final URL, and the instant the body was fully
// received, plus the HTTP modification label. Local files remain useful for replay and tests but
// have no base URL, final URL, or response headers.
func acquire(source string, timeout time.Duration, client *http.Client) ([]byte, *url.URL, string, string, time.Time, error) {
	if !isHTTPSource(source) {
		f, err := os.Open(source)
		if err != nil {
			return nil, nil, "", "", time.Time{}, err
		}
		body, readErr := io.ReadAll(io.LimitReader(f, maxResponseBytes+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil {
			return nil, nil, "", "", time.Time{}, errors.Join(readErr, closeErr)
		}
		if len(body) > maxResponseBytes {
			return nil, nil, "", "", time.Time{}, fmt.Errorf("read %s: file exceeds %d bytes", source, maxResponseBytes)
		}
		return body, nil, "", "", time.Now().UTC(), nil
	}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, nil, "", "", time.Time{}, err
	}
	// A realistic UA and HTML Accept header keep sites from serving an empty or bot page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; track/0.1; +https://github.com/ttak0422/track)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if client == nil {
		client = newGuardedClient(timeout)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, "", "", time.Time{}, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, "", "", time.Time{}, fmt.Errorf("fetch %s: HTTP %s", source, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		resp.Body.Close()
		return nil, nil, "", "", time.Time{}, fmt.Errorf("fetch %s: unsupported content type %q (expected HTML)", source, ct)
	}
	lastModified := resp.Header.Get("Last-Modified")
	// Reading into one bounded body lets snapshot mode save exactly the bytes that Extract parses.
	// Read one byte past the cap so oversize responses fail instead of becoming plausible truncations.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		return nil, nil, "", "", time.Time{}, errors.Join(readErr, closeErr)
	}
	if len(body) > maxResponseBytes {
		return nil, nil, "", "", time.Time{}, fmt.Errorf("fetch %s: response exceeds %d bytes", source, maxResponseBytes)
	}
	final := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL
	}
	// resp.Request.URL is the final URL after redirects; relative links resolve against it.
	return body, final, final.String(), lastModified, time.Now().UTC(), nil
}

// cgnat is the carrier-grade NAT range (RFC 6598), which net.IP.IsPrivate does not cover but is
// just as internal as RFC 1918 space.
var cgnat = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

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
				ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
				cgnat.Contains(ip) {
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
