// Package elem converts a browser-grab payload into a clipped web element — the engine behind the
// track-fetch-elem tool (see docs/spec/fetch.md). Like the web and rss packages it depends only on
// the dataset contract, never on the track CLI or store.
//
// A grab payload is untrusted page-controlled data produced by an external browser tool. This
// package is the converter/safety net: it clamps every string and array to a budget, allowlists
// attributes, redacts credential-shaped values, sanitizes URLs, and curates the computed-style
// subset before anything becomes track data. It never invents fields the payload omits.
package elem

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// Budget mirrors the payload budget: oversized values are truncated with a marker, not dropped.
type Budget struct {
	TextSnippetMaxLength  int
	HtmlSnippetMaxLength  int
	SelectorMaxLength     int
	PathMaxLength         int
	MetadataMaxLength     int
	NearbyTextMaxEntries  int
	NearbyTextMaxLength   int
	NearbyElementsMax     int
	NearbyElementMax      int
	AncestorPathMax       int
	SelectedTextMaxLength int
}

// DefaultBudget is the budget every clip is clamped to unless the caller tightens it.
var DefaultBudget = Budget{
	TextSnippetMaxLength:  200,
	HtmlSnippetMaxLength:  4096,
	SelectorMaxLength:     700,
	PathMaxLength:         900,
	MetadataMaxLength:     500,
	NearbyTextMaxEntries:  10,
	NearbyTextMaxLength:   200,
	NearbyElementsMax:     6,
	NearbyElementMax:      160,
	AncestorPathMax:       10,
	SelectedTextMaxLength: 500,
}

// safeAttributeNames allowlists attribute names; aria-* is always kept.
var safeAttributeNames = map[string]bool{
	"id": true, "class": true, "name": true, "type": true, "role": true, "href": true,
	"src": true, "alt": true, "title": true, "placeholder": true, "for": true,
	"action": true, "method": true,
}

// secretPatterns are credential-shaped value fragments that get redacted. Deliberately tight:
// broad words like "code" or "state" match ordinary class names and would degrade real sites.
var secretPatterns = []string{
	"access_token", "auth_token", "api_key", "apikey", "client_secret", "oauth_state",
	"x-amz-", "session_id", "sessionid", "csrf", "secret", "password", "passwd",
}

// styleProperties is the curated computed-style subset, in stable output order.
var styleProperties = []struct{ key, label string }{
	{"display", "display"},
	{"position", "position"},
	{"width", "width"},
	{"height", "height"},
	{"margin", "margin"},
	{"padding", "padding"},
	{"color", "color"},
	{"backgroundColor", "background"},
	{"border", "border"},
	{"borderRadius", "border-radius"},
	{"fontFamily", "font-family"},
	{"fontSize", "font-size"},
	{"fontWeight", "font-weight"},
	{"lineHeight", "line-height"},
	{"textAlign", "text-align"},
	{"zIndex", "z-index"},
}

// payload is the raw grab shape the tool accepts. Extra fields are ignored; absent fields stay
// absent rather than being invented.
type payload struct {
	Page         pagePayload   `json:"page"`
	Target       targetPayload `json:"target"`
	NearbyText   []string      `json:"nearbyText"`
	AncestorPath []string      `json:"ancestorPath"`
}

type pagePayload struct {
	SanitizedURL   string  `json:"sanitizedUrl"`
	Title          string  `json:"title"`
	ViewportWidth  float64 `json:"viewportWidth"`
	ViewportHeight float64 `json:"viewportHeight"`
	CapturedAt     string  `json:"capturedAt"`
}

type targetPayload struct {
	TagName         string               `json:"tagName"`
	Selector        string               `json:"selector"`
	ElementPath     string               `json:"elementPath"`
	FullPath        string               `json:"fullPath"`
	CSSClasses      string               `json:"cssClasses"`
	SelectedText    string               `json:"selectedText"`
	ReactComponents string               `json:"reactComponents"`
	SourceFile      string               `json:"sourceFile"`
	TextSnippet     string               `json:"textSnippet"`
	HTMLSnippet     string               `json:"htmlSnippet"`
	Attributes      map[string]string    `json:"attributes"`
	Accessibility   accessibilityPayload `json:"accessibility"`
	RectViewport    rectPayload          `json:"rectViewport"`
	ComputedStyles  map[string]string    `json:"computedStyles"`
}

type accessibilityPayload struct {
	Role           string `json:"role"`
	AccessibleName string `json:"accessibleName"`
	AriaLabel      string `json:"ariaLabel"`
}

type rectPayload struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Element is one clipped web element: the metadata a note needs plus the element rendered as
// Markdown.
type Element struct {
	Title    string    // accessible name, text snippet, or tag name — never empty
	URL      string    // sanitized page URL, query/fragment stripped
	Captured time.Time // the page's captured timestamp; zero when it declares none
	TagName  string
	Selector string
	Path     string // elementPath, else fullPath
	Markdown string // the element rendered as a Markdown body
}

// Clamp re-validates and clamps a raw grab payload into a bounded Element, returning an error only
// when the payload is not valid JSON.
func Clamp(raw []byte, b Budget) (Element, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Element{}, fmt.Errorf("parse grab payload: %w", err)
	}
	sanitized := sanitizeURL(p.Page.SanitizedURL)
	captured, _ := parseCaptured(p.Page.CapturedAt)

	el := Element{
		URL:      sanitized,
		Captured: captured,
		TagName:  clampStr(p.Target.TagName, 50),
		Selector: clampStr(p.Target.Selector, b.SelectorMaxLength),
		Path:     firstNonEmpty(clampStr(p.Target.ElementPath, b.PathMaxLength), clampStr(p.Target.FullPath, b.PathMaxLength)),
	}
	el.Title = elementTitle(p, el)
	el.Markdown = renderMarkdown(p, el, b)
	return el, nil
}

// Record maps a clipped element onto one canonical event record (docs/spec/fetch.md): time is the
// captured timestamp when the payload declares one, the run time otherwise, and the rendered
// Markdown rides along as an extra field. Validated against the event kind.
func Record(el Element, fetched time.Time) (dataset.Record, error) {
	t := el.Captured
	if t.IsZero() {
		t = fetched
	}
	title := el.Title
	if title == "" {
		title = el.TagName
	}
	if title == "" {
		title = el.URL
	}
	rec := dataset.Record{
		"version": dataset.SchemaVersion,
		"time":    t.Format(time.RFC3339),
		"title":   title,
	}
	if el.URL != "" {
		rec["url"] = el.URL
	}
	if el.Markdown != "" {
		rec["markdown"] = el.Markdown
	}
	if el.Selector != "" {
		rec["selector"] = el.Selector
	}
	if err := dataset.Validate(dataset.KindEvent, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// NoteBody renders the clip as a ready-to-pipe note body for `track new --title`.
func NoteBody(el Element) string {
	return el.Markdown + "\n"
}

// titleTextLen is the most text a title carries from the element's label/snippet — a title is a
// summary, not a quote, so it stays short and never carries the truncation marker.
const titleTextLen = 60

// elementTitle picks a title: accessible name first, then the text snippet, then the tag name.
func elementTitle(p payload, el Element) string {
	tag := el.TagName
	if n := inlineText(p.Target.Accessibility.AccessibleName); n != "" {
		return fmt.Sprintf("%s %q", tag, truncate(n, titleTextLen))
	}
	if s := inlineText(p.Target.TextSnippet); s != "" {
		return fmt.Sprintf("%s %q", tag, truncate(s, titleTextLen))
	}
	return tag
}

func inlineText(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// truncate shortens s to at most max runes without a marker; it is for display labels, not budget
// enforcement.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func renderMarkdown(p payload, el Element, b Budget) string {
	var lines []string
	if el.URL != "" {
		lines = append(lines, fmt.Sprintf("[Source](%s)", el.URL))
	}
	if role := clampStr(p.Target.Accessibility.Role, 100); role != "" {
		lines = append(lines, fmt.Sprintf("**Role:** %s", inlineText(role)))
	}
	if el.Selector != "" {
		lines = append(lines, fmt.Sprintf("**Selector:** %s", inlineCode(el.Selector)))
	}
	if el.Path != "" {
		lines = append(lines, fmt.Sprintf("**Location:** %s", inlineCode(el.Path)))
	}
	if react := clampStr(p.Target.ReactComponents, b.MetadataMaxLength); react != "" {
		lines = append(lines, fmt.Sprintf("**React:** %s", inlineText(react)))
	}
	if src := clampStr(p.Target.SourceFile, b.MetadataMaxLength); src != "" {
		lines = append(lines, fmt.Sprintf("**Source:** %s", inlineText(src)))
	}
	if r := p.Target.RectViewport; r.Width > 0 || r.Height > 0 {
		lines = append(lines, fmt.Sprintf("**Bounds:** x=%d, y=%d, %dx%d",
			int(r.X), int(r.Y), int(r.Width), int(r.Height)))
	}
	if classes := clampStr(p.Target.CSSClasses, b.MetadataMaxLength); classes != "" {
		lines = append(lines, fmt.Sprintf("**Classes:** %s", inlineCode(classes)))
	}
	if sel := clampStr(p.Target.SelectedText, b.SelectedTextMaxLength); sel != "" {
		lines = append(lines, fmt.Sprintf("**Selected text:** %q", inlineText(sel)))
	} else if s := clampStr(p.Target.TextSnippet, b.TextSnippetMaxLength); s != "" {
		lines = append(lines, fmt.Sprintf("**Text:** %q", inlineText(s)))
	}
	if attrs := safeAttributes(p.Target.Attributes); len(attrs) > 0 {
		lines = append(lines, "**Attributes:**")
		for _, k := range sortedKeys(attrs) {
			lines = append(lines, fmt.Sprintf("- `%s`: %s", k, inlineText(attrs[k])))
		}
	}
	if nearby := clampStrings(p.NearbyText, b.NearbyTextMaxEntries, b.NearbyTextMaxLength); len(nearby) > 0 {
		lines = append(lines, "**Nearby text:**")
		for _, t := range nearby {
			lines = append(lines, "- "+inlineText(t))
		}
	}
	if styles := formatStyles(p.Target.ComputedStyles); len(styles) > 0 {
		lines = append(lines, "**Computed styles:**")
		lines = append(lines, styles...)
	}
	if html := clampStr(p.Target.HTMLSnippet, b.HtmlSnippetMaxLength); html != "" {
		lines = append(lines, fenced("html", html))
	}
	return strings.Join(lines, "\n\n")
}

func formatStyles(styles map[string]string) []string {
	var lines []string
	for _, sp := range styleProperties {
		v := strings.TrimSpace(styles[sp.key])
		if v == "" || v == "auto" || v == "normal" {
			continue
		}
		if sp.key == "position" && v == "static" {
			continue
		}
		if sp.key == "display" && v == "inline" {
			continue
		}
		if sp.key == "backgroundColor" && v == "rgba(0, 0, 0, 0)" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", sp.label, inlineText(v)))
	}
	return lines
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func fenced(lang, content string) string {
	marker := strings.Repeat("`", maxBacktickRun(content, 3)+1)
	return marker + lang + "\n" + content + "\n" + marker
}

// inlineCode wraps content in backticks sized to fence any backtick runs inside.
func inlineCode(content string) string {
	marker := strings.Repeat("`", maxBacktickRun(content, 0)+1)
	pad := ""
	if strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") {
		pad = " "
	}
	return marker + pad + content + pad + marker
}

func maxBacktickRun(s string, floor int) int {
	best, run := floor, 0
	for i := 0; i < len(s); i++ {
		if s[i] != '`' {
			run = 0
			continue
		}
		run++
		if run > best {
			best = run
		}
	}
	return best
}

func clampStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + " (truncated)"
}

func clampStrings(arr []string, maxEntries, maxEntryLength int) []string {
	if len(arr) > maxEntries {
		arr = arr[:maxEntries]
	}
	out := make([]string, 0, len(arr))
	for _, s := range arr {
		if s = strings.TrimSpace(clampStr(s, maxEntryLength)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func containsSecret(v string) bool {
	lower := strings.ToLower(v)
	for _, p := range secretPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// safeAttributes allowlists attribute names (plus aria-*), redacts secret values, and sanitizes
// URL-bearing attributes.
func safeAttributes(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range attrs {
		name := strings.ToLower(k)
		isAria := strings.HasPrefix(name, "aria-")
		if !isAria && !safeAttributeNames[name] {
			continue
		}
		value := clampStr(v, 2000)
		switch {
		case containsSecret(value):
			out[name] = "[redacted]"
		case (name == "href" || name == "src" || name == "action") && value != "":
			if s := sanitizeURL(value); s != "" {
				out[name] = s
			}
		case name == "class":
			out[name] = clampStr(value, 200)
		default:
			out[name] = clampStr(value, 500)
		}
	}
	return out
}

func sanitizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func parseCaptured(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
