package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

// TestAgentsEndpointProjectsSafely verifies GET /api/agents returns each registered agent as a safe
// projection — id, name, operations, agmsg availability — and never the token or the agmsg
// connection settings (docs/spec/live-agent-requests.md: machine-local only, never in the web API).
func TestAgentsEndpointProjectsSafely(t *testing.T) {
	server, _ := requestServerAgents(t, map[string]config.AgentConfig{
		"research-assistant": {
			Name:       "Research Assistant",
			Operations: []string{"research", "explain"},
			Token:      "token-research-9876",
			Agmsg: &config.AgmsgConfig{
				SendScript: "/opt/agmsg/send.sh",
				Team:       "team-a",
				Sender:     "track-web",
				Recipient:  "ra@agmsg.local",
			},
		},
		"code-reviewer": {
			Name:  "Code Reviewer",
			Token: "token-review-1234",
		},
		"bare": {
			Token: "token-bare-5678",
		},
	})

	code, body := agentsBody(t, server.URL+"/api/agents")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	var out struct {
		Agents []agentInfo `json:"agents"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
	if len(out.Agents) != 3 {
		t.Fatalf("listed %d agents, want 3: %v", len(out.Agents), out.Agents)
	}
	// Deterministic order by id.
	for i, want := range []string{"bare", "code-reviewer", "research-assistant"} {
		if out.Agents[i].ID != want {
			t.Fatalf("agents[%d].id = %q, want %q", i, out.Agents[i].ID, want)
		}
	}

	byID := map[string]agentInfo{}
	for _, a := range out.Agents {
		byID[a.ID] = a
	}
	if got := byID["research-assistant"]; got.Name != "Research Assistant" ||
		len(got.Operations) != 2 || got.Operations[0] != "research" || got.Operations[1] != "explain" ||
		!got.AgmsgAvailable {
		t.Fatalf("research-assistant projection wrong: %+v", got)
	}
	if got := byID["code-reviewer"]; got.Name != "Code Reviewer" || got.AgmsgAvailable {
		t.Fatalf("code-reviewer projection wrong: %+v", got)
	}
	if got := byID["bare"]; got.Name != "" || len(got.Operations) != 0 || got.AgmsgAvailable {
		t.Fatalf("bare projection wrong: %+v", got)
	}

	// Redaction: neither the token, the agmsg connection keys/values, nor the agmsg connection
	// object itself may appear anywhere in the body. The projection struct has no fields for them,
	// so this asserts the wire never regresses (e.g. by marshaling AgentConfig directly).
	forbiddenKeys := []string{`"token"`, `"send_script"`, `"team"`, `"sender"`, `"recipient"`, `"agmsg":`}
	for _, key := range forbiddenKeys {
		if bytes.Contains(body, []byte(key)) {
			t.Fatalf("response leaks the connection key %s: %s", key, body)
		}
	}
	for _, secret := range []string{"token-research-9876", "token-review-1234", "token-bare-5678",
		"/opt/agmsg/send.sh", "team-a", "ra@agmsg.local"} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatalf("response leaks the secret %q: %s", secret, body)
		}
	}
}

// agentsBody issues a GET and returns the raw response body, so the redaction assertions check the
// exact bytes the wire carries rather than a round-tripped map.
func agentsBody(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, buf.Bytes()
}

// TestAgentsEndpointMethodAndEmptyRegistry covers the endpoint's edges: only GET is allowed, and an
// unregistered agent registry lists as an empty array rather than null.
func TestAgentsEndpointMethodAndEmptyRegistry(t *testing.T) {
	server, _ := requestServerAgents(t, map[string]config.AgentConfig{})
	if code, _ := postRequest(t, server.URL+"/api/agents", `{}`); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST should 405, got %d", code)
	}
	code, body := agentsBody(t, server.URL+"/api/agents")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if bytes.Contains(body, []byte(`"agents":null`)) {
		t.Fatalf("empty registry must list as [], got %s", body)
	}
	if !bytes.Contains(body, []byte(`"agents":[]`)) {
		t.Fatalf("empty registry should list an empty array: %s", body)
	}
}
