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

// BuildDir publishes a directory of plain Markdown files (e.g. repo-mounted help/docs that live outside
// any vault). Each file becomes a published note slugged by its base name; ids are assigned in name
// order. Wiki links resolve by file base name or by the file's first H1 title among the directory's
// files. rootName names the entry file (with or without ".md"); empty defaults to "index". An "assets"
// subdirectory supplies referenced media. frontendDir is the static-mode frontend build.
func BuildDir(srcDir, rootName, frontendDir, outDir string) (Result, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return Result{}, fmt.Errorf("read src dir: %w", err)
	}

	type file struct {
		slug  string
		title string
		body  string // raw
	}
	var files []file
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		slug := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		title := firstHeading(string(raw))
		if title == "" {
			title = slug
		}
		files = append(files, file{slug: slug, title: title, body: string(raw)})
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("no .md files found in %s", srcDir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].slug < files[j].slug })

	// Assign ids in name order and build the key -> id map (base name and title both resolve).
	keyToID := map[string]int64{}
	idForSlug := map[string]int64{}
	for i, f := range files {
		id := int64(i + 1)
		idForSlug[f.slug] = id
		keyToID[f.slug] = id
		keyToID[f.title] = id
	}

	rootSlug := strings.TrimSuffix(rootName, filepath.Ext(rootName))
	if rootSlug == "" {
		rootSlug = "index"
	}
	root, ok := idForSlug[rootSlug]
	if !ok {
		return Result{}, fmt.Errorf("root %q not found among .md files in %s", rootSlug, srcDir)
	}

	assetSrc := filepath.Join(srcDir, "assets")
	var docs []doc
	var edges []edge
	seenEdge := map[edge]bool{}
	for _, f := range files {
		id := idForSlug[f.slug]
		body, err := sanitize(&note.Note{ID: id, Kind: "note", Body: f.body, Meta: note.Metadata{Title: f.title}})
		if err != nil {
			return Result{}, fmt.Errorf("render %s: %w", f.slug, err)
		}
		docs = append(docs, doc{
			id:       id,
			title:    f.title,
			kind:     "note",
			path:     f.slug + ".md",
			body:     body,
			keys:     []string{f.slug, f.title},
			assets:   collectAssets(f.body),
			assetSrc: assetSrc,
		})
		for _, ref := range link.Refs(f.body) {
			if dst, ok := keyToID[ref.Text]; ok {
				e := edge{src: id, dst: dst}
				if !seenEdge[e] {
					seenEdge[e] = true
					edges = append(edges, e)
				}
			}
		}
	}

	return writeBundle(docs, edges, root, frontendDir, outDir)
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
