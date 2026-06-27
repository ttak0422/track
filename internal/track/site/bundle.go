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
	path     string   // source/display path (informational in the static site)
	body     string   // web-sanitized Markdown the frontend renders
	keys     []string // resolution keys ([[key]]) that point at this doc (title, file name, …)
	assets   []string // "assets/<rel>" references in the body
	assetSrc string   // directory those assets are copied from
}

// edge is a directed [[link]] between two in-set docs.
type edge struct{ src, dst int64 }

// JSON shapes matching web/src/types.ts. Kept local so the bundle is self-describing and decoupled from
// the store/webui structs.

type jsonRef struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Path     string `json:"path,omitempty"`
	Title    string `json:"title"`
}

type jsonSearchResult struct {
	NoteID   int64    `json:"note_id"`
	FileKind string   `json:"file_kind"`
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
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
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
}

type jsonGraphEdge struct {
	SourceID int64 `json:"source_id"`
	TargetID int64 `json:"target_id"`
}

type jsonGraph struct {
	CenterID int64           `json:"center_id"`
	Nodes    []jsonGraphNode `json:"nodes"`
	Edges    []jsonGraphEdge `json:"edges"`
}

type jsonSite struct {
	Root  int64  `json:"root"`
	Title string `json:"title"`
}

// writeBundle emits the data bundle, copies the static frontend over it, and copies assets. frontendDir
// is the static-mode Vite build (index.html + assets/...). root is the entry note's id.
func writeBundle(docs []doc, edges []edge, root int64, frontendDir, outDir string) (Result, error) {
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

	// notes.json
	notes := make([]jsonSearchResult, 0, len(docs))
	for _, d := range docs {
		notes = append(notes, searchResultOf(d))
	}
	if err := writeJSONFile(filepath.Join(outDir, "data", "notes.json"), map[string]any{"notes": notes}); err != nil {
		return Result{}, err
	}

	// note/<id>.json with backlinks derived from edges.
	backlinks := map[int64][]jsonRef{}
	byID := map[int64]doc{}
	for _, d := range docs {
		byID[d.id] = d
	}
	for _, e := range edges {
		src, ok := byID[e.src]
		if !ok {
			continue
		}
		backlinks[e.dst] = append(backlinks[e.dst], refOf(src))
	}
	for _, d := range docs {
		bl := backlinks[d.id]
		if bl == nil {
			bl = []jsonRef{}
		}
		resp := jsonNoteResponse{
			Note: jsonNoteDetail{
				jsonSearchResult: searchResultOf(d),
				CopyPath:         d.path,
				Body:             d.body,
				ETag:             etag(d.body),
			},
			Backlinks: bl,
		}
		if err := writeJSONFile(filepath.Join(outDir, "data", "note", fmt.Sprintf("%d.json", d.id)), resp); err != nil {
			return Result{}, err
		}
	}

	// graph.json (whole published set).
	nodes := make([]jsonGraphNode, 0, len(docs))
	for _, d := range docs {
		nodes = append(nodes, jsonGraphNode{NoteID: d.id, FileKind: kindOf(d), Title: d.title})
	}
	gEdges := make([]jsonGraphEdge, 0, len(edges))
	for _, e := range edges {
		gEdges = append(gEdges, jsonGraphEdge{SourceID: e.src, TargetID: e.dst})
	}
	if err := writeJSONFile(filepath.Join(outDir, "data", "graph.json"),
		map[string]any{"graph": jsonGraph{CenterID: 0, Nodes: nodes, Edges: gEdges}}); err != nil {
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

	// site.json: the entry note.
	if err := writeJSONFile(filepath.Join(outDir, "data", "site.json"), jsonSite{Root: root, Title: rootTitle}); err != nil {
		return Result{}, err
	}

	// Copy the static frontend over the output (index.html + assets/...).
	if err := copyTree(frontendDir, outDir); err != nil {
		return Result{}, fmt.Errorf("copy frontend: %w", err)
	}

	// Copy referenced note assets.
	res := Result{OutDir: outDir}
	for _, d := range docs {
		res.Notes = append(res.Notes, d.id)
	}
	bySrc := map[string]map[string]bool{}
	for _, d := range docs {
		for _, rel := range d.assets {
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
		copied, missing, err := copyAssets(src, outDir, rels)
		if err != nil {
			return Result{}, fmt.Errorf("copy assets: %w", err)
		}
		res.Assets = append(res.Assets, copied...)
		res.Missing = append(res.Missing, missing...)
	}
	return res, nil
}

func searchResultOf(d doc) jsonSearchResult {
	return jsonSearchResult{NoteID: d.id, FileKind: kindOf(d), Path: d.path, Title: d.title, Tags: d.tags}
}

func refOf(d doc) jsonRef {
	return jsonRef{NoteID: d.id, FileKind: kindOf(d), Path: d.path, Title: d.title}
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
