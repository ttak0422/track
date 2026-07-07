package site

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The static site is the React web frontend running against a pre-generated JSON bundle instead of the
// live `track web` server. This file builds that bundle: it mirrors the server's /api/* response shapes
// (see internal/track/webui handlers and web/src/types.ts) as static files under <out>/data, copies the
// static-mode frontend build over it, and copies referenced assets. Both input front-ends (a vault
// selection and a Markdown directory) reduce to the same in-memory model below.

// doc is one published note in the bundle.
type doc struct {
	id       int64
	title    string
	kind     string // "note" or "journal"
	tags     []string
	days     []string // activity days (YYYY-MM-DD) from the sidecar; journals carry none
	mtime    int64    // file mtime, for the shared recently-updated-first listing order (0 in dir mode)
	path     string   // source/display path (informational in the static site)
	body     string   // web-sanitized Markdown the frontend renders
	keys     []string // resolution keys ([[key]]) that point at this doc (title, file name, …)
	assets   []string // "assets/<rel>" references in the body
	assetSrc string   // directory those assets are copied from
	desc     string   // page summary (sidecar description), published as og:description
	image    string   // cover image, relative under assets/ ("" = none), published as og:image
	dataDir  string   // canonical-data directory for embedded ```viewspec charts ("" = inline data only)
}

// edge is a directed [[link]] between two in-set docs.
type edge struct{ src, dst int64 }

// JSON shapes matching web/src/types.ts. Kept local so the bundle is self-describing and decoupled from
// the store/webui structs.

// Ids are the opaque published slugs (see PublishID), not the internal note ids, so the bundle never
// exposes the timestamp-based source file names.
type jsonRef struct {
	NoteID   string `json:"note_id"`
	FileKind string `json:"file_kind"`
	Path     string `json:"path,omitempty"`
	Title    string `json:"title"`
}

type jsonSearchResult struct {
	NoteID   string   `json:"note_id"`
	FileKind string   `json:"file_kind"`
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Days     []string `json:"days,omitempty"`
	// Description and Image feed the prerender's og: tags; Image is the published asset path
	// (assets/<slug><ext>), so the consumer never sees the source file name.
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
}

type jsonNoteDetail struct {
	jsonSearchResult
	CopyPath string `json:"copy_path"`
	Body     string `json:"body"`
	ETag     string `json:"etag"`
}

type jsonNoteResponse struct {
	Note      jsonNoteDetail `json:"note"`
	Backlinks []jsonRef      `json:"backlinks"`
}

type jsonGraphNode struct {
	NoteID   string `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
}

type jsonGraphEdge struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
}

type jsonGraph struct {
	CenterID string          `json:"center_id"`
	Nodes    []jsonGraphNode `json:"nodes"`
	Edges    []jsonGraphEdge `json:"edges"`
}

type jsonSite struct {
	Root  string `json:"root"`
	Title string `json:"title"`
	// Calendar opts the published site into the calendar view: the frontend shows the rail button and
	// the prerender emits /calendar and the per-day pages. Off suits reference sites (help docs); on
	// suits activity-shaped ones (a blog over a vault).
	Calendar bool `json:"calendar,omitempty"`
	// BaseURL is the site's absolute origin (export-site --base-url, no trailing slash). The
	// prerender needs it for og:image / og:url, which must be absolute; empty omits those tags.
	BaseURL string `json:"base_url,omitempty"`
}

// writeBundle emits the data bundle, copies the static frontend over it, and copies assets. frontendDir
// is the static-mode Vite build (index.html + assets/...). root is the entry note's id.
func writeBundle(docs []doc, edges []edge, root int64, calendar bool, baseURL, frontendDir, outDir string) (Result, error) {
	if len(docs) == 0 {
		return Result{}, fmt.Errorf("no notes to publish")
	}
	if err := os.MkdirAll(filepath.Join(outDir, "data", "note"), 0o755); err != nil {
		return Result{}, fmt.Errorf("create out dir: %w", err)
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].id < docs[j].id })
	rootTitle := ""
	for _, d := range docs {
		if d.id == root {
			rootTitle = d.title
		}
	}

	// notes.json, in the shared note-list order (recently updated first) so the published calendar,
	// day pages, and search listing read like the live server's.
	listed := append([]doc(nil), docs...)
	byRecency(listed)
	notes := make([]jsonSearchResult, 0, len(listed))
	for _, d := range listed {
		notes = append(notes, searchResultOf(d))
	}
	if err := writeJSONFile(filepath.Join(outDir, "data", "notes.json"), map[string]any{"notes": notes}); err != nil {
		return Result{}, err
	}

	// note/<id>.json with backlinks derived from edges, each list in the shared order.
	linkers := map[int64][]doc{}
	byID := map[int64]doc{}
	for _, d := range docs {
		byID[d.id] = d
	}
	// Chart datums reference vault notes by internal id; published charts must carry the opaque slug
	// instead (and drop references to notes outside the set — no dangling navigation).
	noteSlug := func(ref string) (string, bool) {
		id, err := strconv.ParseInt(ref, 10, 64)
		if err != nil {
			return "", false
		}
		if _, ok := byID[id]; !ok {
			return "", false
		}
		return PublishID(id), true
	}
	for _, e := range edges {
		src, ok := byID[e.src]
		if !ok {
			continue
		}
		linkers[e.dst] = append(linkers[e.dst], src)
	}
	for _, d := range docs {
		srcs := linkers[d.id]
		byRecency(srcs)
		bl := make([]jsonRef, 0, len(srcs))
		for _, src := range srcs {
			bl = append(bl, refOf(src))
		}
		// Rewrite asset references to their published (slugged) names, matching the copied files.
		body := rewriteAssetRefs(d.body)
		// Then resolve ```viewspec fences to ready-to-draw ```echarts option blocks at build time.
		body = resolveViewSpecBlocks(body, d.dataDir, noteSlug)
		resp := jsonNoteResponse{
			Note: jsonNoteDetail{
				jsonSearchResult: searchResultOf(d),
				CopyPath:         "", // see searchResultOf: the source path is intentionally not published.
				Body:             body,
				ETag:             etag(body),
			},
			Backlinks: bl,
		}
		if err := writeJSONFile(filepath.Join(outDir, "data", "note", fmt.Sprintf("%s.json", PublishID(d.id))), resp); err != nil {
			return Result{}, err
		}
	}

	// graph.json (whole published set).
	nodes := make([]jsonGraphNode, 0, len(docs))
	for _, d := range docs {
		nodes = append(nodes, jsonGraphNode{NoteID: PublishID(d.id), FileKind: kindOf(d), Title: d.title})
	}
	gEdges := make([]jsonGraphEdge, 0, len(edges))
	for _, e := range edges {
		gEdges = append(gEdges, jsonGraphEdge{SourceID: PublishID(e.src), TargetID: PublishID(e.dst)})
	}
	// CenterID is empty for the whole-set graph: there is no centered node, and no slug ever equals "".
	if err := writeJSONFile(filepath.Join(outDir, "data", "graph.json"),
		map[string]any{"graph": jsonGraph{CenterID: "", Nodes: nodes, Edges: gEdges}}); err != nil {
		return Result{}, err
	}

	// resolve.json: every key that should navigate to a published note.
	resolve := map[string]jsonRef{}
	for _, d := range docs {
		ref := refOf(d)
		for _, k := range d.keys {
			if k != "" {
				resolve[k] = ref
			}
		}
	}
	if err := writeJSONFile(filepath.Join(outDir, "data", "resolve.json"), resolve); err != nil {
		return Result{}, err
	}

	// site.json: the entry note and site-level toggles.
	if err := writeJSONFile(
		filepath.Join(outDir, "data", "site.json"),
		jsonSite{Root: PublishID(root), Title: rootTitle, Calendar: calendar, BaseURL: strings.TrimRight(baseURL, "/")},
	); err != nil {
		return Result{}, err
	}

	// Copy the static frontend over the output (index.html + assets/...).
	if err := copyTree(frontendDir, outDir); err != nil {
		return Result{}, fmt.Errorf("copy frontend: %w", err)
	}
	if err := finalizeIndex(filepath.Join(outDir, "index.html"), PublishID(root)); err != nil {
		return Result{}, fmt.Errorf("finalize index.html: %w", err)
	}

	// Copy referenced note assets.
	res := Result{OutDir: outDir}
	for _, d := range docs {
		res.Notes = append(res.Notes, d.id)
	}
	bySrc := map[string]map[string]bool{}
	for _, d := range docs {
		rels := d.assets
		if d.image != "" {
			// The cover image is published even when the body never references it.
			rels = append(append([]string(nil), rels...), d.image)
		}
		for _, rel := range rels {
			if bySrc[d.assetSrc] == nil {
				bySrc[d.assetSrc] = map[string]bool{}
			}
			bySrc[d.assetSrc][rel] = true
		}
	}
	for _, src := range sortedKeys(bySrc) {
		rels := make([]string, 0, len(bySrc[src]))
		for rel := range bySrc[src] {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		copied, missing, err := copyAssets(src, outDir, rels, noteSlug)
		if err != nil {
			return Result{}, fmt.Errorf("copy assets: %w", err)
		}
		res.Assets = append(res.Assets, copied...)
		res.Missing = append(res.Missing, missing...)
	}
	return res, nil
}

// The source path is dropped from the bundle: like the id, the file name is timestamp-based, so emitting
// it would re-expose what the slug is meant to hide. It was only informational in the static site.
func searchResultOf(d doc) jsonSearchResult {
	out := jsonSearchResult{NoteID: PublishID(d.id), FileKind: kindOf(d), Path: "", Title: d.title, Tags: d.tags, Days: d.days, Description: d.desc}
	if d.image != "" {
		out.Image = "assets/" + publishAssetName(d.image)
	}
	return out
}

func refOf(d doc) jsonRef {
	return jsonRef{NoteID: PublishID(d.id), FileKind: kindOf(d), Title: d.title}
}

// byRecency sorts docs into the one note-list order every surface shares (see webui's sortRefs):
// most recently updated first, id ascending on ties — which in dir mode (all mtimes zero) keeps the
// name-derived id order.
func byRecency(ds []doc) {
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].mtime != ds[j].mtime {
			return ds[i].mtime > ds[j].mtime
		}
		return ds[i].id < ds[j].id
	})
}

func kindOf(d doc) string {
	if d.kind == "" {
		return "note"
	}
	return d.kind
}

func etag(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:16])
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// finalizeIndex substitutes the placeholders the live server fills in at request time. The static site
// has no server, so the default theme falls back to "system" and there are no color overrides; left
// unsubstituted, __TRACK_COLOR_OVERRIDES__ would otherwise show as literal text in the page. startPage is
// the root note's published id, baked in so the frontend redirects to the start page on launch without a
// site.json round-trip (see web/src/runtime.ts START_PAGE_ID).
func finalizeIndex(path, startPage string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	html := strings.ReplaceAll(string(raw), "__TRACK_DEFAULT_THEME__", "system")
	html = strings.ReplaceAll(html, "__TRACK_COLOR_OVERRIDES__", "")
	html = strings.ReplaceAll(html, "__TRACK_START_PAGE__", startPage)
	return os.WriteFile(path, []byte(html), 0o644)
}

// copyTree copies every file under src into dst, preserving the relative layout. Dot-prefixed entries
// (e.g. .DS_Store, .vite build metadata) are skipped so they never leak into the published site.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != src {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}
