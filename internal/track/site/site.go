// Package site builds a self-contained static site from a chosen set of notes, suitable for hosting on
// GitHub Pages or any plain file server. The site is the React web frontend (built in static mode)
// running against a pre-generated JSON bundle instead of the live `track web` server, so it keeps
// track's real reading experience — sidebar, graph, hover previews — without a backend.
//
// Build takes a selection of vault notes by id, read through the index/store, and hands them to the
// bundle writer (see bundle.go).
package site

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/task"
)

// Options selects which notes go into the static site and which one is the entry page.
type Options struct {
	Root     int64   // entry note id, the site's landing page
	IDs      []int64 // additional note ids to publish; Root is always included
	Calendar bool    // include the calendar view (and per-day pages) in the published site
	BaseURL  string  // absolute site origin for og:image / og:url in the prerender ("" omits them)
	Share    bool    // include static note sharing actions (requires BaseURL for absolute links)
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
	baseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return Result{}, err
	}
	ids := dedupIDs(append([]int64{opts.Root}, opts.IDs...))
	inSet := make(map[int64]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}

	// Paths are derived from each note's indexed file kind (the caller reindexed just before), so a
	// selection can include journals, which live under journal/ rather than note/.
	refs, err := st.AllNotes()
	if err != nil {
		return Result{}, fmt.Errorf("list notes: %w", err)
	}
	kinds := make(map[int64]string, len(refs))
	for _, ref := range refs {
		kinds[ref.NoteID] = ref.FileKind
	}

	assetSrc := cfg.AssetsDir()
	docs := make([]doc, 0, len(ids))
	upKeys := make([][]string, 0, len(ids))
	titleID := make(map[string]int64, len(ids))
	for _, id := range ids {
		n, err := note.ParseFile(cfg.PathForKind(kinds[id], id), cfg)
		if err != nil {
			return Result{}, fmt.Errorf("load note %d: %w", id, err)
		}
		body, err := export.WebBody(n.Body)
		if err != nil {
			return Result{}, fmt.Errorf("render note %d: %w", id, err)
		}
		props := note.CollectProps(n.Meta, n.Body)
		docs = append(docs, doc{
			id: id,
			// A note may pin the URL it is already published at (see slugOf).
			slug:     n.Meta.Slug,
			title:    noteTitle(n),
			kind:     n.Kind,
			tags:     n.Meta.Tags,
			days:     note.ActivityDays(n.Kind, n.Meta),
			created:  n.Meta.Created,
			mtime:    n.Mtime,
			path:     cfg.PathForKind(n.Kind, id),
			body:     body,
			keys:     []string{noteTitle(n)},
			assets:   CollectAssets(n.Body),
			desc:     n.Meta.Description,
			image:    strings.TrimPrefix(n.Meta.Image, "assets/"),
			icon:     cfg.NoteIcon(n.Kind, n.Meta.Tags, n.Meta.Icon),
			assetSrc: assetSrc,
			dataDir:  cfg.DataDir(),
			tasks:    docTasks(n.Body),
			props:    props,
		})
		upKeys = append(upKeys, note.UpTargets(props))
		titleID[noteTitle(n)] = id
	}
	// The "up" relation property resolves by title within the published set (titles are unique in a
	// vault); a parent outside the set is skipped, like every other out-of-set link.
	for i := range docs {
		for _, key := range upKeys[i] {
			if pid, ok := titleID[key]; ok {
				docs[i].up = append(docs[i].up, pid)
			}
		}
	}

	edges, grades, err := vaultEdges(st, inSet)
	if err != nil {
		return Result{}, err
	}
	for i := range docs {
		docs[i].size = grades[docs[i].id]
	}

	// The site icon (config web.icon) replaces the brand mark and favicon on the published site. A
	// configured icon that is missing fails the build: publishing a site without its brand silently
	// is worse than stopping.
	iconSrc := ""
	if cfg.WebIcon != "" {
		iconSrc = filepath.Join(cfg.VaultDir, filepath.FromSlash(cfg.WebIcon))
		if _, err := os.Stat(iconSrc); err != nil {
			return Result{}, fmt.Errorf("web.icon: %s: not found", cfg.WebIcon)
		}
	}
	return writeBundle(docs, edges, opts.Root, opts.Calendar, opts.Share, baseURL, iconSrc, cfg.Queries, frontendDir, outDir)
}

// normalizeBaseURL keeps the origin used by canonical metadata, sharing, and the sitemap in one
// form. A sitemap cannot use a relative or query-bearing base, and accepting one would produce a
// site whose crawler URLs disagree with the deployment URL the flag is meant to describe.
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("base-url must be an absolute http(s) URL (got %q)", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("base-url must not contain a query or fragment (got %q)", raw)
	}
	return strings.TrimRight(raw, "/"), nil
}

// vaultEdges returns the [[link]] edges of the index whose endpoints are both in the published set,
// plus each note's five-level graph grade. The grade is graded over the whole vault's links (not the
// published slice), which is the point of it: a note is the same size in the published graph as it
// is in the workspace it came from.
func vaultEdges(st *store.Store, inSet map[int64]bool) ([]edge, map[int64]int, error) {
	g, err := st.FullGraph()
	if err != nil {
		return nil, nil, fmt.Errorf("graph: %w", err)
	}
	var edges []edge
	for _, e := range g.Edges {
		if inSet[e.SourceID] && inSet[e.TargetID] {
			edges = append(edges, edge{src: e.SourceID, dst: e.TargetID})
		}
	}
	grades := make(map[int64]int, len(g.Nodes))
	for _, n := range g.Nodes {
		grades[n.NoteID] = n.Size
	}
	return edges, grades, nil
}

// docTasks parses a source body's task lines for the published bundle, or nil when it has none.
// Tasks parse from the raw body (not the sanitized one) so token extraction matches the live server.
func docTasks(body string) *task.Set {
	set := task.NewSet(body)
	if len(set.Items) == 0 {
		return nil
	}
	return &set
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
