package webui

import (
	"net/http"

	"github.com/ttak0422/track/internal/track/store"
)

func (s *Server) handleLocalGraph(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	graph, err := v.store.LocalGraph(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	addGraphPaths(v, graph.Nodes)
	writeJSON(w, map[string]any{"vault": v.label, "graph": graph})
}

func (s *Server) handleGraph(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
	graph, err := v.store.FullGraph()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	addGraphPaths(v, graph.Nodes)
	writeJSON(w, map[string]any{"vault": v.label, "graph": graph})
}

// handleHierarchy serves the whole vault's "up" tree, the rail's hierarchy menu. It is a listing like
// the full graph rather than a per-note lookup, and the client asks for it only when that menu is
// first opened.
func (s *Server) handleHierarchy(v *vaultView, w http.ResponseWriter, r *http.Request) {
	s.refresh(v)
	tree, err := v.store.Hierarchy()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"vault": v.label, "hierarchy": tree})
}

// addGraphPaths labels each node with its path and the vault it lives in, so a graph drawn over
// several vaults can tell two same-numbered notes apart.
func addGraphPaths(v *vaultView, nodes []store.GraphNode) {
	for i := range nodes {
		nodes[i].Vault = v.label
		nodes[i].Path = v.cfg.PathForKind(nodes[i].FileKind, nodes[i].NoteID)
	}
}
