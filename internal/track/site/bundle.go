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
	"time"

	"github.com/ttak0422/track/internal/track/dashboard"
	"github.com/ttak0422/track/internal/track/link"
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
	icon     string   // resolved icon shown beside the title in lists/nav ("" = none)
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
	Icon     string   `json:"icon,omitempty"`
	// Description and Image feed the prerender's og: tags; Image is the published asset path
	// (assets/<slug><ext>), so the consumer never sees the source file name.
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
}

type jsonNoteDetail struct {
	Includes []link.ResolvedInclude `json:"includes,omitempty"`
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

	// Dashboard widget data for any ```dashboard blocks in the published bodies: recent-notes titles in
	// the shared recently-updated-first order, and today's journal name. A static site rarely has a
	// journal, so the shortcut link may be unresolved — harmless, it just renders as plain text.
	dashData := dashboard.Data{JournalTitle: time.Now().Format("20060102")}
	for _, d := range listed {
		if kindOf(d) != "journal" && d.title != "" {
			dashData.RecentTitles = append(dashData.RecentTitles, d.title)
		}
	}
	if err := writeJSONFile(filepath.Join(outDir, "data", "notes.json"), map[string]any{"notes": notes}); err != nil {
		return Result{}, err
	}

	// note/<id>.json with backlinks derived from edges, each list in the shared order.
	linkers := map[int64][]doc{}
	byID := map[int64]doc{}
	// keyDocs resolves a [[key]] to its published doc, for include extraction — the bundle's
	// counterpart of the live server's keyword dictionary. Only in-set docs resolve, so an include
	// of an unpublished note renders as unresolved rather than leaking its content.
	keyDocs := map[string]doc{}
	for _, d := range docs {
		byID[d.id] = d
		for _, k := range d.keys {
			if k != "" {
				keyDocs[k] = d
			}
		}
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
		// Resolve ```dashboard widget blocks to Markdown (recent/journal/pinned lists) at build time, so
		// a published home note shows the same landing view the live workspace does.
		body = dashboard.Resolve(body, dashData)
		// Then resolve ```viewspec fences to ready-to-draw ```echarts option blocks at build time.
		body = resolveViewSpecBlocks(body, d.dataDir, noteSlug)
		resp := jsonNoteResponse{
			Note: jsonNoteDetail{
				// Includes resolve against the published body so their line numbers match what the
				// frontend renders. Target ids stay unpublished (0): the embed header navigates by
				// key through resolve.json, like every other link on the static site.
				// ponytail: a viewspec fence inside an embedded region shows as source in static
				// mode (targets skip resolveViewSpecBlocks); resolve per-target if that ever matters.
				Includes: link.ResolveIncludes(body, func(key string) (int64, string, string, bool) {
					t, ok := keyDocs[key]
					if !ok {
						return 0, "", "", false
					}
					return 0, kindOf(t), rewriteAssetRefs(t.body), true
				}),
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
	siteMeta := jsonSite{Root: PublishID(root), Title: rootTitle, Calendar: calendar, BaseURL: strings.TrimRight(baseURL, "/")}
	if err := writeJSONFile(filepath.Join(outDir, "data", "site.json"), siteMeta); err != nil {
		return Result{}, err
	}

	// Copy the static frontend over the output (index.html + assets/...).
	if err := copyTree(frontendDir, outDir); err != nil {
		return Result{}, fmt.Errorf("copy frontend: %w", err)
	}
	// Emit a real HTML file per route (start page, per note, and the site-level pages) with that page's
	// OGP meta injected into the copied shell, so crawlers/social shares see per-note metadata and deep
	// links resolve without a host fallback.
	if err := writePages(outDir, PublishID(root), root, docs, listed, siteMeta); err != nil {
		return Result{}, fmt.Errorf("write pages: %w", err)
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
	out := jsonSearchResult{NoteID: PublishID(d.id), FileKind: kindOf(d), Path: "", Title: d.title, Tags: d.tags, Days: d.days, Icon: d.icon, Description: d.desc}
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
