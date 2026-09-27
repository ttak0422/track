// Package web extracts the readable article from an HTML page — the engine behind the
// track-fetch-web clipper (see docs/spec/fetch.md). Like the rss package it depends only on the
// dataset contract, never on the track CLI or store.
//
// Extraction is a compact readability heuristic, not a port of a readability library: prefer the
// page's own semantic container (<article>, then <main>), otherwise pick the element that
// accumulates the most paragraph text with a low link density. That covers article-shaped pages
// without a heavyweight dependency; pages rendered entirely by scripts are out of scope.
package web

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Page is one clipped web page: the metadata a note needs plus the main content as Markdown.
type Page struct {
	Title     string       // og:title, <title>, or the first heading
	Published DateMetadata // declared publication metadata; only an explicit instant has Timestamp
	Modified  DateMetadata // declared update metadata; never used as the publication time
	Image     string       // lead image URL (og:image or the first content image), absolute
	Markdown  string       // readable main content converted to Markdown
}

// DateMetadata keeps the source value and its declared precision separately from a normalized
// instant. Date-only and timezone-less values are retained as raw metadata but are not instants.
type DateMetadata struct {
	Raw       string  `json:"raw"`
	Precision string  `json:"precision"`
	Timestamp *string `json:"timestamp"`
}

// ApplyLastModified uses the HTTP header only when the document does not declare a modified time.
// The header is never treated as a publication date.
func ApplyLastModified(p Page, raw string) Page {
	if p.Modified.Raw == "" && strings.TrimSpace(raw) != "" {
		p.Modified = parseDateMetadata(raw)
	}
	return p
}

// Extract parses an HTML page and returns its readable content. pageURL, when non-nil, is the base
// for resolving relative links and images; pass nil for local files with no base.
func Extract(r io.Reader, pageURL *url.URL) (Page, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return Page{}, fmt.Errorf("parse html: %w", err)
	}

	meta := collectMeta(doc)
	prune(doc)
	content := mainContent(doc)

	p := Page{
		Title:     firstNonEmpty(meta.props["og:title"], meta.props["twitter:title"], meta.title),
		Published: meta.published,
		Modified:  meta.modified,
		Image:     resolveURL(pageURL, firstNonEmpty(meta.props["og:image"], meta.props["og:image:url"], meta.props["twitter:image"])),
		Markdown:  renderMarkdown(content, pageURL),
	}
	if p.Title == "" {
		p.Title = firstHeadingText(content)
	}
	p.Markdown = dropLeadingTitleHeading(p.Markdown, p.Title)
	if p.Image == "" {
		p.Image = firstImage(content, pageURL)
	}
	return p, nil
}

// Record maps a clipped page onto one canonical event record (docs/spec/fetch.md): time is the
// explicitly zoned publication instant when available, otherwise the retrieval time. Raw
// publication/modification labels and retrieval time remain separate extra fields.
func Record(p Page, sourceURL string, retrieved time.Time) (dataset.Record, error) {
	t := retrieved
	if p.Published.Timestamp != nil {
		if published, err := time.Parse(time.RFC3339Nano, *p.Published.Timestamp); err == nil {
			t = published
		}
	}
	title := p.Title
	if title == "" {
		title = sourceURL
	}
	rec := dataset.Record{
		"version":      dataset.SchemaVersion,
		"time":         t.Format(time.RFC3339),
		"title":        title,
		"published":    withDatePrecision(p.Published),
		"modified":     withDatePrecision(p.Modified),
		"retrieved_at": retrieved.UTC().Format(time.RFC3339Nano),
	}
	if sourceURL != "" {
		rec["url"] = sourceURL
	}
	if p.Image != "" {
		rec["image"] = p.Image
	}
	if p.Markdown != "" {
		rec["markdown"] = p.Markdown
	}
	if err := dataset.Validate(dataset.KindEvent, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// NoteBody renders the clip as a ready-to-pipe note body for `track new --title`: a provenance
// line dated by retrieval time (not publication time), source-declared dates, the lead image, then
// the content.
func NoteBody(p Page, sourceURL string, retrieved time.Time) string {
	var blocks []string
	provenance := fmt.Sprintf("Retrieved %s", retrieved.Format("2006-01-02"))
	if sourceURL != "" {
		provenance = fmt.Sprintf("[Source](%s) — retrieved %s", sourceURL, retrieved.Format("2006-01-02"))
	}
	blocks = append(blocks, provenance)
	if p.Published.Raw != "" {
		blocks = append(blocks, "Published: "+p.Published.Raw)
	}
	if p.Modified.Raw != "" {
		blocks = append(blocks, "Modified: "+p.Modified.Raw)
	}
	if p.Image != "" {
		blocks = append(blocks, fmt.Sprintf("![](%s)", p.Image))
	}
	if p.Markdown != "" {
		blocks = append(blocks, p.Markdown)
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// pageMeta is what the document head (plus <time> elements) declares about the page.
type pageMeta struct {
	props     map[string]string
	title     string
	published DateMetadata
	modified  DateMetadata
}

// collectMeta walks the document for <meta property/name>, <title>, and <time datetime> before any
// pruning, so metadata survives even when it sits inside a container the heuristic would drop.
func collectMeta(doc *html.Node) pageMeta {
	m := pageMeta{props: map[string]string{}}
	var genericTime, publishedTime, modifiedTime string
	walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		switch n.DataAtom {
		case atom.Meta:
			key := strings.ToLower(firstNonEmpty(attr(n, "property"), attr(n, "name"), attr(n, "itemprop")))
			if content := attr(n, "content"); key != "" && strings.TrimSpace(content) != "" {
				if _, seen := m.props[key]; !seen {
					m.props[key] = content
				}
			}
		case atom.Title:
			if m.title == "" {
				m.title = collapseSpace(innerText(n))
			}
		case atom.Time:
			if value := strings.TrimSpace(attr(n, "datetime")); value != "" {
				switch strings.ToLower(attr(n, "itemprop")) {
				case "datepublished":
					if publishedTime == "" {
						publishedTime = value
					}
				case "datemodified":
					if modifiedTime == "" {
						modifiedTime = value
					}
				default:
					if genericTime == "" {
						genericTime = value
					}
				}
			}
		}
		return true
	})
	m.published = parseDateMetadata(firstRaw(
		m.props["article:published_time"], m.props["og:article:published_time"],
		m.props["datepublished"], m.props["date_published"], m.props["pubdate"],
		m.props["date"], m.props["dc.date.issued"], publishedTime, genericTime,
	))
	m.modified = parseDateMetadata(firstRaw(
		m.props["article:modified_time"], m.props["og:updated_time"],
		m.props["og:article:modified_time"], m.props["datemodified"], m.props["date_modified"],
		m.props["last-modified"], m.props["lastmodified"], m.props["dc.date.modified"], modifiedTime,
	))
	return m
}

func firstRaw(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseDateMetadata(raw string) DateMetadata {
	metadata := DateMetadata{Raw: raw, Precision: "absent"}
	value := strings.TrimSpace(raw)
	if value == "" {
		return metadata
	}
	metadata.Precision = "unknown"
	// RFC 3339 -00:00 (and mail-date -0000) denotes an unknown local offset,
	// not evidence of UTC. Preserve the label without inventing an instant.
	if strings.HasSuffix(value, "-00:00") || strings.HasSuffix(value, "-0000") {
		return metadata
	}
	if _, err := time.Parse(time.DateOnly, value); err == nil {
		metadata.Precision = "date"
		return metadata
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z0700",
		"2006-01-02T15:04Z07:00",
		"2006-01-02T15:04Z0700",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z0700",
		"2006-01-02 15:04Z07:00",
		"2006-01-02 15:04Z0700",
		time.RFC1123Z,
	} {
		if instant, err := time.Parse(layout, value); err == nil {
			timestamp := instant.Format(time.RFC3339Nano)
			metadata.Precision = "instant"
			metadata.Timestamp = &timestamp
			return metadata
		}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04",
		"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02 15:04",
	} {
		if _, err := time.Parse(layout, value); err == nil {
			metadata.Precision = "local_datetime"
			return metadata // Preserve a timezone-less label without inventing an instant.
		}
	}
	// HTTP dates are accepted only with an explicit zone token. The legacy asctime form has no zone
	// and must not be promoted to UTC merely because net/http can parse it.
	if strings.HasSuffix(value, " GMT") || strings.HasSuffix(value, " UTC") {
		if instant, err := http.ParseTime(value); err == nil {
			timestamp := instant.Format(time.RFC3339Nano)
			metadata.Precision = "instant"
			metadata.Timestamp = &timestamp
		}
	}
	return metadata
}

// noiseTags never carry article content and are removed wholesale.
var noiseTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "iframe": true,
	"svg": true, "form": true, "button": true, "input": true, "select": true, "textarea": true,
	"nav": true, "header": true, "footer": true, "aside": true, "dialog": true,
}

// noiseClassRe matches class/id values of page furniture that a readability pass drops. Word
// boundaries keep it from firing on content-bearing names like "article-nav-guide" firing only on
// the noise word itself.
var noiseClassRe = regexp.MustCompile(`(?i)\b(comments?|sidebar|share|social|related|promo|advert|ads?|banner|menu|nav|footer|breadcrumbs?|newsletter|subscribe|cookie|popup)\b`)

// prune removes chrome and boilerplate from the tree in place: known non-content tags everywhere,
// and containers whose class/id names page furniture. Semantic candidates (article/main/body) are
// never class-pruned so an over-broad site class cannot delete the article itself.
func prune(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && shouldPrune(c) {
			n.RemoveChild(c)
		} else {
			prune(c)
		}
		c = next
	}
}

func shouldPrune(n *html.Node) bool {
	if noiseTags[n.Data] {
		return true
	}
	switch n.DataAtom {
	case atom.Article, atom.Main, atom.Body:
		return false
	}
	return noiseClassRe.MatchString(attr(n, "class") + " " + attr(n, "id"))
}

// minCandidateText is the least text (in runes) a semantic container must hold to be trusted as the
// article — some sites use <article> for teaser cards.
const minCandidateText = 140

// minParagraphText is the least text a paragraph must hold to count toward its parent's score.
const minParagraphText = 25

// mainContent picks the node that holds the readable article. Semantic containers win when they
// carry real text; otherwise the parent accumulating the most paragraph text (discounted by link
// density, so link farms lose to prose) is chosen. Falls back to <body>.
func mainContent(doc *html.Node) *html.Node {
	for _, tag := range []atom.Atom{atom.Article, atom.Main} {
		if n := richest(doc, tag); n != nil {
			return n
		}
	}

	scores := map[*html.Node]float64{}
	walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		switch n.DataAtom {
		case atom.P, atom.Pre, atom.Blockquote:
			t := len([]rune(strings.TrimSpace(innerText(n))))
			if t < minParagraphText || n.Parent == nil {
				return true
			}
			scores[n.Parent] += float64(t)
			if gp := n.Parent.Parent; gp != nil {
				scores[gp] += float64(t) / 2
			}
		}
		return true
	})
	var best *html.Node
	bestScore := 0.0
	walk(doc, func(n *html.Node) bool { // walk order makes ties deterministic (outermost first)
		if s, ok := scores[n]; ok {
			if s = s * (1 - linkDensity(n)); s > bestScore {
				best, bestScore = n, s
			}
		}
		return true
	})
	if best != nil {
		return best
	}
	if body := find(doc, atom.Body); body != nil {
		return body
	}
	return doc
}

// richest returns the tag's occurrence with the most text, or nil when none holds enough to be the
// article.
func richest(doc *html.Node, tag atom.Atom) *html.Node {
	var best *html.Node
	bestLen := minCandidateText - 1
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == tag {
			if l := len([]rune(strings.TrimSpace(innerText(n)))); l > bestLen {
				best, bestLen = n, l
			}
			return false // nested same-tag nodes are part of this candidate
		}
		return true
	})
	return best
}

// linkDensity is the share of a node's text that sits inside links; near 1 means navigation.
func linkDensity(n *html.Node) float64 {
	total := len([]rune(innerText(n)))
	if total == 0 {
		return 0
	}
	linked := 0
	walk(n, func(c *html.Node) bool {
		if c.Type == html.ElementNode && c.DataAtom == atom.A {
			linked += len([]rune(innerText(c)))
			return false
		}
		return true
	})
	return float64(linked) / float64(total)
}

// firstHeadingText is the title fallback of last resort: the first h1/h2 in the content.
func firstHeadingText(content *html.Node) string {
	text := ""
	walk(content, func(n *html.Node) bool {
		if n.Type == html.ElementNode && (n.DataAtom == atom.H1 || n.DataAtom == atom.H2) {
			text = collapseSpace(innerText(n))
			return false
		}
		return text == ""
	})
	return text
}

// dropLeadingTitleHeading removes the content's first block when it is a heading repeating the page
// title — the title becomes the note title, so keeping it would duplicate it in the body.
func dropLeadingTitleHeading(md, title string) string {
	if title == "" {
		return md
	}
	first, rest, _ := strings.Cut(md, "\n\n")
	if h := strings.TrimLeft(first, "# "); strings.HasPrefix(first, "#") && strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(title)) {
		return rest
	}
	return md
}

// firstImage returns the first usable content image, absolute, for the lead-image fallback.
func firstImage(content *html.Node, base *url.URL) string {
	src := ""
	walk(content, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.Img {
			if resolved := resolveURL(base, attr(n, "src")); resolved != "" {
				src = resolved
				return false
			}
		}
		return src == ""
	})
	return src
}

// walk visits n and its descendants depth-first; fn returning false skips the node's children.
func walk(n *html.Node, fn func(*html.Node) bool) {
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func find(doc *html.Node, tag atom.Atom) *html.Node {
	var found *html.Node
	walk(doc, func(n *html.Node) bool {
		if found == nil && n.Type == html.ElementNode && n.DataAtom == tag {
			found = n
		}
		return found == nil
	})
	return found
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// innerText concatenates all text under n, whitespace as-is.
func innerText(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) bool {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		return true
	})
	return b.String()
}

var spaceRe = regexp.MustCompile(`\s+`)

// collapseSpace normalizes runs of whitespace to single spaces and trims.
func collapseSpace(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// resolveURL turns a possibly-relative reference into an absolute http(s) URL against base,
// dropping other schemes so a clip never carries a javascript:/data: source.
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
