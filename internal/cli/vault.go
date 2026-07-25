package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
)

// selectedVault is the registry name chosen with the global --vault flag for this invocation; empty
// means the default vault. open() uses it to gate the first-run skeleton: a registered vault whose
// directory is missing (unmounted cloud storage, a stale registry entry) must never be silently
// created the way the default vault is (ADR 0004's typo-creates-vault, one level up).
var selectedVault string

// applyVaultFlag pre-parses the global --vault NAME flag from anywhere in argv, resolves the name
// through the machine config's vaults: registry, and exports the path as TRACK_VAULT — so
// config.Load stays flag-agnostic and every command inherits the selection. An unknown name is a
// hard error listing the registered names, never a fresh vault.
func applyVaultFlag(args []string) ([]string, bool) {
	selectedVault = ""
	name := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--vault":
			if i+1 >= len(args) {
				fail("--vault needs a vault name")
				return nil, false
			}
			name = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--vault="):
			name = strings.TrimPrefix(args[i], "--vault=")
		default:
			rest = append(rest, args[i])
		}
	}
	if name == "" {
		return rest, true
	}
	vaults, err := config.Vaults()
	if err != nil {
		fail("%v", err)
		return nil, false
	}
	path, ok := vaults[name]
	if !ok {
		if len(vaults) == 0 {
			fail("unknown vault %q: no vaults are registered (add a vaults: map to the machine config)", name)
		} else {
			fail("unknown vault %q: registered vaults are %s", name, strings.Join(vaultNames(vaults), ", "))
		}
		return nil, false
	}
	os.Setenv("TRACK_VAULT", path)
	selectedVault = name
	return rest, true
}

func vaultNames(vaults map[string]string) []string {
	names := make([]string, 0, len(vaults))
	for name := range vaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// activeVaultName maps the active vault back to its registry name by canonical path, or "" when the
// active vault is not a registered one (the default vault, or a direnv-style TRACK_VAULT path).
func activeVaultName(cfg *config.Config, vaults map[string]string) string {
	if selectedVault != "" {
		return selectedVault
	}
	for _, name := range vaultNames(vaults) {
		if canonical, err := config.CanonicalPath(vaults[name]); err == nil && canonical == cfg.VaultDir {
			return name
		}
	}
	return ""
}

func cmdVault(args []string) int {
	if len(args) == 0 {
		return fail("vault: want list, current, or which <name>")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdVaultList()
	case "current":
		return cmdVaultCurrent()
	case "which":
		return cmdVaultWhich(rest)
	default:
		return fail("vault: unknown subcommand %q (want list, current, or which)", sub)
	}
}

// cmdVaultList prints the registry with the active vault marked. The active vault also appears as a
// top-level object because it may not be a registered one (default vault or a TRACK_VAULT path).
func cmdVaultList() int {
	vaults, err := config.Vaults()
	if err != nil {
		return fail("%v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	activeName := activeVaultName(cfg, vaults)
	rows := make([]map[string]any, 0, len(vaults))
	for _, name := range vaultNames(vaults) {
		rows = append(rows, map[string]any{
			"name":   name,
			"path":   vaults[name],
			"active": name == activeName,
		})
	}
	return emit(map[string]any{
		"active": map[string]any{"name": activeName, "path": cfg.VaultDirDisplay},
		"vaults": rows,
	})
}

func cmdVaultCurrent() int {
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	vaults, err := config.Vaults()
	if err != nil {
		return fail("%v", err)
	}
	return emit(map[string]any{"name": activeVaultName(cfg, vaults), "path": cfg.VaultDirDisplay})
}

// cmdVaultWhich resolves a registry name to its path without loading the vault, so it works even
// when the vault is unmounted.
func cmdVaultWhich(args []string) int {
	if len(args) != 1 {
		return fail("vault which: want exactly one vault name")
	}
	name := args[0]
	vaults, err := config.Vaults()
	if err != nil {
		return fail("%v", err)
	}
	path, ok := vaults[name]
	if !ok {
		return fail("unknown vault %q", name)
	}
	return emit(map[string]any{"name": name, "path": path})
}

// requireVaultDir refuses to operate on a --vault selection whose directory does not exist: the
// path may be an unmounted cloud drive, and auto-creating a skeleton there would bury the real
// vault when it mounts again. `track init --vault NAME` creates it explicitly.
func requireVaultDir(cfg *config.Config) error {
	if selectedVault == "" {
		return nil
	}
	if _, err := os.Stat(cfg.VaultDir); os.IsNotExist(err) {
		return fmt.Errorf("vault %q is registered at %s but the directory does not exist (unmounted? 'track init --vault %s' creates it)", selectedVault, cfg.VaultDirDisplay, selectedVault)
	}
	return nil
}
