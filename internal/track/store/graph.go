package store

// GraphNode is one note shown in a local graph.
type GraphNode struct {
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Path     string `json:"path,omitempty"`
	Title    string `json:"title"`
	Center   bool   `json:"center,omitempty"`
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

	notes, err := s.AllNotes()
	if err != nil {
		return Graph{}, err
	}
	known := make(map[int64]NoteRef, len(notes))
	for _, n := range notes {
		known[n.NoteID] = n
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
