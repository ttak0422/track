package site

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dashboard"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/query"
	"github.com/ttak0422/track/internal/track/task"
)

// The static site is the React web frontend running against a pre-generated JSON bundle instead of the
// live `track web` server. This file builds that bundle: it mirrors the server's /api/* response shapes
// (see internal/track/webui handlers and web/src/types.ts) as static files under <out>/data, copies the
// static-mode frontend build over it, and copies referenced assets. Both input front-ends (a vault
// selection and a Markdown directory) reduce to the same in-memory model below.

// doc is one published note in the bundle.
type doc struct {
	id int64
	// slug pins the published address; empty derives it from the id (see slugOf).
	slug     string
	title    string
	kind     string // "note" or "journal"
	tags     []string
	days     []string    // activity days (YYYY-MM-DD) from the sidecar; journals carry none
	created  string      // sidecar creation date, verbatim in the vault's date format ("" = none)
	mtime    int64       // file mtime, for the shared recently-updated-first listing order (0 in dir mode)
	path     string      // source/display path (informational in the static site)
	body     string      // web-sanitized Markdown the frontend renders
	keys     []string    // resolution keys ([[key]]) that point at this doc (title, file name, …)
	assets   []string    // "assets/<rel>" references in the body
	assetSrc string      // directory those assets are copied from
	desc     string      // page summary (sidecar description), published as og:description
	image    string      // cover image, relative under assets/ ("" = none), published as og:image
	icon     string      // resolved icon shown beside the title in lists/nav ("" = none)
	dataDir  string      // canonical-data directory for embedded ```viewspec charts ("" = inline data only)
	tasks    *task.Set   // parsed task lines + state set, for the read-only board (nil = none)
	props    []note.Prop // flattened typed properties (sidecar props + inline fields), shown read-only
	up       []int64     // ids of the doc's parents ("up" relation), resolved by the input front-end; out-of-set ids are skipped
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

// jsonTaskRow mirrors store.TaskRow on the wire, so the published calendar and day pages read dated
// tasks with the same frontend code the live server feeds. The note id is the published slug, like
// every other id in the bundle.
type jsonTaskRow struct {
	NoteID   string `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
	task.Task
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

// jsonSearchDoc is one note in the full-text corpus: its published slug and its published body.
type jsonSearchDoc struct {
	NoteID string `json:"note_id"`
	Body   string `json:"body"`
}

type jsonNoteDetail struct {
	Includes []link.ResolvedInclude `json:"includes,omitempty"`
	jsonSearchResult
	CopyPath string `json:"copy_path"`
	// Created and Updated mirror the live server's note timestamps: the sidecar date string verbatim
	// and the file mtime in unix seconds.
	Created string `json:"created,omitempty"`
	Updated int64  `json:"updated,omitempty"`
	// Props mirrors the live server's flattened note properties; link values stay resolution keys,
	// which the frontend resolves through resolve.json like any other wiki link.
	Props []note.Prop `json:"props,omitempty"`
	Body  string      `json:"body"`
	ETag  string      `json:"etag"`
	// Tasks feeds the read-only task board (```taskboard) on the published site.
	Tasks *task.Set `json:"tasks,omitempty"`
}

type jsonNoteResponse struct {
	Note      jsonNoteDetail `json:"note"`
	Backlinks []jsonRef      `json:"backlinks"`
	// Trail and Children mirror the live server's hierarchy navigation, derived from each doc's "up"
	// relation property resolved within the published set.
	Trail    []jsonRef `json:"trail"`
	Children []jsonRef `json:"children"`
}

// jsonHierarchyNode mirrors store.HierarchyNode: a published reference plus the notes below it.
type jsonHierarchyNode struct {
	jsonRef
	Children []jsonHierarchyNode `json:"children,omitempty"`
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
	// Share opts the static note reader into showing its X and copy-link actions. It is deliberately
	// opt-in because the track documentation site does not need publishing controls.
	Share bool `json:"share,omitempty"`
	// Icon is the published site icon's file name at the site root ("icon.<ext>", from config
	// web.icon). The frontend shows it as the brand mark; the favicon swap bakes it into every page.
	// Empty keeps the built-in track mark.
	Icon string `json:"icon,omitempty"`
}

// writeBundle emits the data bundle, copies the static frontend over it, and copies assets. frontendDir
// is the static-mode Vite build (index.html + assets/...). root is the entry note's id. saved supplies
// the named queries a ```track-query fence may reference (nil on a directory site, which has no config).
// iconSrc is the resolved site-icon file to publish as icon.<ext> at the site root ("" = none).
func writeBundle(docs []doc, edges []edge, root int64, calendar, share bool, baseURL, iconSrc string, saved map[string]string, frontendDir, outDir string) (Result, error) {
	if len(docs) == 0 {
		return Result{}, fmt.Errorf("no notes to publish")
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].id < docs[j].id })
	rootTitle, rootSlug := "", ""
	for i := range docs {
		if docs[i].id == root {
			rootTitle = docs[i].title
			rootSlug = slugOf(&docs[i])
		}
	}
	// Every data file below is written locked (see lock.go). The key is derived from the site's address
	// and travels in the page, so the app opens its data and a bulk consumer has to unlock deliberately.
	// The writer also stages the files and fingerprints them, so the bundle publishes under a path that
	// changes with its content (ADR 0070).
	bundle, err := newBundleWriter(outDir, LockKey(strings.TrimRight(baseURL, "/"), rootSlug))
	if err != nil {
		return Result{}, err
	}
	// Published asset names are addressed by content, so every surface that names one — a rewritten
	// body, a cover in the listing, the copy itself — resolves it here (ADR 0070).
	names := newAssetNamer()

	// notes.json, in the shared note-list order (recently updated first) so the published calendar,
	// day pages, and search listing read like the live server's.
	listed := append([]doc(nil), docs...)
	byRecency(listed)
	notes := make([]jsonSearchResult, 0, len(listed))
	for _, d := range listed {
		notes = append(notes, searchResultOf(d, names))
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
	if err := bundle.writeJSON("notes.json", map[string]any{"notes": notes}); err != nil {
		return Result{}, err
	}

	// searchBodies collects the corpus the site's full-text search scans; it is written below, once the
	// per-note loop has produced each published body.
	searchBodies := make(map[int64]string, len(docs))

	// tasks.json: every published task carrying a date, the read-only half of the live workspace's
	// vault-wide listing. Written even when empty, so the client's fetch is never a 404.
	datedTasks := []jsonTaskRow{}
	for _, d := range listed {
		if d.tasks == nil {
			continue
		}
		for _, t := range d.tasks.Items {
			if t.Scheduled == "" && t.Due == "" {
				continue
			}
			datedTasks = append(datedTasks, jsonTaskRow{NoteID: slugOf(&d), FileKind: kindOf(d), Title: d.title, Task: t})
		}
	}
	if err := bundle.writeJSON("tasks.json", map[string]any{"tasks": datedTasks}); err != nil {
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
		d, ok := byID[id]
		if !ok {
			return "", false
		}
		return slugOf(&d), true
	}
	for _, e := range edges {
		src, ok := byID[e.src]
		if !ok {
			continue
		}
		linkers[e.dst] = append(linkers[e.dst], src)
	}
	// Hierarchy from each doc's up-targets: parentOf follows a doc's first in-set up-target
	// (single-path trail, like the live server's Trail), and childrenOf collects the reverse for
	// the children list.
	parentOf := map[int64]int64{}
	childrenOf := map[int64][]doc{}
	childSeen := map[edge]bool{}
	for _, d := range docs {
		for _, pid := range d.up {
			t, ok := byID[pid]
			if !ok || t.id == d.id {
				continue
			}
			if _, have := parentOf[d.id]; !have {
				parentOf[d.id] = t.id
			}
			if e := (edge{src: d.id, dst: t.id}); !childSeen[e] {
				childSeen[e] = true
				childrenOf[t.id] = append(childrenOf[t.id], d)
			}
		}
	}
	// trailOf walks parentOf to the root, cycle-safe, returning ancestors root first.
	trailOf := func(id int64) []jsonRef {
		trail := []jsonRef{}
		seen := map[int64]bool{id: true}
		for cur := id; ; {
			p, ok := parentOf[cur]
			if !ok || seen[p] {
				return trail
			}
			seen[p] = true
			trail = append([]jsonRef{refOf(byID[p])}, trail...)
			cur = p
		}
	}
	// hierarchy.json: the published "up" forest, prebuilt here so the rail's hierarchy menu never walks
	// the tree in the browser. Like search.json it is fetched when that menu is first opened rather
	// than at first paint, so a reader who never opens it never downloads it. Notes the hierarchy does
	// not place — no parent, no children — are simply absent. Every level is by title, not the shared
	// recency order the rest of the bundle uses: see store.Hierarchy for why a tree holds still.
	var hierarchyNode func(d doc) jsonHierarchyNode
	hierarchyNode = func(d doc) jsonHierarchyNode {
		kids := append([]doc(nil), childrenOf[d.id]...)
		byTitle(kids)
		node := jsonHierarchyNode{jsonRef: refOf(d)}
		for _, kid := range kids {
			node.Children = append(node.Children, hierarchyNode(kid))
		}
		return node
	}
	// A doc in a cycle has a parent and so is never a root: its branch drops out of the forest, which
	// is what keeps the walk above finite.
	hRoots := []doc{}
	for _, d := range docs {
		if _, hasParent := parentOf[d.id]; !hasParent && len(childrenOf[d.id]) > 0 {
			hRoots = append(hRoots, d)
		}
	}
	byTitle(hRoots)
	forest := make([]jsonHierarchyNode, 0, len(hRoots))
	for _, d := range hRoots {
		forest = append(forest, hierarchyNode(d))
	}
	if err := bundle.writeJSON("hierarchy.json", map[string]any{"hierarchy": forest}); err != nil {
		return Result{}, err
	}

	// The query domain for embedded ```track-query fences is the published set (in the shared
	// recently-updated-first order), so a published table never links to — or leaks — an unpublished
	// note; its [[Title]] cells resolve through resolve.json like any other wiki link.
	queryRows := make([]query.NoteRow, 0, len(listed))
	// Gallery covers publish under their opaque asset names, matching the copied files (covers are
	// always copied, referenced or not — see the asset loop below). Icons stand in on cards without
	// a cover; docs carry them already resolved.
	queryCovers := map[int64]string{}
	queryIcons := map[int64]string{}
	for _, d := range listed {
		queryRows = append(queryRows, query.NoteRow{ID: d.id, Title: d.title, Tags: d.tags, Props: d.props, Mtime: d.mtime})
		if d.image != "" {
			queryCovers[d.id] = "assets/" + names.name(d.assetSrc, d.image)
		}
		queryIcons[d.id] = d.icon
	}
	for _, d := range docs {
		srcs := linkers[d.id]
		byRecency(srcs)
		bl := make([]jsonRef, 0, len(srcs))
		for _, src := range srcs {
			bl = append(bl, refOf(src))
		}
		kids := childrenOf[d.id]
		byRecency(kids)
		children := make([]jsonRef, 0, len(kids))
		for _, kid := range kids {
			children = append(children, refOf(kid))
		}
		// Rewrite asset references to their published (slugged) names, matching the copied files.
		body := rewriteAssetRefs(d.body, d.assetSrc, names)
		// Resolve ```dashboard widget blocks to Markdown (recent/journal/pinned lists) at build time, so
		// a published home note shows the same landing view the live workspace does.
		body = dashboard.Resolve(body, dashData)
		// Then resolve ```viewspec fences to ready-to-draw ```echarts option blocks, and
		// ```track-query fences to their Markdown result tables, at build time.
		body = resolveViewSpecBlocks(body, d.dataDir, noteSlug)
		body = query.ExpandBlocks(body, saved, queryRows, func(id int64) (string, string) { return queryCovers[id], queryIcons[id] })
		resp := jsonNoteResponse{
			Note: jsonNoteDetail{
				// Includes resolve against the published body so their line numbers match what the
				// frontend renders. Target ids stay unpublished (0): the embed header navigates by
				// key through resolve.json, like every other link on the static site.
				// ponytail: a viewspec or track-query fence inside an embedded region shows as
				// source in static mode (targets skip the fence resolvers); resolve per-target if
				// that ever matters.
				Includes: link.ResolveIncludes(body, func(key string) (int64, string, string, string, bool) {
					t, ok := keyDocs[key]
					if !ok {
						return 0, "", "", "", false
					}
					return 0, kindOf(t), rewriteAssetRefs(t.body, t.assetSrc, names), etag(t.body), true
				}),
				jsonSearchResult: searchResultOf(d, names),
				CopyPath:         "", // see searchResultOf: the source path is intentionally not published.
				Created:          d.created,
				Updated:          d.mtime,
				Props:            d.props,
				Body:             body,
				ETag:             etag(body),
				Tasks:            d.tasks,
			},
			Backlinks: bl,
			Trail:     trailOf(d.id),
			Children:  children,
		}
		searchBodies[d.id] = body
		if err := bundle.writeJSON(fmt.Sprintf("note/%s.json", slugOf(&d)), resp); err != nil {
			return Result{}, err
		}
	}

	// search.json: the corpus the site's full-text search scans (see web/src/staticSearch.ts). It is a
	// separate file rather than a field on notes.json because notes.json is fetched at first paint and
	// this is only wanted once someone types; the client fetches it on the first search, so a reader
	// who never searches never downloads it.
	//
	// The body is the *published* one, byte for byte what note/<slug>.json carries — not the source.
	// That is what makes this file exposure-free: every published surface strips things the vault
	// should not hand out (asset file names and internal note ids become opaque slugs), and a corpus
	// built from the source body would be the one place in the built site that put them back.
	//
	// Ordered the way the engine's body scan returns hits (searchOrder), so the client can keep the
	// file's order and land on the engine's ordering without shipping mtimes to sort by.
	ordered := append([]doc(nil), docs...)
	searchOrder(ordered)
	searchDocs := make([]jsonSearchDoc, 0, len(ordered))
	for _, d := range ordered {
		searchDocs = append(searchDocs, jsonSearchDoc{NoteID: slugOf(&d), Body: searchBodies[d.id]})
	}
	if err := bundle.writeJSON("search.json", map[string]any{"docs": searchDocs}); err != nil {
		return Result{}, err
	}

	// graph.json (whole published set).
	nodes := make([]jsonGraphNode, 0, len(docs))
	for _, d := range docs {
		nodes = append(nodes, jsonGraphNode{NoteID: slugOf(&d), FileKind: kindOf(d), Title: d.title})
	}
	gEdges := make([]jsonGraphEdge, 0, len(edges))
	for _, e := range edges {
		gEdges = append(gEdges, jsonGraphEdge{SourceID: slugOf(docPtr(byID, e.src)), TargetID: slugOf(docPtr(byID, e.dst))})
	}
	// CenterID is empty for the whole-set graph: there is no centered node, and no slug ever equals "".
	if err := bundle.writeJSON("graph.json",
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
	if err := bundle.writeJSON("resolve.json", resolve); err != nil {
		return Result{}, err
	}

	// site.json: the entry note and site-level toggles.
	siteMeta := jsonSite{
		Root:     slugOf(docPtr(byID, root)),
		Title:    rootTitle,
		Calendar: calendar,
		Share:    share,
		BaseURL:  strings.TrimRight(baseURL, "/"),
	}
	if iconSrc != "" {
		// Published under a fixed name so the source file name never leaks; the extension carries the
		// format, so the favicon works without a type attribute.
		siteMeta.Icon = "icon" + strings.ToLower(filepath.Ext(iconSrc))
	}
	if err := bundle.writeJSON("site.json", siteMeta); err != nil {
		return Result{}, err
	}

	// The bundle is complete: publish it under its fingerprint, so a page always fetches the data of its
	// own deploy and never a cached mix of two (ADR 0070).
	generation, err := bundle.publish()
	if err != nil {
		return Result{}, err
	}

	// Copy the static frontend over the output (index.html + assets/...).
	if err := copyTree(frontendDir, outDir); err != nil {
		return Result{}, fmt.Errorf("copy frontend: %w", err)
	}
	// The site icon lands after copyTree so a stray icon.* in the frontend build can never clobber it.
	if iconSrc != "" {
		if err := copyFile(iconSrc, filepath.Join(outDir, siteMeta.Icon)); err != nil {
			return Result{}, fmt.Errorf("copy site icon: %w", err)
		}
	}
	// Emit a real HTML file per route (start page, per note, and the site-level pages) with that page's
	// OGP meta injected into the copied shell, so crawlers/social shares see per-note metadata and deep
	// links resolve without a host fallback.
	if err := writePages(outDir, slugOf(docPtr(byID, root)), root, docs, listed, siteMeta, bundle.key, generation, names); err != nil {
		return Result{}, fmt.Errorf("write pages: %w", err)
	}
	// Use the same in-memory route inventory as the HTML writer; walking outDir would mix assets and
	// encrypted data files into the sitemap and could drift from the pages this export actually emits.
	if err := writeSitemap(outDir, baseURL, publishedPageRoutes(docs, listed, root, calendar)); err != nil {
		return Result{}, fmt.Errorf("write sitemap: %w", err)
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
		copied, missing, err := copyAssets(src, outDir, rels, noteSlug, bundle.key, names)
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
func searchResultOf(d doc, names *assetNamer) jsonSearchResult {
	out := jsonSearchResult{NoteID: slugOf(&d), FileKind: kindOf(d), Path: "", Title: d.title, Tags: d.tags, Days: d.days, Icon: d.icon, Description: d.desc}
	if d.image != "" {
		out.Image = "assets/" + names.name(d.assetSrc, d.image)
	}
	return out
}

func refOf(d doc) jsonRef {
	return jsonRef{NoteID: slugOf(&d), FileKind: kindOf(d), Title: d.title}
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

// byTitle sorts docs the way the hierarchy is drawn: by title, case-insensitively (the comparison
// `SORT title` uses), with the id breaking ties. It mirrors store.compareHierarchyNodes so a
// published tree reads in the same order as the live one.
func byTitle(ds []doc) {
	sort.Slice(ds, func(i, j int) bool {
		a, b := strings.ToLower(ds[i].title), strings.ToLower(ds[j].title)
		if a != b {
			return a < b
		}
		return ds[i].id < ds[j].id
	})
}

// searchOrder sorts docs the way a body search returns hits (search.Sort): most recently updated
// first, highest id on a tie. It is deliberately not byRecency — the note *listing* breaks the same
// tie the other way, and so does the live server's listing (webui sortRefs), so the two orders are
// genuinely different and the corpus follows the search one.
func searchOrder(ds []doc) {
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].mtime != ds[j].mtime {
			return ds[i].mtime > ds[j].mtime
		}
		return ds[i].id > ds[j].id
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

// writeJSONFile writes one data file locked (see lock.go): the caller names it "<name>.json" because
// that is what it holds, but the published file is "<name>.bin" — the bytes on disk are not JSON, and a
// name promising JSON would be a lie to every host and reader. The frontend swaps the extension the same
// way (web/src/api.ts staticData).
// bundleWriter writes the data files and publishes them under a fingerprint of what they hold.
//
// The fingerprint is over the *plaintext* the files carry, not the bytes on disk: the lock uses a fresh
// nonce per file, so identical data encrypts differently every build, and a path that changed on every
// build would throw away a reader's cache for no reason. Hashing the data means the path changes when —
// and only when — the data does.
type bundleWriter struct {
	stage string
	key   []byte
	sum   hash.Hash
}

func newBundleWriter(outDir string, key []byte) (*bundleWriter, error) {
	// Staged one level down: the finished directory is renamed into place under its fingerprint, which is
	// only known once every file is written.
	stage := filepath.Join(outDir, "data", "staging")
	if err := os.MkdirAll(filepath.Join(stage, "note"), 0o755); err != nil {
		return nil, fmt.Errorf("create out dir: %w", err)
	}
	return &bundleWriter{stage: stage, key: key, sum: sha256.New()}, nil
}

// writeJSON writes one data file. name is the slash path of what it holds ("notes.json",
// "note/<slug>.json"); the published file is "<name>.bin", because its bytes are not JSON (ADR 0069).
func (w *bundleWriter) writeJSON(name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, _ = w.sum.Write([]byte(name))
	_, _ = w.sum.Write(data)
	locked, err := lock(w.key, data)
	if err != nil {
		return err
	}
	path := filepath.Join(w.stage, filepath.FromSlash(strings.TrimSuffix(name, ".json")+".bin"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, locked, 0o644)
}

// publish moves the staged bundle to "data/<generation>" and returns that generation.
func (w *bundleWriter) publish() (string, error) {
	generation := hex.EncodeToString(w.sum.Sum(nil)[:8])
	dst := filepath.Join(filepath.Dir(w.stage), generation)
	// A rebuild into the same output directory finds its own previous generation there; replace it.
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.Rename(w.stage, dst); err != nil {
		return "", fmt.Errorf("publish data bundle: %w", err)
	}
	return generation, nil
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
