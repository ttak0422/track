package webui

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
)

// agentInfo is the safe wire projection of one registered agent for the request panel
// (docs/spec/live-agent-requests.md, stage 3). It carries only what the panel needs to offer an
// agent: the stable id to send as agent_id, the display name, the declared operations, and whether
// a request against it would actually be delivered (agmsg availability). The token and the agmsg
// connection settings are machine-local and never projected — the struct simply has no fields for
// them, so no future marshaling path can leak them.
type agentInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name,omitempty"`
	Operations     []string `json:"operations,omitempty"`
	AgmsgAvailable bool     `json:"agmsg_available"`
}

// handleAgents lists the registered machine-local agents as safe projections. The web API is
// live-only: the route lives on the `track web` server's mux, and the static export (site.Build)
// never consults cfg.Agents, so a published site carries no agent data (see
// site.TestBuildKeepsAgentRegistryOut). The route sits under the same Host/Origin guard as every
// other read API through Handler.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	ids := make([]string, 0, len(s.cfg.Agents))
	for id := range s.cfg.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	agents := make([]agentInfo, 0, len(ids))
	for _, id := range ids {
		a := s.cfg.Agents[id]
		agents = append(agents, agentInfo{
			ID:             id,
			Name:           a.Name,
			Operations:     a.Operations,
			AgmsgAvailable: agentAgmsgAvailable(a),
		})
	}
	writeJSON(w, map[string]any{"agents": agents})
}

// agentAgmsgAvailable reports whether the agent has a working agmsg delivery path: a connection is
// configured and names a send script. An agmsg entry with an empty send_script cannot send — the
// delivery fails at send time (agmsg.Send) — so it is not "available" to the panel. It reveals only
// that a connection exists, never any of its settings.
func agentAgmsgAvailable(a config.AgentConfig) bool {
	return a.Agmsg != nil && strings.TrimSpace(a.Agmsg.SendScript) != ""
}
