// Package site builds a self-contained static HTML site from a chosen set of notes, suitable for
// hosting on GitHub Pages or any plain file server. It is the SSG counterpart to the single-note
// Markdown exporter: notes are rendered to standalone HTML pages, [[...]] links between selected
// notes become navigable anchors, and links to anything outside the selection are flattened to inert
// text. There is no live index, heatmap, or editor — the published output is rendered content only.
//
// The note body is transformed in two stages: the export pipeline (internal/track/export) rewrites
// track-specific spans into intermediate Markdown, then goldmark turns that Markdown into HTML. The
// selection is an explicit set of note ids with one designated root that becomes index.html.
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

// Build renders the selected notes as a static HTML site under outDir, creating the directory if
// needed. The root note is written as index.html; every other note becomes "<id>.html". A shared
// style.css is written alongside the pages.
func Build(cfg *config.Config, resolve Resolver, opts Options, outDir string) (Result, error) {
	if opts.Root == 0 {
		return Result{}, fmt.Errorf("root note id is required")
	}
	ids := dedupIDs(append([]int64{opts.Root}, opts.IDs...))
	inSet := make(map[int64]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create out dir: %w", err)
	}

	md := newMarkdown()
	renderer := siteRenderer{resolve: resolve, inSet: inSet, root: opts.Root}

	res := Result{OutDir: outDir}
	assetRefs := map[string]bool{}
	for _, id := range ids {
		n, err := note.ParseFile(cfg.NotePath(id), cfg)
		if err != nil {
			return Result{}, fmt.Errorf("load note %d: %w", id, err)
		}
		for _, rel := range collectAssets(n.Body) {
			assetRefs[rel] = true
		}
		ex, err := export.Export(n, renderer, export.Options{})
		if err != nil {
			return Result{}, fmt.Errorf("render note %d: %w", id, err)
		}
		var body bytes.Buffer
		if err := md.Convert([]byte(ex.Markdown), &body); err != nil {
			return Result{}, fmt.Errorf("markdown note %d: %w", id, err)
		}
		page := pageName(id, opts.Root)
		full := renderPage(noteTitle(n), body.String(), id != opts.Root)
		if err := os.WriteFile(filepath.Join(outDir, page), []byte(full), 0o644); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", page, err)
		}
		res.Pages = append(res.Pages, page)
	}

	if len(assetRefs) > 0 {
		rels := make([]string, 0, len(assetRefs))
		for rel := range assetRefs {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		copied, missing, err := copyAssets(cfg.AssetsDirForKind(config.KindNote), outDir, rels)
		if err != nil {
			return Result{}, fmt.Errorf("copy assets: %w", err)
		}
		res.Assets = copied
		res.Missing = missing
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

// pageName is the output filename for a note: the root note owns index.html, every other note gets
// its id-based page so cross-links between selected notes are stable and relative.
func pageName(id, root int64) string {
	if id == root {
		return "index.html"
	}
	return strconv.FormatInt(id, 10) + ".html"
}

func noteTitle(n *note.Note) string {
	if t := n.Meta.Title; t != "" {
		return t
	}
	return strconv.FormatInt(n.ID, 10)
}

func renderPage(title, body string, showHome bool) string {
	var nav string
	if showHome {
		nav = "<nav class=\"site-nav\"><a href=\"index.html\">← home</a></nav>\n"
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
