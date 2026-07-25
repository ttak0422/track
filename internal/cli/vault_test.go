package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runWithRegistry runs one Run invocation against a machine config whose vaults: registry maps the
// given names to paths, with the default vault pointed at defaultVault.
func runWithRegistry(t *testing.T, defaultVault string, registry map[string]string, args ...string) (map[string]any, int) {
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
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
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
