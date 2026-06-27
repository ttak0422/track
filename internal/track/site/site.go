// Package site builds a self-contained static HTML site from a chosen set of notes, suitable for
// hosting on GitHub Pages or any plain file server. It is the SSG counterpart to the single-note
// Markdown exporter: notes are rendered to standalone HTML pages, [[...]] links between selected
// notes become navigable anchors, and links to anything outside the selection are flattened to inert
// text. There is no live index, heatmap, or editor — the published output is rendered content only.
//
// Each note body is transformed in two stages: the export pipeline (internal/track/export) rewrites
// track-specific spans into intermediate Markdown, then goldmark turns that Markdown into HTML.
//
// Two input front-ends share one rendering core:
//   - Build:    a selection of vault notes by id (resolved through the index).
//   - BuildDir: a directory of plain Markdown files, for repo-mounted help/docs that live outside any
//     vault. Wiki links resolve by filename among the directory's files.
//
// Internally a page is keyed by a string slug (the note id, or a file's base name); the root slug owns
// index.html.
package site

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/note"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// Resolver maps a wiki-link key (a note title) to a note id, reporting whether it resolved. The
// caller supplies it (normally backed by the index) so the site package stays store-free.
type Resolver func(key string) (int64, bool)

// Options selects which notes go into the static site and which one is the entry page.
type Options struct {
	Root int64   // entry note id, rendered as index.html
	IDs  []int64 // additional note ids to publish; Root is always included
}

// Result reports what a Build produced.
type Result struct {
	OutDir string   `json:"out"`
	Pages  []string `json:"pages"`            // generated HTML filenames, in publish order
	Assets []string `json:"assets,omitempty"` // asset paths copied under <out>/assets
	// Missing lists referenced assets whose source file was not found; the build still succeeds.
	Missing []string `json:"missing_assets,omitempty"`
}

// page is one document to publish, keyed by a slug. The body still carries track syntax; assetSrc is
// the directory its "assets/<path>" references resolve against.
type page struct {
	slug     string
	title    string
	note     *note.Note
	assetSrc string
}

// Build renders the selected vault notes as a static HTML site under outDir. The root note is written
// as index.html; every other note becomes "<id>.html". A shared style.css is written alongside.
func Build(cfg *config.Config, resolve Resolver, opts Options, outDir string) (Result, error) {
	if opts.Root == 0 {
		return Result{}, fmt.Errorf("root note id is required")
	}
	ids := dedupIDs(append([]int64{opts.Root}, opts.IDs...))

	assetSrc := cfg.AssetsDirForKind(config.KindNote)
	pages := make([]page, 0, len(ids))
	for _, id := range ids {
		n, err := note.ParseFile(cfg.NotePath(id), cfg)
		if err != nil {
			return Result{}, fmt.Errorf("load note %d: %w", id, err)
		}
		pages = append(pages, page{slug: idSlug(id), title: noteTitle(n), note: n, assetSrc: assetSrc})
	}

	// Adapt the id-based resolver to the slug-based core: a key resolves to a published page only when
	// its target id is part of the selection.
	slugResolve := func(key string) (string, bool) {
		if resolve == nil {
			return "", false
		}
		id, ok := resolve(key)
		if !ok {
			return "", false
		}
		return idSlug(id), true
	}

	return build(pages, slugResolve, idSlug(opts.Root), outDir)
}

// build is the shared rendering core: it writes one HTML page per document, copies referenced assets,
// and emits the stylesheet. rootSlug names the page that becomes index.html.
func build(pages []page, resolve func(key string) (string, bool), rootSlug, outDir string) (Result, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create out dir: %w", err)
	}

	inSet := make(map[string]bool, len(pages))
	for _, p := range pages {
		inSet[p.slug] = true
	}

	md := newMarkdown()
	renderer := siteRenderer{resolve: resolve, inSet: inSet, rootSlug: rootSlug}

	res := Result{OutDir: outDir}
	assetRefs := map[string]map[string]bool{} // assetSrc dir -> set of referenced rel paths
	for _, p := range pages {
		for _, rel := range collectAssets(p.note.Body) {
			if assetRefs[p.assetSrc] == nil {
				assetRefs[p.assetSrc] = map[string]bool{}
			}
			assetRefs[p.assetSrc][rel] = true
		}
		ex, err := export.Export(p.note, renderer, export.Options{})
		if err != nil {
			return Result{}, fmt.Errorf("render %s: %w", p.slug, err)
		}
		var body bytes.Buffer
		if err := md.Convert([]byte(ex.Markdown), &body); err != nil {
			return Result{}, fmt.Errorf("markdown %s: %w", p.slug, err)
		}
		bodyHTML, hasMermaid := transformMermaid(body.String())
		file := pageFile(p.slug, rootSlug)
		full := renderPage(p.title, bodyHTML, p.slug != rootSlug, hasMermaid)
		if err := os.WriteFile(filepath.Join(outDir, file), []byte(full), 0o644); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", file, err)
		}
		res.Pages = append(res.Pages, file)
	}

	for _, src := range sortedKeys(assetRefs) {
		rels := make([]string, 0, len(assetRefs[src]))
		for rel := range assetRefs[src] {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		copied, missing, err := copyAssets(src, outDir, rels)
		if err != nil {
			return Result{}, fmt.Errorf("copy assets: %w", err)
		}
		res.Assets = append(res.Assets, copied...)
		res.Missing = append(res.Missing, missing...)
	}

	if err := os.WriteFile(filepath.Join(outDir, "style.css"), []byte(styleCSS), 0o644); err != nil {
		return Result{}, fmt.Errorf("write style.css: %w", err)
	}
	return res, nil
}

// newMarkdown builds the Markdown-to-HTML converter: GFM (tables, task lists, strikethrough,
// autolinks) plus stable heading ids. Raw HTML in note bodies is preserved (WithUnsafe) because the
// content is the user's own vault, not untrusted input.
func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
}

// idSlug is the slug for a vault note: its decimal id.
func idSlug(id int64) string { return strconv.FormatInt(id, 10) }

// pageFile is the output filename for a slug: the root slug owns index.html, every other slug gets its
// own "<slug>.html" so cross-links between published pages are stable and relative.
func pageFile(slug, rootSlug string) string {
	if slug == rootSlug {
		return "index.html"
	}
	return slug + ".html"
}

func noteTitle(n *note.Note) string {
	if t := n.Meta.Title; t != "" {
		return t
	}
	return strconv.FormatInt(n.ID, 10)
}

func renderPage(title, body string, showHome, hasMermaid bool) string {
	var nav string
	if showHome {
		nav = "<nav class=\"site-nav\"><a href=\"index.html\">← home</a></nav>\n"
	}
	var script string
	if hasMermaid {
		script = mermaidScript
	}
	return "<!DOCTYPE html>\n" +
		"<html lang=\"ja\">\n" +
		"<head>\n" +
		"<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + html.EscapeString(title) + "</title>\n" +
		"<link rel=\"stylesheet\" href=\"style.css\">\n" +
		"</head>\n" +
		"<body>\n" +
		"<main class=\"note\">\n" +
		nav +
		"<article>\n" + body + "</article>\n" +
		"</main>\n" +
		script +
		"</body>\n" +
		"</html>\n"
}

func dedupIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func sortedKeys(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
