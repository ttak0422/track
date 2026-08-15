package store

import "strings"

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
