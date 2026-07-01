// Package site builds a self-contained static site from a chosen set of notes, suitable for hosting on
// GitHub Pages or any plain file server. The site is the React web frontend (built in static mode)
// running against a pre-generated JSON bundle instead of the live `track web` server, so it keeps
// track's real reading experience — sidebar, graph, hover previews — without a backend.
//
// Two input front-ends share one bundle writer (see bundle.go):
//   - Build:    a selection of vault notes by id, read through the index/store.
//   - BuildDir: a directory of plain Markdown files, for repo-mounted help/docs outside any vault.
package site

import (
	"fmt"
	"sort"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// Options selects which notes go into the static site and which one is the entry page.
type Options struct {
	Root int64   // entry note id, the site's landing page
	IDs  []int64 // additional note ids to publish; Root is always included
}

// Result reports what a build produced.
type Result struct {
	OutDir  string   `json:"out"`
	Notes   []int64  `json:"notes"`            // published note ids
	Assets  []string `json:"assets,omitempty"` // asset paths copied under <out>/assets
	Missing []string `json:"missing_assets,omitempty"`
}

// Build publishes the selected vault notes. frontendDir is the static-mode frontend build copied into
// the output; outDir receives the data bundle, frontend, and assets.
func Build(cfg *config.Config, st *store.Store, opts Options, frontendDir, outDir string) (Result, error) {
	if opts.Root == 0 {
		return Result{}, fmt.Errorf("root note id is required")
	}
	ids := dedupIDs(append([]int64{opts.Root}, opts.IDs...))
	inSet := make(map[int64]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}

	assetSrc := cfg.AssetsDir()
	docs := make([]doc, 0, len(ids))
	for _, id := range ids {
		n, err := note.ParseFile(cfg.NotePath(id), cfg)
		if err != nil {
			return Result{}, fmt.Errorf("load note %d: %w", id, err)
		}
		body, err := sanitize(n)
		if err != nil {
			return Result{}, fmt.Errorf("render note %d: %w", id, err)
		}
		docs = append(docs, doc{
			id:       id,
			title:    noteTitle(n),
			kind:     n.Kind,
			tags:     n.Meta.Tags,
			path:     cfg.PathForKind(n.Kind, id),
			body:     body,
			keys:     []string{noteTitle(n)},
			assets:   collectAssets(n.Body),
			assetSrc: assetSrc,
		})
	}

	edges, err := vaultEdges(st, inSet)
	if err != nil {
		return Result{}, err
	}
	return writeBundle(docs, edges, opts.Root, frontendDir, outDir)
}

// vaultEdges returns the [[link]] edges of the index whose endpoints are both in the published set.
func vaultEdges(st *store.Store, inSet map[int64]bool) ([]edge, error) {
	g, err := st.FullGraph()
	if err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	var edges []edge
	for _, e := range g.Edges {
		if inSet[e.SourceID] && inSet[e.TargetID] {
			edges = append(edges, edge{src: e.SourceID, dst: e.TargetID})
		}
	}
	return edges, nil
}

// sanitize renders a note body into the Markdown the frontend expects: wiki links are kept for the
// frontend to resolve, action links are flattened, and code blocks become plain fences. This is the
// same transform the live server applies in /api/render, so a published note reads identically.
func sanitize(n *note.Note) (string, error) {
	res, err := export.Export(n, export.NewWebRenderer(), export.Options{})
	if err != nil {
		return "", err
	}
	return res.Markdown, nil
}

func noteTitle(n *note.Note) string {
	if t := n.Meta.Title; t != "" {
		return t
	}
	return fmt.Sprintf("%d", n.ID)
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
