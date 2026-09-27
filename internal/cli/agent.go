package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// defaultWebAddr is the web API address the agent reports to; it matches `track web`'s default bind,
// so an agent running next to a default-started server needs no --addr.
const defaultWebAddr = "127.0.0.1:8765"

// agentHTTPTimeout bounds one report request. A claim/result/fail is a single local POST; a server
// that accepts but never answers should fail the report rather than hang the agent. It is a var so
// tests can shorten the wait.
var agentHTTPTimeout = 30 * time.Second

// cmdAgent routes the agent report subcommands: claim, result, and fail. These are the agent-facing
// half of the live request gateway (docs/spec/live-agent-requests.md): they authenticate with the
// machine config's token for the request's agent and POST the report to the local web API.
func cmdAgent(args []string) int {
	if len(args) == 0 {
		return fail("agent: want claim, result, or fail")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "claim":
		return cmdAgentClaim(rest)
	case "result":
		return cmdAgentResult(rest)
	case "fail":
		return cmdAgentFail(rest)
	default:
		return fail("agent: unknown subcommand %q (want claim, result, or fail)", sub)
	}
}

// agentReportOpts is the parsed input shared by every report subcommand: the web API address and the
// request+dispatch dual key the report is keyed by. The addressed vault comes from the global
// --vault flag / TRACK_VAULT, resolved by config.Load like every other command.
type agentReportOpts struct {
	addr       string
	requestID  string
	dispatchID string
}

func cmdAgentClaim(args []string) int {
	fs := flag.NewFlagSet("agent claim", flag.ContinueOnError)
	o := agentReportOpts{}
	fs.StringVar(&o.addr, "addr", defaultWebAddr, "web API address (matches 'track web --addr')")
	fs.StringVar(&o.requestID, "request", "", "request id")
	fs.StringVar(&o.dispatchID, "dispatch", "", "dispatch id")
	if code, ok := parseArgs(fs, args, `track agent claim [--addr HOST:PORT] [--vault NAME] --request REQ --dispatch DISP

confirm that the agent started executing a request; the stored request is printed as JSON`); !ok {
		return code
	}
	return agentReport("claim", o, nil)
}

func cmdAgentResult(args []string) int {
	fs := flag.NewFlagSet("agent result", flag.ContinueOnError)
	o := agentReportOpts{}
	fs.StringVar(&o.addr, "addr", defaultWebAddr, "web API address (matches 'track web --addr')")
	fs.StringVar(&o.requestID, "request", "", "request id")
	fs.StringVar(&o.dispatchID, "dispatch", "", "dispatch id")
	if code, ok := parseArgs(fs, args, `track agent result [--addr HOST:PORT] [--vault NAME] --request REQ --dispatch DISP

submit the result JSON on stdin ({"answer_markdown":...,"sources":[...]} for explain/research,
{"proposed_body":...} for update); the stored request is printed as JSON`); !ok {
		return code
	}
	payload, code, ok := readResultStdin()
	if !ok {
		return code
	}
	return agentReport("result", o, payload)
}

func cmdAgentFail(args []string) int {
	fs := flag.NewFlagSet("agent fail", flag.ContinueOnError)
	o := agentReportOpts{}
	fs.StringVar(&o.addr, "addr", defaultWebAddr, "web API address (matches 'track web --addr')")
	fs.StringVar(&o.requestID, "request", "", "request id")
	fs.StringVar(&o.dispatchID, "dispatch", "", "dispatch id")
	reason := fs.String("reason", "", "failure reason")
	if code, ok := parseArgs(fs, args, `track agent fail [--addr HOST:PORT] [--vault NAME] --request REQ --dispatch DISP [--reason R]

report a confirmed execution failure; the stored request is printed as JSON`); !ok {
		return code
	}
	return agentReport("fail", o, map[string]any{"reason": *reason})
}

// readResultStdin reads the result payload from piped stdin. An interactive terminal (no pipe) or
// input that is not a JSON object is an error — a result report must always carry a body, so the
// command never guesses or sends an empty result.
func readResultStdin() (map[string]any, int, bool) {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return nil, fail("agent result: pipe the result JSON on stdin"), false
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fail("agent result: read stdin: %v", err), false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fail("agent result: stdin is not a JSON object: %v", err), false
	}
	if len(payload) == 0 {
		return nil, fail("agent result: stdin is empty; send {\"answer_markdown\":...} or {\"proposed_body\":...}"), false
	}
	return payload, 0, true
}

// agentReport performs one authenticated report against the local web API and prints the stored
// request on success. The token is resolved from the machine config — it is never read from a flag,
// so it cannot leak through a process list or a shell history.
func agentReport(action string, o agentReportOpts, payload map[string]any) int {
	if strings.TrimSpace(o.requestID) == "" {
		return fail("agent %s: --request is required", action)
	}
	if strings.TrimSpace(o.dispatchID) == "" {
		return fail("agent %s: --dispatch is required", action)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("agent %s: %v", action, err)
	}
	token, err := agentToken(cfg, o.requestID)
	if err != nil {
		return fail("agent %s: %v", action, err)
	}
	// The dispatch id is the flag's; a body that also carried one is overridden so the flag always
	// names the attempt being reported.
	if payload == nil {
		payload = map[string]any{}
	}
	payload["dispatch_id"] = o.dispatchID
	body, err := json.Marshal(payload)
	if err != nil {
		return fail("agent %s: encode body: %v", action, err)
	}
	req, err := http.NewRequest(http.MethodPost, webAPIURL(o.addr, o.requestID, action, cfg.VaultName), bytes.NewReader(body))
	if err != nil {
		return fail("agent %s: build request: %v", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: agentHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fail("agent %s: %v", action, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail("agent %s: read response: %v", action, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// The server responds with the stored request; print it as the command's one JSON result,
		// byte-identical to what the request gateway recorded.
		if len(bytes.TrimSpace(raw)) == 0 {
			return fail("agent %s: server returned an empty response", action)
		}
		fmt.Println(string(bytes.TrimSpace(raw)))
		return 0
	}
	if msg := errorMessage(raw); msg != "" {
		return fail("agent %s: %s", action, msg)
	}
	return fail("agent %s: server returned HTTP %d", action, resp.StatusCode)
}

// webAPIURL builds the report endpoint for the addressed vault. The vault is addressed by its
// registry name (the same ?vault= the browser API uses); an unregistered active vault is addressed
// without the query parameter, exactly as the server resolves the workspace vault.
func webAPIURL(addr, requestID, action, vaultName string) string {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	addr = strings.TrimSuffix(addr, "/")
	u := addr + "/api/requests/" + url.PathEscape(requestID) + "/" + action
	if vaultName != "" {
		u += "?vault=" + url.QueryEscape(vaultName)
	}
	return u
}

// agentToken resolves the Bearer token for one request report. The request file in the addressed
// vault carries the immutable agent id, and the machine config registers that agent's token; reading
// the file is the connection between the two. The token is machine config only — it never travels on
// the command line and never appears in the request JSON model or the web API.
func agentToken(cfg *config.Config, requestID string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(cfg.RequestsDir(), requestID+".json"))
	if err != nil {
		return "", fmt.Errorf("read request record %s (is the request server running for this vault?): %w", requestID, err)
	}
	var rec struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return "", fmt.Errorf("request record %s is not valid JSON: %w", requestID, err)
	}
	if rec.AgentID == "" {
		return "", fmt.Errorf("request record %s carries no agent id", requestID)
	}
	agent, ok := cfg.Agents[rec.AgentID]
	if !ok || agent.Token == "" {
		return "", fmt.Errorf("agent %q is not registered with a token in the machine config; reports are refused", rec.AgentID)
	}
	return agent.Token, nil
}

// errorMessage extracts the {"error": ...} body the web API writes for every failure.
func errorMessage(raw []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &e); err != nil || e.Error == "" {
		return ""
	}
	return e.Error
}
