package site

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// writePages writes one static HTML file per crawlable route: the copied SPA shell (index.html) with
// per-page Open Graph / Twitter Card meta injected into <head>. This makes `track export-site`
// self-sufficient for crawlers and social shares — a shared/crawled note URL shows that note's title,
// description, and cover image instead of one generic site-wide head — and it lays down a real file at
// every route so a deep link resolves on a fallback-less host instead of 404ing.
//
// The client renders the actual content from the URL (createRoot, not hydration; see web/src/main.tsx),
// so these pages carry only the head. The richer Node prerender (web/scripts/prerender.mjs, run by
// `make site`) overwrites the same files with SSR content; this is the standalone fallback the CLI
// produces on its own. startPage is baked into every page's shell, but only the "/" route reads it
// (START_PAGE_ID), so per-note pages still render their own route.
func writePages(outDir, startPage string, root int64, docs, listed []doc, site jsonSite) error {
	raw, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		return err
	}
	base := applyPlaceholders(string(raw), startPage)

	write := func(rel, head string) error {
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(injectHead(base, head)), 0o644)
	}

	// Root index.html carries the start note's meta (sharing the site root previews that note).
	var rootDoc *doc
	for i := range docs {
		if docs[i].id == root {
			rootDoc = &docs[i]
			break
		}
	}
	if err := write("index.html", pageHead(site, rootDoc, "/")); err != nil {
		return err
	}

	// Per-note pages: notes/<slug>/index.html resolves /notes/<slug>.
	for i := range docs {
		d := &docs[i]
		slug := PublishID(d.id)
		if err := write(filepath.Join("notes", slug, "index.html"), pageHead(site, d, "/notes/"+slug)); err != nil {
			return err
		}
	}

	// Site-level pages get a generic head (no per-day OG images). Mirrors the prerender's route set so
	// every deep link resolves standalone; the calendar and per-day pages are calendar-only.
	generic := []string{"graph", "empty"}
	if site.Calendar {
		generic = append(generic, "calendar")
	}
	for _, r := range generic {
		if err := write(filepath.Join(r, "index.html"), pageHead(site, nil, "/"+r)); err != nil {
			return err
		}
	}
	if site.Calendar {
		days := map[string]bool{}
		for _, d := range listed {
			for _, day := range d.days {
				days[day] = true
			}
		}
		for day := range days {
			if err := write(filepath.Join("day", day, "index.html"), pageHead(site, nil, "/day/"+day)); err != nil {
				return err
			}
		}
	}

	// Per-tag pages: tags/<tag>/index.html resolves /tags/<tag> for every published tag and each of
	// its ancestors (tags are hierarchical, so /tags/a lists #a/b notes too).
	for _, tag := range tagRoutes(docs) {
		if err := write(filepath.Join("tags", filepath.FromSlash(tag), "index.html"), pageHead(site, nil, "/tags/"+tag)); err != nil {
			return err
		}
	}
	return nil
}

// tagRoutes returns every tag used by the published docs plus each hierarchical ancestor ("a/b/c"
// also yields "a/b" and "a"), deduplicated, so every reachable tag page is a real file.
func tagRoutes(docs []doc) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range docs {
		for _, tag := range d.tags {
			parts := strings.Split(tag, "/")
			for i := range parts {
				prefix := strings.Join(parts[:i+1], "/")
				if prefix == "" || seen[prefix] {
					continue
				}
				seen[prefix] = true
				out = append(out, prefix)
			}
		}
	}
	sort.Strings(out)
	return out
}

// applyPlaceholders substitutes the placeholders the live server fills in at request time. The static
// site has no server, so the default theme falls back to "system" and there are no color overrides;
// left unsubstituted, __TRACK_COLOR_OVERRIDES__ would show as literal text. startPage is the root note's
// published id, baked in so the frontend redirects to the start page on launch without a site.json
// round-trip (see web/src/runtime.ts START_PAGE_ID).
func applyPlaceholders(tmpl, startPage string) string {
	tmpl = strings.ReplaceAll(tmpl, "__TRACK_DEFAULT_THEME__", "system")
	tmpl = strings.ReplaceAll(tmpl, "__TRACK_COLOR_OVERRIDES__", "")
	tmpl = strings.ReplaceAll(tmpl, "__TRACK_START_PAGE__", startPage)
	return tmpl
}

var titleTagRe = regexp.MustCompile(`(?is)<title\b[^>]*>.*?</title>`)

// injectHead drops the shell's own <title> and inserts head (a fresh <title> plus OGP meta) before
// </head>; if the shell has no </head> (a bare test stub), it prepends the tags.
func injectHead(base, head string) string {
	base = titleTagRe.ReplaceAllString(base, "")
	if i := strings.Index(base, "</head>"); i >= 0 {
		return base[:i] + head + base[i:]
	}
	return head + base
}

// pageHead returns the <title> + Open Graph / Twitter Card head for one route. d is the route's note, or
// nil for the site-level pages (graph/calendar/day), which get a generic head. All injected content is
// HTML-escaped. og:url and og:image are absolute, so they are emitted only when the export ran with a
// base URL (--base-url); the published image path is always a slugged assets/ reference, never an
// external or unsafe scheme.
func pageHead(site jsonSite, d *doc, route string) string {
	name := siteName(site)
	title, ogType, desc, image := name, "website", "", ""
	if d != nil {
		ogType = "article"
		if d.title != "" {
			title = d.title
		}
		desc = d.desc
		if desc == "" {
			desc = bodyExcerpt(d.body)
		}
		if d.image != "" {
			image = "assets/" + publishAssetName(d.image)
		}
	}
	if desc == "" {
		desc = name
	}

	pageTitle := name
	if d != nil && title != "" && title != name {
		pageTitle = title + " · " + name
	}

	var b strings.Builder
	b.WriteString("<title>" + html.EscapeString(pageTitle) + "</title>")
	b.WriteString(metaProperty("og:site_name", name))
	b.WriteString(metaProperty("og:title", title))
	b.WriteString(metaProperty("og:type", ogType))
	b.WriteString(metaProperty("og:description", desc))
	if image != "" {
		b.WriteString(metaName("twitter:card", "summary_large_image"))
	} else {
		b.WriteString(metaName("twitter:card", "summary"))
	}
	if site.BaseURL != "" {
		b.WriteString(metaProperty("og:url", site.BaseURL+routePath(route)))
		if image != "" {
			b.WriteString(metaProperty("og:image", site.BaseURL+"/"+image))
		}
	}
	return b.String()
}

func siteName(site jsonSite) string {
	if site.Title != "" {
		return site.Title
	}
	return "track"
}

// routePath is the URL path for a route under the base URL, with the trailing slash the directory-index
// files sit at ("/" stays "/").
func routePath(route string) string {
	if route == "/" {
		return "/"
	}
	return route + "/"
}

func metaProperty(property, content string) string {
	return `<meta property="` + property + `" content="` + html.EscapeString(content) + `">`
}

func metaName(name, content string) string {
	return `<meta name="` + name + `" content="` + html.EscapeString(content) + `">`
}

var (
	excerptFence = regexp.MustCompile("(?s)```.*?```")
	excerptHead  = regexp.MustCompile(`(?m)^#+\s.*$`)
	excerptImg   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	excerptLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	excerptWiki  = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	excerptPunct = regexp.MustCompile("[`*_>#|-]")
	excerptWS    = regexp.MustCompile(`\s+`)
)

// bodyExcerpt flattens the first meaningful text of a note body into one og:description-sized line:
// code fences and headings drop, links/images/emphasis reduce to their text. Mirrors the prerender's
// bodyExcerpt so both pipelines produce the same fallback description.
func bodyExcerpt(body string) string {
	t := excerptFence.ReplaceAllString(body, " ")
	t = excerptHead.ReplaceAllString(t, " ")
	t = excerptImg.ReplaceAllString(t, " ")
	t = excerptLink.ReplaceAllString(t, "$1")
	t = excerptWiki.ReplaceAllString(t, "$1")
	t = excerptPunct.ReplaceAllString(t, " ")
	t = strings.TrimSpace(excerptWS.ReplaceAllString(t, " "))
	if r := []rune(t); len(r) > 160 {
		return string(r[:157]) + "…"
	}
	return t
}
