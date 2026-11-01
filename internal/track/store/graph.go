package store

import (
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
	// Size retains the absolute outgoing-link grade: 0 / 1 / 2–3 / 4–7 / 8+.
	Size int `json:"size,omitempty"`
	// AreaScore is a continuous area weight in [1, 5], relative to this vault's
	// median positive outgoing-link count, computed before authenticated view filtering.
	AreaScore float64 `json:"area_score,omitempty"`
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

// GraphSizeLevel retains the original five-level outgoing-link grade for older clients.
func GraphSizeLevel(count int) int {
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

// GraphAreaScores maps outgoing counts to bounded area weights in the input order.
// Callers must pass only counts from their allowed population; exports use public edges only.
func GraphAreaScores(counts []int) []float64 {
	positive := make([]int, 0, len(counts))
	for _, count := range counts {
		if count > 0 {
			positive = append(positive, count)
		}
	}
	sort.Ints(positive)
	median := 1.0
	if length := len(positive); length > 0 {
		median = (float64(positive[(length-1)/2]) + float64(positive[length/2])) / 2
	}
	scores := make([]float64, len(counts))
	for i, count := range counts {
		// A bounded squared ratio adds contrast without magnifying tiny differences by rank.
		// Twice the median gets half the available area above the minimum dot area.
		ratio := float64(count) / (2 * median)
		squared := ratio * ratio
		scores[i] = 1 + 4*squared/(1+squared)
	}
	return scores
}

func graphSizes(notes []SearchResult, counts map[int64]int) map[int64]float64 {
	known := make([]int, len(notes))
	for i, n := range notes {
		known[i] = counts[n.NoteID]
	}
	scores := GraphAreaScores(known)
	sizes := make(map[int64]float64, len(notes))
	for i, n := range notes {
		sizes[n.NoteID] = scores[i]
	}
	return sizes
}

// outgoingCounts returns each note's count of links to other notes (self-links excluded) — the
// measure the graph sizes its nodes by. One query for any graph view.
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
// known notes as an edge. Unlike LocalGraph there is no center, so the client lays out the whole vault.
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
	// Use the complete vault as the size reference before any view filters the graph.
	counts, err := s.outgoingCounts()
	if err != nil {
		return Graph{}, err
	}
	sizes := graphSizes(notes, counts)
	for i := range nodes {
		nodes[i].Size = GraphSizeLevel(counts[nodes[i].NoteID])
		nodes[i].AreaScore = sizes[nodes[i].NoteID]
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
	return Graph{CenterID: 0, Nodes: nodes, Edges: edges}, nil
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
	// Keep the same reference as the full graph, including notes outside this neighbourhood.
	counts, err := s.outgoingCounts()
	if err != nil {
		return Graph{}, err
	}
	sizes := graphSizes(notes, counts)
	var nodes []GraphNode
	for _, n := range notes {
		if !nodeIDs[n.NoteID] {
			continue
		}
		nodes = append(nodes, GraphNode{
			NoteID:    n.NoteID,
			FileKind:  n.FileKind,
			Title:     n.Title,
			Center:    n.NoteID == centerID,
			Size:      GraphSizeLevel(counts[n.NoteID]),
			AreaScore: sizes[n.NoteID],
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
