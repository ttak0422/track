package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/webui"
	"gopkg.in/yaml.v3"
)

// agentMachineConfig is the shape of the machine config file the CLI under test reads. It mirrors
// config.machineFileConfig for the fields an agent command needs; yaml tags keep it strict-decoding.
type agentMachineConfig struct {
	CacheDir     string                        `yaml:"cache_dir"`
	VaultDir     string                        `yaml:"vault_dir"`
	DefaultVault string                        `yaml:"default_vault"`
	Vaults       map[string]string             `yaml:"vaults"`
	Agents       map[string]config.AgentConfig `yaml:"agents"`
}

// agentEnv wires a temp machine config, a temp vault (or registry), and a live webui server for CLI
// agent tests. The CLI under test reads the machine config file; the server is built from the same
// agent registry, so a request created through the API is addressable by the CLI's reports.
type agentEnv struct {
	server     *httptest.Server
	vault      string // the launch (active) vault directory
	configPath string
}

// newAgentEnv builds a single-vault env: the machine config carries vault_dir plus the agents.
func newAgentEnv(t *testing.T, agents map[string]config.AgentConfig) *agentEnv {
	t.Helper()
	return newAgentEnvRegistry(t, "", nil, agents)
}

// newAgentEnvRegistry builds a registry env: vaults names the registered vaults, defaultVault names
// the one the web server is launched in ("" keeps the single-vault vault_dir form).
func newAgentEnvRegistry(t *testing.T, defaultVault string, vaults map[string]string, agents map[string]config.AgentConfig) *agentEnv {
	t.Helper()
	launch := t.TempDir()
	cfg := &config.Config{
		VaultDir:          launch,
		DBPath:            filepath.Join(t.TempDir(), "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
		VaultName:         defaultVault,
		Vaults:            vaults,
		Agents:            agents,
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	server := httptest.NewServer(webui.New(cfg, s).Handler())
	t.Cleanup(server.Close)

	configPath := filepath.Join(t.TempDir(), "config.yml")
	mc := agentMachineConfig{
		CacheDir:     t.TempDir(),
		DefaultVault: defaultVault,
		Vaults:       vaults,
		Agents:       agents,
	}
	if len(vaults) == 0 {
		// A registry is the vault_dir-refusing form; a single vault is named by vault_dir.
		mc.VaultDir = launch
		mc.DefaultVault = ""
	}
	raw, err := yaml.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal machine config: %v", err)
	}
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write machine config: %v", err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", launch)
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(launch, ".test-cache"))
	return &agentEnv{server: server, vault: launch, configPath: configPath}
}

// rewriteAgentConfig replaces the machine config's agents, so a test can make the CLI resolve a
// different token than the server registered.
func rewriteAgentConfig(t *testing.T, env *agentEnv, agents map[string]config.AgentConfig) {
	t.Helper()
	raw, err := yaml.Marshal(agentMachineConfig{VaultDir: env.vault, Agents: agents})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.configPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// runAgentCLI runs one `track ...` invocation with the given piped stdin ("" points stdin at
// /dev/null, a char device, so commands that require a pipe fail deterministically) and returns the
// decoded stdout JSON and the exit code.
func runAgentCLI(t *testing.T, stdin string, args ...string) (map[string]any, int) {
	t.Helper()
	old := os.Stdin
	if stdin == "" {
		devnull, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer devnull.Close()
		os.Stdin = devnull
	} else {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.WriteString(stdin); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		os.Stdin = r
		defer r.Close()
	}
	defer func() { os.Stdin = old }()
	out, code := capture(t, func() int { return Run(args) })
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q (err %v)", out, err)
	}
	return decoded, code
}

// createRequest creates a request through the web API and returns its request and dispatch ids.
func createRequest(t *testing.T, url, body string) (id, dispatch string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create = %d: %v", resp.StatusCode, decoded)
	}
	r, _ := decoded["request"].(map[string]any)
	id, _ = r["id"].(string)
	attempts, _ := r["attempts"].([]any)
	last, _ := attempts[len(attempts)-1].(map[string]any)
	dispatch, _ = last["id"].(string)
	if id == "" || dispatch == "" {
		t.Fatalf("create response carries no request/dispatch ids: %v", decoded)
	}
	return id, dispatch
}

// requestDetail fetches one request through the web API.
func requestDetail(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("detail %s: %v", url, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail = %d: %v", resp.StatusCode, decoded)
	}
	return decoded
}

// fakeSendScript writes an executable send.sh that appends its argv to logPath (one argument per
// line) and then runs body; the log is the observable record of what the dispatch worker handed it.
func fakeSendScript(t *testing.T, logPath, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "send.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"" + logPath + "\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake send.sh: %v", err)
	}
	return script
}

func sendLineCount(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read send log: %v", err)
	}
	trimmed := strings.TrimRight(string(raw), "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// waitFor polls cond until it holds or the timeout passes; the dispatch worker runs async, so the
// delivery assertions wait on its observable effects rather than sleeping.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

// deliveryStatus reads the current attempt's delivery status from a request detail response.
func deliveryStatus(resp map[string]any) string {
	attempts, _ := resp["attempts"].([]any)
	last, _ := attempts[len(attempts)-1].(map[string]any)
	d, _ := last["delivery"].(string)
	return d
}

// TestAgentClaimResultFail drives the happy path entirely through the CLI: claim turns the request
// running, result on stdin completes it with the answer, and each response is the stored request.
func TestAgentClaimResultFail(t *testing.T) {
	env := newAgentEnv(t, map[string]config.AgentConfig{"a": {Token: "token-a"}})
	id, dispatch := createRequest(t, env.server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)

	decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
	if code != 0 {
		t.Fatalf("claim exit = %d: %v", code, decoded)
	}
	if decoded["status"] != "running" {
		t.Fatalf("claim status = %v, want running", decoded["status"])
	}
	attempts := decoded["attempts"].([]any)
	last := attempts[len(attempts)-1].(map[string]any)
	if last["status"] != "running" {
		t.Fatalf("claimed attempt status = %v, want running", last["status"])
	}

	decoded, code = runAgentCLI(t, `{"answer_markdown":"the answer","sources":["src-a"]}`,
		"agent", "result", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
	if code != 0 {
		t.Fatalf("result exit = %d: %v", code, decoded)
	}
	if decoded["status"] != "completed" {
		t.Fatalf("result status = %v, want completed", decoded["status"])
	}
	res, _ := decoded["result"].(map[string]any)
	if res["answer_markdown"] != "the answer" {
		t.Fatalf("result content wrong: %v", decoded["result"])
	}
	if got := res["fingerprint"]; got == "" || got == nil {
		t.Fatalf("server-computed fingerprint missing from the result: %v", res)
	}

	// A second identical result is an idempotent replay, still exit 0.
	decoded, code = runAgentCLI(t, `{"answer_markdown":"the answer","sources":["src-a"]}`,
		"agent", "result", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
	if code != 0 || decoded["status"] != "completed" {
		t.Fatalf("replayed result = %d %v", code, decoded)
	}
}

func TestAgentFail(t *testing.T) {
	env := newAgentEnv(t, map[string]config.AgentConfig{"a": {Token: "token-a"}})
	id, dispatch := createRequest(t, env.server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)

	decoded, code := runAgentCLI(t, "", "agent", "fail", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch, "--reason", "roster broke")
	if code != 0 {
		t.Fatalf("fail exit = %d: %v", code, decoded)
	}
	if decoded["status"] != "failed" || decoded["error"] != "roster broke" {
		t.Fatalf("fail result wrong: %v", decoded)
	}
	attempts := decoded["attempts"].([]any)
	last := attempts[len(attempts)-1].(map[string]any)
	if last["status"] != "failed" || last["failure_reason"] != "roster broke" {
		t.Fatalf("failed attempt wrong: %v", last)
	}
}

// TestAgentCLIErrors pins the CLI's JSON/exit contract: every failure is {"error":...} with exit 1,
// and the report is refused before any state change when a flag is missing or the token is wrong.
func TestAgentCLIErrors(t *testing.T) {
	env := newAgentEnv(t, map[string]config.AgentConfig{"a": {Token: "token-a"}})
	id, dispatch := createRequest(t, env.server.URL+"/api/requests", `{"intent":"explain","instruction":"x","agent_id":"a"}`)

	t.Run("missing request", func(t *testing.T) {
		decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--dispatch", dispatch)
		if code != 1 {
			t.Fatalf("missing --request must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "--request") {
			t.Fatalf("error should name --request: %v", decoded)
		}
	})
	t.Run("missing dispatch", func(t *testing.T) {
		decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--request", id)
		if code != 1 {
			t.Fatalf("missing --dispatch must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "--dispatch") {
			t.Fatalf("error should name --dispatch: %v", decoded)
		}
	})
	t.Run("unknown request", func(t *testing.T) {
		decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--request", "req-nope", "--dispatch", dispatch)
		if code != 1 {
			t.Fatalf("unknown request must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "request record") {
			t.Fatalf("error should name the request record: %v", decoded)
		}
	})
	t.Run("unknown dispatch", func(t *testing.T) {
		decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--request", id, "--dispatch", "d-nope")
		if code != 1 {
			t.Fatalf("unknown dispatch must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "not an attempt") {
			t.Fatalf("error should name the dispatch: %v", decoded)
		}
	})
	t.Run("wrong token", func(t *testing.T) {
		// The server registered token-a; rewrite the machine config so the CLI resolves a different
		// token, proving the report is authenticated by the machine config's value.
		rewriteAgentConfig(t, env, map[string]config.AgentConfig{"a": {Token: "stale-token"}})
		t.Cleanup(func() { rewriteAgentConfig(t, env, map[string]config.AgentConfig{"a": {Token: "token-a"}}) })
		decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
		if code != 1 {
			t.Fatalf("wrong token must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "authorization") {
			t.Fatalf("error should report the authorization failure: %v", decoded)
		}
	})
	t.Run("result without piped stdin", func(t *testing.T) {
		// stdin points at /dev/null (a char device): the command refuses instead of guessing.
		decoded, code := runAgentCLI(t, "", "agent", "result", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
		if code != 1 {
			t.Fatalf("result without stdin must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "stdin") {
			t.Fatalf("error should ask for stdin: %v", decoded)
		}
	})
	t.Run("result with invalid stdin", func(t *testing.T) {
		decoded, code := runAgentCLI(t, "not json", "agent", "result", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
		if code != 1 {
			t.Fatalf("invalid stdin must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "JSON") {
			t.Fatalf("error should say stdin is not JSON: %v", decoded)
		}
	})
	t.Run("unknown agent subcommand", func(t *testing.T) {
		decoded, code := runAgentCLI(t, "", "agent", "dance")
		if code != 1 {
			t.Fatalf("unknown subcommand must exit 1, got %d", code)
		}
		if msg, _ := decoded["error"].(string); !strings.Contains(msg, "claim, result, or fail") {
			t.Fatalf("error should list the subcommands: %v", decoded)
		}
	})
	t.Run("unreachable addr", func(t *testing.T) {
		old := agentHTTPTimeout
		agentHTTPTimeout = 2 * time.Second
		t.Cleanup(func() { agentHTTPTimeout = old })
		decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", "127.0.0.1:1", "--request", id, "--dispatch", dispatch)
		if code != 1 {
			t.Fatalf("unreachable addr must exit 1, got %d", code)
		}
		if _, ok := decoded["error"]; !ok {
			t.Fatalf("error output missing: %v", decoded)
		}
	})
}

// TestAgentCLIDeliveryLoopAndIdempotentCreate exercises the whole stage-2 loop from the CLI's side:
// a fake send.sh delivers a fresh request, the same create replayed is reused without a second send,
// and the agent claims and completes through the CLI — with the delivery still recorded on the
// attempt.
func TestAgentCLIDeliveryLoopAndIdempotentCreate(t *testing.T) {
	log := filepath.Join(t.TempDir(), "send.log")
	env := newAgentEnv(t, map[string]config.AgentConfig{
		"a": {
			Token: "token-a",
			Agmsg: &config.AgmsgConfig{
				SendScript: fakeSendScript(t, log, "exit 0"),
				Team:       "team-a",
				Sender:     "track-web",
				Recipient:  "agent-a",
			},
		},
	})
	base := `{"client_request_id":"click-1","intent":"explain","instruction":"x","agent_id":"a"}`
	id, dispatch := createRequest(t, env.server.URL+"/api/requests", base)
	// The send worker is async and the test process shares the machine with the rest of the suite;
	// 5s keeps the assertion robust under parallel test load.
	waitFor(t, 5*time.Second, func() bool { return deliveryStatus(requestDetail(t, env.server.URL+"/api/requests/"+id)) == "sent" }, "first delivery did not become sent")
	if n := sendLineCount(t, log); n != 4 {
		t.Fatalf("send.sh received %d arguments, want the 4-arg plain form", n)
	}

	// The same create replayed is reused and must not dispatch a second send. A stray resend would
	// bump the log to 8 lines; poll long enough for one to arrive, failing the instant it does.
	againID, _ := createRequest(t, env.server.URL+"/api/requests", base)
	if againID != id {
		t.Fatalf("idempotent create made a new request %s != %s", againID, id)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n := sendLineCount(t, log); n != 4 {
			t.Fatalf("idempotent create resent the delivery: send.log has %d lines, want 4", n)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The agent claims and completes through the CLI; the recorded delivery survives the lifecycle.
	decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
	if code != 0 || decoded["status"] != "running" {
		t.Fatalf("claim = %d %v", code, decoded)
	}
	decoded, code = runAgentCLI(t, `{"answer_markdown":"done"}`,
		"agent", "result", "--addr", env.server.URL, "--request", id, "--dispatch", dispatch)
	if code != 0 || decoded["status"] != "completed" {
		t.Fatalf("result = %d %v", code, decoded)
	}
	attempts := decoded["attempts"].([]any)
	last := attempts[len(attempts)-1].(map[string]any)
	if last["delivery"] != "sent" || last["delivery_note"] != "sent" {
		t.Fatalf("delivery not recorded on the completed attempt: %v", last)
	}
}

// TestAgentCLIVaultFlagAddressesRegisteredVault verifies the --vault flag: the CLI reads the request
// record from the addressed vault and sends ?vault=NAME to the web API, so a report never lands in
// the wrong vault.
func TestAgentCLIVaultFlagAddressesRegisteredVault(t *testing.T) {
	env := newAgentEnvRegistry(t, "alpha",
		map[string]string{"alpha": t.TempDir(), "beta": t.TempDir()},
		map[string]config.AgentConfig{"a": {Token: "token-a"}})

	id, dispatch := createRequest(t, env.server.URL+"/api/requests?vault=beta", `{"intent":"explain","instruction":"x","agent_id":"a"}`)

	decoded, code := runAgentCLI(t, "", "agent", "claim", "--addr", env.server.URL, "--vault", "beta", "--request", id, "--dispatch", dispatch)
	if code != 0 {
		t.Fatalf("claim via --vault beta = %d: %v", code, decoded)
	}
	if decoded["status"] != "running" {
		t.Fatalf("claim status = %v, want running", decoded["status"])
	}
	if decoded["vault"] != "beta" {
		t.Fatalf("claimed request vault = %v, want beta", decoded["vault"])
	}
}
