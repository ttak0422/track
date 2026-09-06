package book

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"
)

// API endpoints. Base URLs are fields on Client so tests can point lookups at an
// httptest server without touching the network.
const (
	googleBooksBase = "https://www.googleapis.com/books/v1"
	openLibraryBase = "https://openlibrary.org"
)

// Client performs book lookups and cover downloads. It owns the HTTP client — an
// SSRF-guarded transport mirroring the engine's link-preview fetcher, so DNS names
// (and redirect hops) that resolve to private addresses are refused — and carries
// the API base URLs for testability.
type Client struct {
	hc *http.Client
	gb string // Google Books volumes endpoint root
	ol string // Open Library API root
	ua string
}

// NewClient returns a Client with the SSRF-guarded HTTP transport and the live
// API endpoints.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		hc: guardedClient(timeout),
		gb: googleBooksBase,
		ol: openLibraryBase,
		ua: "track-fetch-book/0.1 (+https://github.com/ttak0422/track)",
	}
}

// newTestClient wires an unguarded client (httptest servers listen on loopback,
// which the guarded transport refuses) against caller-provided endpoints.
func newTestClient(hc *http.Client, gbBase, olBase string) *Client {
	return &Client{hc: hc, gb: gbBase, ol: olBase, ua: "track-fetch-book-test/0.1"}
}

// getJSON performs a GET with the tool's User-Agent and decodes the JSON body,
// failing on non-2xx statuses. The caller closes nothing: the body is drained and
// closed here.
func (c *Client) getJSON(ctx context.Context, rawURL string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{URL: rawURL, Status: resp.StatusCode, Body: truncate(body)}
	}
	if err := decodeJSON(body, v); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// HTTPError is a non-2xx API response. The body snippet lets the caller
// distinguish a quota 429 from a missing volume, which decides whether to fall
// back to the next source.
type HTTPError struct {
	URL    string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fetch %s: HTTP %d (%s)", e.URL, e.Status, e.Body)
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// downloadCover fetches the cover image into out under name. It uses the guarded
// transport, so cover CDN redirects (e.g. Open Library → archive.org) are checked
// for private targets too. Any failure is reported to the caller; the note falls
// back to the remote URL.
func (c *Client) downloadCover(ctx context.Context, coverURL, out, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cover %s: HTTP %s", coverURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return err
	}
	return writeFile(out, name, data)
}

// coverFileName derives a stable, filesystem-safe cover filename from a book:
// cover-<isbn> when the edition has one, otherwise cover-<title-slug>. The caller
// supplies the directory (conventionally the vault's assets/ directory, hence the
// "assets/<file>" reference).
func coverFileName(b Book) string {
	stem := "cover-" + b.ISBN
	if stem == "cover-" {
		slug := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r >= 0x80:
				return r
			default:
				return '-'
			}
		}, b.Title)
		slug = strings.Trim(strings.TrimSpace(slug), "-")
		if slug == "" {
			slug = "unknown"
		}
		stem = "cover-" + slug
	}
	return stem + coverExt(b.CoverURL)
}

// FetchCover downloads a book's cover into dir and returns the "assets/<file>"
// reference to embed in a note — the caller's bridge to `track asset import` (the
// tool itself never writes into the vault). It returns ("", nil) when the book has
// no cover, and an error when the download fails (the caller keeps the remote URL).
func (c *Client) FetchCover(ctx context.Context, dir string, b Book) (string, error) {
	if b.CoverURL == "" || dir == "" {
		return "", nil
	}
	name := coverFileName(b)
	if err := c.downloadCover(ctx, b.CoverURL, dir, name); err != nil {
		return "", err
	}
	return "assets/" + name, nil
}

// coverExt guesses an image extension from a cover URL (Open Library serves
// -S/-M/-L jpg, Google Books serves zoom=… with content-type). Fallback: jpg.
func coverExt(coverURL string) string {
	p := strings.ToLower(path.Base(coverURL))
	switch {
	case strings.HasSuffix(p, ".png"):
		return ".png"
	case strings.HasSuffix(p, ".gif"):
		return ".gif"
	case strings.HasSuffix(p, ".webp"):
		return ".webp"
	default:
		return ".jpg"
	}
}

// cgnat is the carrier-grade NAT range (RFC 6598), which net.IP.IsPrivate does
// not cover but is just as internal as RFC 1918 space.
var cgnat = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// guardedClient is the SSRF-guarded HTTP client, mirroring the engine's web
// link-preview fetcher (docs/spec/fetch.md keeps fetch tools independent of the
// engine, hence the local copy): the dial control sees the resolved ip:port, so it
// catches both direct private targets and DNS names (including redirect hops) that
// resolve to private addresses.
func guardedClient(timeout time.Duration) *http.Client {
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
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// isPublicURL reports whether u is an http(s) URL (the guards above already refuse
// private destinations at dial time; this is a cheap shape check for callers).
func isPublicURL(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
