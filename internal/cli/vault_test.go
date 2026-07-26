package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runWithRegistry runs one Run invocation against a machine config whose vaults: registry maps the
// given names to paths, with the default vault pointed at defaultVault. Each call gets a fresh
// cache dir; use runWithRegistryCache when a test needs index state to persist across calls the
// way a real machine's stable cache does.
func runWithRegistry(t *testing.T, defaultVault string, registry map[string]string, args ...string) (map[string]any, int) {
	t.Helper()
	return runWithRegistryCache(t, filepath.Join(t.TempDir(), "cache"), defaultVault, registry, args...)
}

func runWithRegistryCache(t *testing.T, cacheDir, defaultVault string, registry map[string]string, args ...string) (map[string]any, int) {
	t.Helper()
	body := "vaults:\n"
	for name, path := range registry {
		body += "  " + name + ": " + path + "\n"
	}
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", defaultVault)
	t.Setenv("TRACK_DB_PATH", "")
	t.Setenv("TRACK_CACHE_DIR", cacheDir)
	out, code := capture(t, func() int { return Run(args) })
	decoded := decodeJSON(t, out)
	return decoded, code
}

func decodeJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q (err %v)", out, err)
	}
	return decoded
}

func TestVaultFlagSelectsRegisteredVault(t *testing.T) {
	defaultVault := t.TempDir()
	work := t.TempDir()
	if _, code := runWithRegistry(t, defaultVault, map[string]string{"work": work},
		"--vault", "work", "new", "--title", "In work", "--body", "b"); code != 0 {
		t.Fatal("new --vault work failed")
	}
	// The note landed in the registered vault, not the default one.
	notes, _ := filepath.Glob(filepath.Join(work, "note", "*.md"))
	if len(notes) != 1 {
		t.Fatalf("want 1 note in the selected vault, got %v", notes)
	}
	if stray, _ := filepath.Glob(filepath.Join(defaultVault, "note", "*.md")); len(stray) != 0 {
		t.Fatalf("default vault must stay untouched, got %v", stray)
	}
}

func TestVaultFlagRejectsUnknownName(t *testing.T) {
	decoded, code := runWithRegistry(t, t.TempDir(), map[string]string{"work": t.TempDir()},
		"--vault=blog", "notes")
	if code == 0 || decoded["error"] == nil {
		t.Fatalf("unknown vault name must fail, got %v", decoded)
	}
}

func TestVaultFlagRefusesMissingVaultDir(t *testing.T) {
	// A registered path that does not exist (unmounted drive, stale entry) must never be
	// auto-created by an ordinary command; only track init creates it explicitly.
	missing := filepath.Join(t.TempDir(), "unmounted")
	decoded, code := runWithRegistry(t, t.TempDir(), map[string]string{"work": missing},
		"--vault", "work", "new", "--title", "x", "--body", "b")
	if code == 0 || decoded["error"] == nil {
		t.Fatalf("missing vault dir must fail, got %v", decoded)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing vault dir must not be created, stat err=%v", err)
	}

	if _, code := runWithRegistry(t, t.TempDir(), map[string]string{"work": missing},
		"--vault", "work", "init"); code != 0 {
		t.Fatal("track init --vault work should create the vault explicitly")
	}
	if _, err := os.Stat(filepath.Join(missing, "note")); err != nil {
		t.Fatalf("init should have laid down the skeleton: %v", err)
	}
}

func TestVaultListCurrentWhich(t *testing.T) {
	defaultVault := t.TempDir()
	work := t.TempDir()
	blog := t.TempDir()
	registry := map[string]string{"work": work, "blog": blog}

	decoded, code := runWithRegistry(t, defaultVault, registry, "--vault", "work", "vault", "list")
	if code != 0 {
		t.Fatalf("vault list failed: %v", decoded)
	}
	active := decoded["active"].(map[string]any)
	if active["name"] != "work" {
		t.Fatalf("active = %v, want work", active)
	}
	rows := decoded["vaults"].([]any)
	if len(rows) != 2 {
		t.Fatalf("want 2 registered vaults, got %v", rows)
	}
	first := rows[0].(map[string]any) // sorted by name: blog, work
	second := rows[1].(map[string]any)
	if first["name"] != "blog" || first["active"] != false || second["name"] != "work" || second["active"] != true {
		t.Fatalf("unexpected rows: %v", rows)
	}

	// Without --vault the active vault is the default one: unregistered, so its name is empty.
	decoded, _ = runWithRegistry(t, defaultVault, registry, "vault", "current")
	if decoded["name"] != "" || decoded["path"] != defaultVault {
		t.Fatalf("current = %v, want unnamed default %s", decoded, defaultVault)
	}

	// A TRACK_VAULT path that matches a registered vault resolves back to its name.
	decoded, _ = runWithRegistry(t, blog, registry, "vault", "current")
	if decoded["name"] != "blog" {
		t.Fatalf("current = %v, want blog", decoded)
	}

	decoded, _ = runWithRegistry(t, defaultVault, registry, "vault", "which", "work")
	if decoded["name"] != "work" || decoded["path"] != work {
		t.Fatalf("which = %v, want work at %s", decoded, work)
	}
	if decoded, code := runWithRegistry(t, defaultVault, registry, "vault", "which", "nope"); code == 0 || decoded["error"] == nil {
		t.Fatalf("which on an unknown name must fail, got %v", decoded)
	}
}

// runWithMachineConfig runs one invocation against a machine config written verbatim, with no
// TRACK_VAULT in the environment — the only way to exercise the config-selected vault sources.
func runWithMachineConfig(t *testing.T, body string, args ...string) map[string]any {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB_PATH", "")
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	out, _ := capture(t, func() int { return Run(args) })
	return decodeJSON(t, out)
}

// `track vault current` reports what selected the vault. Name and path alone cannot show it: a
// TRACK_VAULT exported once in a shell profile makes default_vault inert for every later command and
// still reads as a perfectly ordinary configured vault.
func TestVaultCurrentReportsSelectionSource(t *testing.T) {
	defaultVault := t.TempDir()
	work := t.TempDir()
	registry := map[string]string{"work": work}

	decoded, _ := runWithRegistry(t, defaultVault, registry, "--vault", "work", "vault", "current")
	if decoded["source"] != "flag" {
		t.Fatalf("--vault should report source=flag, got %v", decoded)
	}
	decoded, _ = runWithRegistry(t, defaultVault, registry, "vault", "current")
	if decoded["source"] != "env" {
		t.Fatalf("TRACK_VAULT should report source=env, got %v", decoded)
	}

	// The same vault, selected by the machine config instead of the environment.
	decoded = runWithMachineConfig(t, "vaults:\n  work: "+work+"\ndefault_vault: work\n", "vault", "current")
	if decoded["source"] != "default_vault" || decoded["name"] != "work" {
		t.Fatalf("default_vault should report source=default_vault, got %v", decoded)
	}

	decoded = runWithMachineConfig(t, "vault_dir: "+work+"\n", "vault", "current")
	if decoded["source"] != "vault_dir" {
		t.Fatalf("a registry-less config should report source=vault_dir, got %v", decoded)
	}

	// Nothing configured and nothing in the environment: the $HOME/track fallback (ADR 0015).
	t.Setenv("HOME", t.TempDir())
	decoded = runWithMachineConfig(t, "", "vault", "current")
	if decoded["source"] != "default" {
		t.Fatalf("the $HOME/track fallback should report source=default, got %v", decoded)
	}
}

func TestMaintenanceSweepsRegistry(t *testing.T) {
	defaultVault := t.TempDir()
	work := t.TempDir()
	missing := filepath.Join(t.TempDir(), "unmounted")
	registry := map[string]string{"work": work, "gone": missing}

	// Seed one note per reachable vault so the sweep has something to index.
	if _, code := runWithRegistry(t, defaultVault, nil, "new", "--title", "Default note", "--body", "d"); code != 0 {
		t.Fatal("seed default vault")
	}
	if _, code := runWithRegistry(t, work, nil, "new", "--title", "Work note", "--body", "w"); code != 0 {
		t.Fatal("seed work vault")
	}

	decoded, code := runWithRegistry(t, defaultVault, registry, "refresh-all")
	if code != 0 {
		t.Fatalf("refresh-all sweep failed: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("aggregate ok must drop for the unreachable vault, got %v", decoded)
	}
	rows := decoded["vaults"].([]any)
	// Unregistered active vault first, then registered names sorted: gone, work.
	if len(rows) != 3 {
		t.Fatalf("want 3 rows (default + 2 registered), got %v", rows)
	}
	byName := map[string]map[string]any{}
	for _, r := range rows {
		row := r.(map[string]any)
		byName[row["name"].(string)] = row
	}
	if byName[""]["reindex"].(map[string]any)["indexed"].(float64) < 1 {
		t.Fatalf("default vault row should have indexed notes: %v", byName[""])
	}
	if byName["work"]["ok"] != true {
		t.Fatalf("work vault should be healthy: %v", byName["work"])
	}
	if byName["gone"]["error"] == nil {
		t.Fatalf("unreachable vault must carry an error: %v", byName["gone"])
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("sweep must not create the unreachable vault, stat err=%v", err)
	}

	// --vault scopes maintenance back to one vault with the single-vault output shape.
	decoded, code = runWithRegistry(t, defaultVault, registry, "--vault", "work", "reindex")
	if code != 0 || decoded["vaults"] != nil || decoded["indexed"].(float64) < 1 {
		t.Fatalf("scoped reindex should use the single-vault contract, got %v", decoded)
	}

	// doctor sweeps read-only, but --fix must not fan out over every vault.
	if decoded, code := runWithRegistry(t, defaultVault, registry, "doctor", "--fix"); code == 0 || decoded["error"] == nil {
		t.Fatalf("doctor --fix without --vault must fail under a registry, got %v", decoded)
	}
	decoded, code = runWithRegistry(t, defaultVault, registry, "doctor")
	if code != 0 || len(decoded["vaults"].([]any)) != 3 {
		t.Fatalf("doctor sweep should report every vault, got %v", decoded)
	}
}

func TestFederatedSearchAcrossVaults(t *testing.T) {
	defaultVault := t.TempDir()
	work := t.TempDir()
	missing := filepath.Join(t.TempDir(), "unmounted")
	registry := map[string]string{"work": work, "gone": missing}

	if _, code := runWithRegistry(t, defaultVault, nil, "new", "--title", "Shared topic home", "--body", "the needle lives here"); code != 0 {
		t.Fatal("seed default vault")
	}
	if _, code := runWithRegistry(t, work, nil, "new", "--title", "Shared topic work", "--body", "another needle body"); code != 0 {
		t.Fatal("seed work vault")
	}

	// Title search crosses vaults and labels each hit with its vault name.
	decoded, code := runWithRegistry(t, defaultVault, registry, "search", "--query", "Shared topic")
	if code != 0 {
		t.Fatalf("federated search failed: %v", decoded)
	}
	results := decoded["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("want hits from both vaults, got %v", results)
	}
	byVault := map[string]map[string]any{}
	for _, r := range results {
		row := r.(map[string]any)
		vault, _ := row["vault"].(string)
		byVault[vault] = row
	}
	if byVault[""] == nil || byVault[""]["title"] != "Shared topic home" {
		t.Fatalf("active vault hit missing or unlabeled: %v", results)
	}
	if byVault["work"] == nil || byVault["work"]["title"] != "Shared topic work" {
		t.Fatalf("work vault hit missing: %v", results)
	}
	if p, _ := byVault["work"]["path"].(string); !strings.HasPrefix(p, canonicalTestPath(t, work)) {
		t.Fatalf("work hit path should resolve inside the work vault, got %q", p)
	}
	unavailable := decoded["unavailable"].([]any)
	if len(unavailable) != 1 || unavailable[0].(map[string]any)["name"] != "gone" {
		t.Fatalf("unreachable vault should be reported: %v", decoded["unavailable"])
	}

	// Body search (FTS) crosses too and carries line/snippet from each vault's own files.
	decoded, _ = runWithRegistry(t, defaultVault, registry, "search", "--query", "needle", "--scope", "body")
	results = decoded["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("want body hits from both vaults, got %v", results)
	}
	for _, r := range results {
		row := r.(map[string]any)
		if row["line"].(float64) < 1 || row["snippet"] == "" {
			t.Fatalf("body hit should carry line and snippet: %v", row)
		}
	}

	// --vault scopes search back to one vault: single-vault contract, no vault labels.
	decoded, _ = runWithRegistry(t, defaultVault, registry, "--vault", "work", "search", "--query", "Shared topic")
	results = decoded["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["vault"] != nil {
		t.Fatalf("scoped search should be single-vault and unlabeled, got %v", results)
	}
	if decoded["unavailable"] != nil {
		t.Fatalf("scoped search must keep the single-vault shape, got %v", decoded)
	}
}

func TestFederatedSearchCoversMoreVaultsThanSQLiteCanAttach(t *testing.T) {
	// Cross-vault search used to be one query over every vault's index ATTACHed to a single
	// connection, and SQLite attaches at most 10: past that, the surplus vaults dropped out and the
	// command answered from the first ten without saying so. Per-vault queries merged in Go have no
	// such ceiling, and a vault that is simply empty of hits is still a vault that was asked.
	cache := filepath.Join(t.TempDir(), "cache") // stable across calls, like a real machine
	registry := map[string]string{}
	for i := range 12 {
		name := fmt.Sprintf("v%02d", i)
		registry[name] = t.TempDir()
		if _, code := runWithRegistryCache(t, cache, registry[name], nil,
			"new", "--title", "Shared topic "+name, "--body", "b"); code != 0 {
			t.Fatalf("seed vault %s", name)
		}
	}

	decoded, code := runWithRegistryCache(t, cache, registry["v00"], registry, "search", "--query", "Shared topic")
	if code != 0 {
		t.Fatalf("federated search failed: %v", decoded)
	}
	seen := map[string]bool{}
	for _, r := range decoded["results"].([]any) {
		vault, _ := r.(map[string]any)["vault"].(string)
		seen[vault] = true
	}
	if len(seen) != len(registry) {
		t.Fatalf("every registered vault must be searched, got hits from %d of %d: %v", len(seen), len(registry), seen)
	}
}

func TestCrossVaultRefsResolveAndBacklinks(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	cache := filepath.Join(t.TempDir(), "cache") // stable across calls, like a real machine
	registry := map[string]string{"home": home, "work": work}

	// work has the target; home references it as [[work:Target note]].
	decoded, code := runWithRegistryCache(t, cache, work, registry, "new", "--title", "Target note", "--body", "the target")
	if code != 0 {
		t.Fatalf("seed work: %v", decoded)
	}
	targetID := int64(decoded["id"].(float64))
	if _, code := runWithRegistryCache(t, cache, home, registry, "new", "--title", "Pointer", "--body", "see [[work:Target note]]"); code != 0 {
		t.Fatal("seed home")
	}

	// resolve crosses: the qualified term resolves to the work vault's note and path.
	decoded, code = runWithRegistryCache(t, cache, home, registry, "resolve", "--term", "work:Target note")
	if code != 0 || decoded["found"] != true || decoded["vault"] != "work" {
		t.Fatalf("qualified resolve failed: %v", decoded)
	}
	if int64(decoded["note_id"].(float64)) != targetID {
		t.Fatalf("resolved wrong note: %v", decoded)
	}
	if p := decoded["path"].(string); !strings.HasPrefix(p, canonicalTestPath(t, work)) {
		t.Fatalf("resolved path should live in work, got %q", p)
	}

	// A reachable vault without the title is found=false, not an error.
	decoded, code = runWithRegistryCache(t, cache, home, registry, "resolve", "--term", "work:No such note")
	if code != 0 || decoded["found"] != false {
		t.Fatalf("missing title should be found=false: %v", decoded)
	}

	// backlinks on the target (in work) reports the home note as an external inbound ref.
	decoded, code = runWithRegistryCache(t, cache, work, registry, "--vault", "work", "backlinks", "--id", decodeID(t, targetID))
	if code != 0 {
		t.Fatalf("backlinks failed: %v", decoded)
	}
	external := decoded["external"].([]any)
	if len(external) != 1 {
		t.Fatalf("want 1 external backlink, got %v", decoded)
	}
	ext := external[0].(map[string]any)
	if ext["vault"] != "home" || ext["title"] != "Pointer" {
		t.Fatalf("unexpected external backlink: %v", ext)
	}
	if len(decoded["unavailable"].([]any)) != 0 {
		t.Fatalf("both vaults reachable, got unavailable: %v", decoded["unavailable"])
	}
}

// decodeID formats an id for CLI args.
func decodeID(t *testing.T, id int64) string {
	t.Helper()
	return strconv.FormatInt(id, 10)
}

// A --path names its own vault: a note sits directly under <vault>/note/, so the path says which
// vault it belongs to without any search. This is what makes a command addressing a file agree with
// the editor editing it, and it only ever replaces a hard error — a path outside the active vault is
// "not a vault note" today.
func TestPathOutsideTheActiveVaultSelectsItsOwnVault(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	if _, code := runIn(t, home, "new", "--title", "Mine", "--id", "100", "--body", "# Mine\n"); code != 0 {
		t.Fatal("new in the home vault failed")
	}
	if _, code := runIn(t, other, "new", "--title", "Theirs", "--id", "200", "--body", "# Theirs\n"); code != 0 {
		t.Fatal("new in the other vault failed")
	}

	// Active vault is `home`; the path points into `other`.
	notePath := filepath.Join(other, "note", "200.md")
	out, code := runIn(t, home, "meta", "--path", notePath)
	if code != 0 {
		t.Fatalf("meta on another vault's note should work, got code=%d out=%v", code, out)
	}
	if out["title"] != "Theirs" {
		t.Fatalf("the path's own vault should have answered, got %v", out)
	}

	// The sidecar it wrote belongs to that vault, not the active one.
	if _, code := runIn(t, home, "meta", "--path", notePath, "--description", "from outside"); code != 0 {
		t.Fatalf("meta write failed")
	}
	raw, err := os.ReadFile(filepath.Join(other, ".track", "notes", "200.yaml"))
	if err != nil {
		t.Fatalf("read the other vault's sidecar: %v", err)
	}
	if !strings.Contains(string(raw), "from outside") {
		t.Fatalf("the write landed elsewhere: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(home, ".track", "notes", "200.yaml")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be written into the active vault (stat err=%v)", err)
	}
}

// An explicit --vault is a decision; a derived one is an inference. The decision wins.
func TestExplicitVaultFlagBeatsAPathDerivedOne(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	if _, code := runIn(t, other, "new", "--title", "Theirs", "--id", "200", "--body", "# Theirs\n"); code != 0 {
		t.Fatal("new in the other vault failed")
	}
	configPath := filepath.Join(t.TempDir(), "config.yml")
	body := "cache_dir: " + t.TempDir() + "\nvaults:\n  home: " + home + "\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB_PATH", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	out, code := capture(t, func() int {
		return Run([]string{"--vault", "home", "meta", "--path", filepath.Join(other, "note", "200.md")})
	})
	if !strings.Contains(out, "not a vault note") {
		t.Fatalf("--vault should have won and refused the foreign path, got %s (code %d)", out, code)
	}
}

// A path that is not in any vault stays the command's error to report, unchanged.
func TestPathInNoVaultIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	loose := filepath.Join(t.TempDir(), "note", "300.md")
	if err := os.MkdirAll(filepath.Dir(loose), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loose, []byte("# Loose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runIn(t, home, "meta", "--path", loose)
	if code != 1 || !strings.Contains(out["error"].(string), "not a vault note") {
		t.Fatalf("a directory without .track/ is not a vault, got code=%d out=%v", code, out)
	}
}

// A vault is registered under its own name too, so a note there can write [[home:Target]] for a
// note sitting beside it. That is not a cross-vault reference: it resolves locally and its backlink
// is a plain one, not an external edge that the graph would never see.
func TestSelfQualifiedRefStaysLocal(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(t.TempDir(), "cache") // stable across calls, like a real machine
	registry := map[string]string{"home": home, "work": t.TempDir()}

	decoded, code := runWithRegistryCache(t, cache, home, registry, "new", "--title", "Target note", "--body", "the target")
	if code != 0 {
		t.Fatalf("seed target: %v", decoded)
	}
	targetID := int64(decoded["id"].(float64))
	if _, code := runWithRegistryCache(t, cache, home, registry, "new", "--title", "Pointer", "--body", "see [[home:Target note]]"); code != 0 {
		t.Fatal("seed pointer")
	}

	decoded, code = runWithRegistryCache(t, cache, home, registry, "resolve", "--term", "home:Target note")
	if code != 0 || decoded["found"] != true {
		t.Fatalf("the vault's own name should resolve locally: %v", decoded)
	}
	if _, qualified := decoded["vault"]; qualified {
		t.Fatalf("a self-qualified term is a local hit, not a cross-vault one: %v", decoded)
	}
	if int64(decoded["note_id"].(float64)) != targetID {
		t.Fatalf("resolved wrong note: %v", decoded)
	}

	decoded, code = runWithRegistryCache(t, cache, home, registry, "backlinks", "--id", decodeID(t, targetID))
	if code != 0 {
		t.Fatalf("backlinks failed: %v", decoded)
	}
	backlinks := decoded["backlinks"].([]any)
	if len(backlinks) != 1 || backlinks[0].(map[string]any)["title"] != "Pointer" {
		t.Fatalf("want the pointer as a plain backlink, got %v", decoded)
	}
	if external := decoded["external"].([]any); len(external) != 0 {
		t.Fatalf("the vault's own name must not produce an external backlink, got %v", external)
	}
}
