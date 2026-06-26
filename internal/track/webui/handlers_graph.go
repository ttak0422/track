package webui

import (
	"net/http"
)

func (s *Server) handleLocalGraph(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	id, err := parseID(r)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	graph, err := s.store.LocalGraph(id)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Path = s.cfg.PathForKind(graph.Nodes[i].FileKind, graph.Nodes[i].NoteID)
	}
	writeJSON(w, map[string]any{"graph": graph})
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	s.refreshIfStale()
	graph, err := s.store.FullGraph()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range graph.Nodes {
		graph.Nodes[i].Path = s.cfg.PathForKind(graph.Nodes[i].FileKind, graph.Nodes[i].NoteID)
	}
	writeJSON(w, map[string]any{"graph": graph})
}
