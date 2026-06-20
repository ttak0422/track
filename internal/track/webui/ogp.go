package webui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// ogpResult is the subset of Open Graph / fallback metadata the web preview renders for an embedded
// link (![label](url)). Empty fields are omitted so the client can degrade to a plain link.
type ogpResult struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
}

type ogpCacheEntry struct {
	res ogpResult
	at  time.Time
	ok  bool
}

const (
	ogpSuccessTTL = 6 * time.Hour
	ogpFailureTTL = 10 * time.Minute
	ogpMaxBody    = 1 << 20 // 1 MiB is plenty to reach <head>'s meta tags.
	ogpMaxEntries = 512
)

// handleOGP fetches Open Graph metadata for an embedded link and returns it as JSON. Only http(s) URLs
// are accepted and the fetch is guarded against SSRF (private/loopback addresses are refused), so a
// note cannot use this to probe the local network. Results are cached so repeated renders of the same
// note do not refetch. A fetch failure is reported with 502 and negatively cached, letting the client
// fall back to a plain link.
func (s *Server) handleOGP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != "" {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeError(w, errors.New("url is required"), http.StatusBadRequest)
		return
	}
	if !isHTTPURL(raw) {
		writeError(w, errors.New("only http(s) URLs are supported"), http.StatusBadRequest)
		return
	}

	if entry, ok := s.ogpFromCache(raw); ok {
		if !entry.ok {
			writeError(w, errors.New("could not fetch link metadata"), http.StatusBadGateway)
			return
		}
		writeJSON(w, entry.res)
		return
	}

	res, err := fetchOGP(r.Context(), raw)
	s.storeOGP(raw, res, err == nil)
	if err != nil {
		writeError(w, fmt.Errorf("fetch link metadata: %w", err), http.StatusBadGateway)
		return
	}
	writeJSON(w, res)
}

func (s *Server) ogpFromCache(key string) (ogpCacheEntry, bool) {
	s.ogpMu.Lock()
	defer s.ogpMu.Unlock()
	entry, ok := s.ogpCache[key]
	if !ok {
		return ogpCacheEntry{}, false
	}
	ttl := ogpSuccessTTL
	if !entry.ok {
		ttl = ogpFailureTTL
	}
	if time.Since(entry.at) > ttl {
		delete(s.ogpCache, key)
		return ogpCacheEntry{}, false
	}
	return entry, true
}

func (s *Server) storeOGP(key string, res ogpResult, ok bool) {
	s.ogpMu.Lock()
	defer s.ogpMu.Unlock()
	if s.ogpCache == nil {
		s.ogpCache = make(map[string]ogpCacheEntry)
	}
	// A note pointing at a runaway set of distinct URLs should not grow the cache without bound; a
	// personal vault never approaches this, so a blunt reset is enough.
	if len(s.ogpCache) >= ogpMaxEntries {
		s.ogpCache = make(map[string]ogpCacheEntry)
	}
	s.ogpCache[key] = ogpCacheEntry{res: res, at: time.Now(), ok: ok}
}

// ogpClient fetches link metadata with the SSRF guard installed. It is shared because it is stateless.
var ogpClient = newOGPClient()

func newOGPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		// Control runs with the resolved ip:port about to be dialed, so it catches both direct private
		// targets and DNS names (including redirect hops) that resolve to private addresses.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("unresolved address %q", address)
			}
			if !isPublicIP(ip) {
				return fmt.Errorf("refusing to fetch non-public address %s", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func fetchOGP(ctx context.Context, target string) (ogpResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ogpResult{}, err
	}
	// A realistic UA and HTML Accept header keep sites from serving an empty or bot page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; track/0.1; +https://github.com/ttak0422/track)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := ogpClient.Do(req)
	if err != nil {
		return ogpResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ogpResult{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return ogpResult{}, fmt.Errorf("unsupported content type %q", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, ogpMaxBody))
	if err != nil {
		return ogpResult{}, err
	}
	// resp.Request.URL is the final URL after any redirects; relative og:image values resolve against it.
	res := parseOGP(string(body), resp.Request.URL)
	res.URL = target
	if res.Title == "" && res.Description == "" && res.Image == "" {
		return ogpResult{}, errors.New("no link metadata found")
	}
	return res, nil
}

var (
	metaTagRe  = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attrRe     = regexp.MustCompile(`(?is)([a-z:_-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	titleTagRe = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	headEndRe  = regexp.MustCompile(`(?is)</head>`)
)

// parseOGP extracts Open Graph metadata from an HTML document, falling back to Twitter card tags, the
// meta description, and the <title> element. It only scans the <head> to avoid mistaking body content
// for metadata, and resolves a relative og:image against the page URL.
func parseOGP(doc string, pageURL *url.URL) ogpResult {
	head := doc
	if loc := headEndRe.FindStringIndex(doc); loc != nil {
		head = doc[:loc[0]]
	}

	props := map[string]string{}
	for _, tag := range metaTagRe.FindAllString(head, -1) {
		key, content := "", ""
		hasContent := false
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			name := strings.ToLower(m[1])
			value := firstNonEmpty(m[3], m[4], m[5])
			switch name {
			case "property", "name":
				key = strings.ToLower(value)
			case "content":
				content = value
				hasContent = true
			}
		}
		if key == "" || !hasContent {
			continue
		}
		// First value wins, so a page's primary og:* tag is not overwritten by a later duplicate.
		if _, seen := props[key]; !seen {
			props[key] = html.UnescapeString(strings.TrimSpace(content))
		}
	}

	res := ogpResult{
		Title:       firstNonEmpty(props["og:title"], props["twitter:title"]),
		Description: firstNonEmpty(props["og:description"], props["twitter:description"], props["description"]),
		Image:       firstNonEmpty(props["og:image"], props["og:image:url"], props["twitter:image"], props["twitter:image:src"]),
		SiteName:    props["og:site_name"],
	}
	if res.Title == "" {
		if m := titleTagRe.FindStringSubmatch(head); m != nil {
			res.Title = html.UnescapeString(strings.TrimSpace(m[1]))
		}
	}
	if res.SiteName == "" && pageURL != nil {
		res.SiteName = pageURL.Hostname()
	}
	res.Image = resolveURL(pageURL, res.Image)
	res.Title = truncateRunes(res.Title, 200)
	res.Description = truncateRunes(res.Description, 320)
	return res
}

// resolveURL turns a possibly-relative image reference into an absolute URL against the page, dropping
// anything that is not http(s) so the client never renders a javascript:/data: image source.
func resolveURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// isPublicIP reports whether ip is a routable public address, rejecting loopback, private, link-local,
// multicast, and unspecified ranges so OGP fetches cannot reach internal services.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
