package site

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
)

// BuildDir renders a directory of plain Markdown files as a static HTML site. It is the repo-mounted
// counterpart to Build: the source is ordinary ".md" files (e.g. bundled help/docs) that live outside
// any vault, so there is no index. Each file becomes a page slugged by its base name; wiki links
// resolve by base name or by the file's first H1 title among the directory's files.
//
// rootName names the entry file (with or without the ".md" extension); it becomes index.html. When
// empty, "index.md" is used if present. An "assets" subdirectory next to the files supplies any
// referenced "assets/<path>" media.
func BuildDir(srcDir, rootName, outDir string) (Result, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return Result{}, fmt.Errorf("read src dir: %w", err)
	}

	assetSrc := filepath.Join(srcDir, "assets")
	var pages []page
	titleToSlug := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		slug := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		body := string(raw)
		title := firstHeading(body)
		if title == "" {
			title = slug
		}
		pages = append(pages, page{
			slug:     slug,
			title:    title,
			note:     &note.Note{Body: body, Meta: note.Metadata{Title: title}},
			assetSrc: assetSrc,
		})
		titleToSlug[slug] = slug
		titleToSlug[title] = slug
	}
	if len(pages) == 0 {
		return Result{}, fmt.Errorf("no .md files found in %s", srcDir)
	}

	rootSlug := strings.TrimSuffix(rootName, filepath.Ext(rootName))
	if rootSlug == "" {
		rootSlug = "index"
	}
	if _, ok := slugSet(pages)[rootSlug]; !ok {
		return Result{}, fmt.Errorf("root %q not found among .md files in %s", rootSlug, srcDir)
	}

	// Deterministic output, with the root first.
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].slug == rootSlug {
			return true
		}
		if pages[j].slug == rootSlug {
			return false
		}
		return pages[i].slug < pages[j].slug
	})

	resolve := func(key string) (string, bool) {
		slug, ok := titleToSlug[key]
		return slug, ok
	}
	return build(pages, resolve, rootSlug, outDir)
}

// firstHeading returns the text of the first level-1 ATX heading in body, or "" when there is none.
func firstHeading(body string) string {
	for _, h := range link.Headings(body) {
		if h.Level == 1 {
			return h.Text
		}
	}
	return ""
}

func slugSet(pages []page) map[string]bool {
	out := make(map[string]bool, len(pages))
	for _, p := range pages {
		out[p.slug] = true
	}
	return out
}
