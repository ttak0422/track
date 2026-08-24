package store

import (
	"math"
	"sort"
	"strings"
)

// titleSep separates hierarchy levels in a note title, Confluence-style: "foo / bar" is a child of "foo".
const titleSep = " / "

// DanglingPrefix is a note whose title names a parent scope that no note owns, e.g. "foo / bar" while
// no note is titled "foo" — the hierarchy equivalent of an orphan.
type DanglingPrefix struct {
	NoteID        int64  `json:"note_id"`
	Title         string `json:"title"`
	MissingParent string `json:"missing_parent"`
}

// OrphanReport lists notes unreachable by the link graph or the title hierarchy: Orphans have no
// inbound links (nothing discovers them via [[wikilinks]]); DanglingPrefixes name a missing parent scope.
type OrphanReport struct {
	Orphans          []GraphNode      `json:"orphans"`
	DanglingPrefixes []DanglingPrefix `json:"dangling_prefixes"`
}

// Orphans reports notes (kind 'note', excluding journals which are dated and legitimately unlinked)
// that have no inbound link, plus notes whose title prefix names a parent scope no note owns. It
// replaces dream's per-note O(N) backlink probing with one query, and folds the title-hierarchy
// integrity check into the same pass.
func (s *Store) Orphans() (OrphanReport, error) {
	report := OrphanReport{Orphans: []GraphNode{}, DanglingPrefixes: []DanglingPrefix{}}
	notes, err := s.SearchRefs()
	if err != nil {
		return report, err
	}

	inbound := map[int64]bool{}
	rows, err := s.db.Query(`SELECT src_id, dst_id FROM links WHERE src_id != dst_id`)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var src, dst int64
		if err := rows.Scan(&src, &dst); err != nil {
			rows.Close()
			return report, err
		}
		inbound[dst] = true
	}
	if err := rows.Close(); err != nil {
		return report, err
	}

	titles := make(map[string]bool, len(notes))
	for _, n := range notes {
		if n.FileKind == "note" {
			titles[n.Title] = true
		}
	}

	for _, n := range notes {
		if n.FileKind != "note" {
			continue
		}
		if !inbound[n.NoteID] {
			report.Orphans = append(report.Orphans, GraphNode{
				NoteID:   n.NoteID,
				FileKind: n.FileKind,
				Title:    n.Title,
			})
		}
		if i := strings.LastIndex(n.Title, titleSep); i >= 0 {
			parent := n.Title[:i]
			if !titles[parent] {
				report.DanglingPrefixes = append(report.DanglingPrefixes, DanglingPrefix{
					NoteID:        n.NoteID,
					Title:         n.Title,
					MissingParent: parent,
				})
			}
		}
	}
	return report, nil
}

// GraphNode is one note shown in a local graph.
type GraphNode struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	// Vault is the registry name of the vault the node lives in, filled by the serving layer when it
	// addresses more than one vault. It is what lets a rendered graph tell two same-numbered notes
	// apart and colour them by vault.
	Vault  string `json:"vault,omitempty"`
	Path   string `json:"path,omitempty"`
	Title  string `json:"title"`
	Center bool   `json:"center,omitempty"`
	// Size is the note's precomputed five-level grade (1–5) from its outgoing-link count — how much
	// this note reaches out to other notes. The grade is absolute: computed from the whole vault's
	// links with fixed thresholds, never from the slice a view shows, so a node keeps the same size
	// in every graph and the client draws it without computing anything.
	Size int `json:"size,omitempty"`
	// X and Y are the node's position in the whole-graph overview layout, filled by FullGraph only —
	// a local graph has no layout of its own and leaves them unset. The coordinates are deterministic
	// (same index, same picture) and always positive, so the omitempty never drops one.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

// GraphEdge is one directed link between graph nodes.
type GraphEdge struct {
	SourceID int64 `json:"source_id"`
	TargetID int64 `json:"target_id"`
}

// Graph is the local link graph around one note.
type Graph struct {
	CenterID int64       `json:"center_id"`
	Nodes    []GraphNode `json:"nodes"`
	Edges    []GraphEdge `json:"edges"`
}

// sizeLevel grades an outgoing-link count into one of five absolute levels: 0, 1, 2–3, 4–7, 8+.
// Log-scaled, so a hub separates from an ordinary note without a few links blowing the top of the
// scale. "Absolute" means the thresholds never depend on the vault's best or worst note.
func sizeLevel(count int) int {
	switch {
	case count <= 0:
		return 1
	case count == 1:
		return 2
	case count <= 3:
		return 3
	case count <= 7:
		return 4
	default:
		return 5
	}
}

// outgoingCounts returns each note's count of links to other notes (self-links excluded) — the
// precomputed measure the graph sizes its nodes by. One query for any graph view, so local and full
// graphs agree on a node's grade.
func (s *Store) outgoingCounts() (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT src_id, COUNT(*) FROM links WHERE src_id != dst_id GROUP BY src_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// FullGraph returns the entire link graph: every indexed note as a node and every link between two
// known notes as an edge. Unlike LocalGraph there is no center, and each node carries its overview
// layout position (see layoutOverview) so the client draws the whole vault without running a
// simulation of its own.
func (s *Store) FullGraph() (Graph, error) {
	notes, err := s.SearchRefs()
	if err != nil {
		return Graph{}, err
	}
	known := make(map[int64]bool, len(notes))
	nodes := make([]GraphNode, 0, len(notes))
	for _, n := range notes {
		known[n.NoteID] = true
		nodes = append(nodes, GraphNode{
			NoteID:   n.NoteID,
			FileKind: n.FileKind,
			Title:    n.Title,
		})
	}
	// The size grade is the same for every view (absolute grading over the vault's own links), so it
	// is precomputed once per graph build rather than derived per node on the client.
	counts, err := s.outgoingCounts()
	if err != nil {
		return Graph{}, err
	}
	for i := range nodes {
		nodes[i].Size = sizeLevel(counts[nodes[i].NoteID])
	}

	rows, err := s.db.Query(`SELECT src_id, dst_id FROM links ORDER BY src_id, dst_id`)
	if err != nil {
		return Graph{}, err
	}
	var edges []GraphEdge
	for rows.Next() {
		var edge GraphEdge
		if err := rows.Scan(&edge.SourceID, &edge.TargetID); err != nil {
			rows.Close()
			return Graph{}, err
		}
		if known[edge.SourceID] && known[edge.TargetID] {
			edges = append(edges, edge)
		}
	}
	if err := rows.Close(); err != nil {
		return Graph{}, err
	}

	if nodes == nil {
		nodes = []GraphNode{}
	}
	if edges == nil {
		edges = []GraphEdge{}
	}
	layoutOverview(nodes, edges)
	return Graph{CenterID: 0, Nodes: nodes, Edges: edges}, nil
}

// Overview layout constants, in layout world units (the client fits its viewBox to whatever comes
// out, so only relative distances matter).
const (
	// graphRingGap is the base radial distance between BFS depth rings.
	graphRingGap = 90.0
	// graphMinArc is the minimum arc length between neighbours on one ring; a crowded ring grows
	// its radius until its circumference fits its nodes.
	graphMinArc = 14.0
	// graphCompPad is the gap between packed connected components.
	graphCompPad = 140.0
	// graphOriginPad keeps every coordinate strictly positive: GraphNode's x/y are omitempty, so a
	// node at exactly 0 would lose a coordinate in JSON.
	graphOriginPad = 60.0
)

// layoutOverview fills each node's X/Y with a deterministic whole-graph overview position, so the
// client can draw the vault's connection structure as a static picture instead of running a force
// simulation over thousands of nodes.
//
// The shape: links split into connected components; each component is laid out as a radial BFS tree
// around its hub — the highest-degree note at the center, one ring per BFS depth, and each branch's
// angular share proportional to its leaf count so dense branches spread wider than chains; the
// components are then shelf-packed tallest-first into a roughly square field. Everything is ordered
// by node id or fixed rules — no randomness — so the same index always yields the same picture, and
// the whole pass is O((N+E) log E).
func layoutOverview(nodes []GraphNode, edges []GraphEdge) {
	index := make(map[int64]int, len(nodes))
	for i := range nodes {
		index[nodes[i].NoteID] = i
	}
	// Undirected adjacency over known nodes, sorted so traversal order depends on the link set
	// alone, never on the order rows came back in.
	adj := make(map[int64][]int64, len(nodes))
	for _, e := range edges {
		if e.SourceID == e.TargetID {
			continue
		}
		if _, ok := index[e.SourceID]; !ok {
			continue
		}
		if _, ok := index[e.TargetID]; !ok {
			continue
		}
		adj[e.SourceID] = append(adj[e.SourceID], e.TargetID)
		adj[e.TargetID] = append(adj[e.TargetID], e.SourceID)
	}
	for id := range adj {
		ids := adj[id]
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	}

	var comps [][]int64
	seen := make(map[int64]bool, len(nodes))
	for i := range nodes {
		start := nodes[i].NoteID
		if seen[start] {
			continue
		}
		seen[start] = true
		comp := []int64{start}
		for head := 0; head < len(comp); head++ {
			for _, nb := range adj[comp[head]] {
				if !seen[nb] {
					seen[nb] = true
					comp = append(comp, nb)
				}
			}
		}
		comps = append(comps, comp)
	}

	type box struct {
		minX, minY, maxX, maxY float64
		minID                  int64 // tie-breaker for packing order
	}
	pos := make(map[int64][2]float64, len(nodes))
	boxes := make([]box, len(comps))
	for ci, comp := range comps {
		boxes[ci].minX, boxes[ci].minY, boxes[ci].maxX, boxes[ci].maxY, boxes[ci].minID =
			layoutComponent(comp, adj, pos)
	}

	// Shelf-pack: tallest component first, rows wrapping at a square-ish total width so the field
	// stays compact no matter how many singleton components trail behind the big ones.
	order := make([]int, len(comps))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		ba, bb := boxes[order[a]], boxes[order[b]]
		if ha, hb := ba.maxY-ba.minY, bb.maxY-bb.minY; ha != hb {
			return ha > hb
		}
		return ba.minID < bb.minID
	})
	area := 0.0
	for _, b := range boxes {
		area += (b.maxX - b.minX + graphCompPad) * (b.maxY - b.minY + graphCompPad)
	}
	targetW := math.Sqrt(area)
	cursorX, cursorY, rowH := graphOriginPad, graphOriginPad, 0.0
	for _, ci := range order {
		b := boxes[ci]
		w := b.maxX - b.minX
		h := b.maxY - b.minY
		if cursorX > graphOriginPad && cursorX-graphOriginPad+w > targetW {
			cursorX = graphOriginPad
			cursorY += rowH + graphCompPad
			rowH = 0
		}
		dx := cursorX - b.minX
		dy := cursorY - b.minY
		for _, id := range comps[ci] {
			p := pos[id]
			node := &nodes[index[id]]
			node.X = p[0] + dx
			node.Y = p[1] + dy
		}
		cursorX += w + graphCompPad
		if h > rowH {
			rowH = h
		}
	}
}

// layoutComponent lays one connected component out as a radial BFS tree around its hub and records
// each member's position in pos (the hub sits at the origin). It returns the component's bounding
// box and smallest node id.
func layoutComponent(comp []int64, adj map[int64][]int64, pos map[int64][2]float64) (minX, minY, maxX, maxY float64, minID int64) {
	// The hub anchors the layout: highest degree wins, ties go to the smaller note id.
	root, bestDeg := int64(-1), -1
	for _, id := range comp {
		if d := len(adj[id]); d > bestDeg || (d == bestDeg && (root == -1 || id < root)) {
			root, bestDeg = id, d
		}
	}

	parent := make(map[int64]int64, len(comp))
	depth := make(map[int64]int, len(comp))
	order := []int64{root}
	depth[root] = 0
	maxDepth := 0
	for head := 0; head < len(order); head++ {
		cur := order[head]
		for _, nb := range adj[cur] {
			if _, done := depth[nb]; done {
				continue
			}
			depth[nb] = depth[cur] + 1
			parent[nb] = cur
			order = append(order, nb)
			if depth[nb] > maxDepth {
				maxDepth = depth[nb]
			}
		}
	}

	// Ring radii: base spacing per depth, widened where a ring's node count needs more circumference,
	// and monotonically non-decreasing so rings never fold back inward.
	countAtDepth := make([]int, maxDepth+1)
	for _, id := range order {
		countAtDepth[depth[id]]++
	}
	radius := make([]float64, maxDepth+1)
	prev := 0.0
	for d := 1; d <= maxDepth; d++ {
		r := math.Max(graphRingGap*float64(d), float64(countAtDepth[d])*graphMinArc/(2*math.Pi))
		if r < prev {
			r = prev
		}
		radius[d] = r
		prev = r
	}

	// Subtree leaf counts decide angular shares: children lists follow BFS order, which follows the
	// sorted adjacency, so angles are fully determined by the link set.
	children := make(map[int64][]int64, len(comp))
	weight := make(map[int64]float64, len(comp))
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		if kids := children[id]; len(kids) == 0 {
			weight[id] = 1
		} else {
			sum := 0.0
			for _, c := range kids {
				sum += weight[c]
			}
			weight[id] = sum
		}
		if id != root {
			children[parent[id]] = append(children[parent[id]], id)
		}
	}

	spanStart := make(map[int64]float64, len(comp))
	spanSize := map[int64]float64{root: 2 * math.Pi}
	pos[root] = [2]float64{0, 0}
	minX, minY, maxX, maxY = 0, 0, 0, 0
	minID = root
	for _, id := range order {
		if id < minID {
			minID = id
		}
		kids := children[id]
		if len(kids) == 0 {
			continue
		}
		start, total := spanStart[id], spanSize[id]
		cum := 0.0
		for _, c := range kids {
			mid := start + total*(cum+weight[c]/2)/weight[id]
			spanStart[c] = start + total*cum/weight[id]
			spanSize[c] = total * weight[c] / weight[id]
			r := radius[depth[c]]
			x, y := r*math.Cos(mid), r*math.Sin(mid)
			pos[c] = [2]float64{x, y}
			minX, minY = math.Min(minX, x), math.Min(minY, y)
			maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
			cum += weight[c]
		}
	}
	return minX, minY, maxX, maxY, minID
}

// LocalGraph returns the one-hop graph around centerID: notes linking to the center,
// notes the center links to, and edges among those visible nodes.
func (s *Store) LocalGraph(centerID int64) (Graph, error) {
	nodeIDs := map[int64]bool{centerID: true}
	rows, err := s.db.Query(`SELECT src_id, dst_id FROM links WHERE src_id = ? OR dst_id = ?`, centerID, centerID)
	if err != nil {
		return Graph{}, err
	}
	for rows.Next() {
		var edge GraphEdge
		if err := rows.Scan(&edge.SourceID, &edge.TargetID); err != nil {
			rows.Close()
			return Graph{}, err
		}
		nodeIDs[edge.SourceID] = true
		nodeIDs[edge.TargetID] = true
	}
	if err := rows.Close(); err != nil {
		return Graph{}, err
	}

	notes, err := s.SearchRefs()
	if err != nil {
		return Graph{}, err
	}
	known := make(map[int64]SearchResult, len(notes))
	for _, n := range notes {
		known[n.NoteID] = n
	}
	// Absolute grading over the vault's own links, so the local graph's sizes match the full
	// graph's — a node is not bigger or smaller depending on the view that shows it.
	counts, err := s.outgoingCounts()
	if err != nil {
		return Graph{}, err
	}
	var nodes []GraphNode
	for _, n := range notes {
		if !nodeIDs[n.NoteID] {
			continue
		}
		nodes = append(nodes, GraphNode{
			NoteID:   n.NoteID,
			FileKind: n.FileKind,
			Title:    n.Title,
			Center:   n.NoteID == centerID,
			Size:     sizeLevel(counts[n.NoteID]),
		})
	}

	rows, err = s.db.Query(`SELECT src_id, dst_id FROM links ORDER BY src_id, dst_id`)
	if err != nil {
		return Graph{}, err
	}
	var edges []GraphEdge
	for rows.Next() {
		var edge GraphEdge
		if err := rows.Scan(&edge.SourceID, &edge.TargetID); err != nil {
			rows.Close()
			return Graph{}, err
		}
		if nodeIDs[edge.SourceID] && nodeIDs[edge.TargetID] && known[edge.SourceID].NoteID != 0 && known[edge.TargetID].NoteID != 0 {
			edges = append(edges, edge)
		}
	}
	if err := rows.Close(); err != nil {
		return Graph{}, err
	}
	if nodes == nil {
		nodes = []GraphNode{}
	}
	if edges == nil {
		edges = []GraphEdge{}
	}
	return Graph{CenterID: centerID, Nodes: nodes, Edges: edges}, nil
}
