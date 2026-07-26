package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
)

// selectedVault is the registry name chosen with the global --vault flag for this invocation; empty
// means the default vault or a TRACK_VAULT path. It names the vault in error messages and puts the
// cross-vault maintenance commands back into single-vault mode; it is deliberately not what
// requireVaultDir gates on, since a missing directory is just as wrong when it was reached by
// default.
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

// activeVaultName is the registry name of the active vault, or "" when it is not a registered one
// (the default vault, or a direnv-style TRACK_VAULT path).
//
// A --vault selection answers first. It is this process's own state, and it still names the vault
// when the registered directory cannot be resolved — an unmounted drive, a stale entry — where the
// config's canonical-path match comes up empty and `track vault current` would report no name for a
// vault the user just selected by name.
//
// The registry argument is no longer read: config.Config resolves its own name at load. It stays
// only because internal/cli/admin_commands.go still passes it.
func activeVaultName(cfg *config.Config, _ map[string]string) string {
	if selectedVault != "" {
		return selectedVault
	}
	return cfg.VaultName
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
	return emit(map[string]any{
		"name":   activeVaultName(cfg, vaults),
		"path":   cfg.VaultDirDisplay,
		"source": vaultSource(cfg),
	})
}

// vaultSource names what selected the active vault, in the precedence order config.Load applies:
// "flag" (--vault NAME), "env" (TRACK_VAULT), "default_vault" (the registry name in the machine
// config), "vault_dir" (a registry-less machine config), or "default" ($HOME/track, ADR 0015).
//
// It is reported because that precedence is otherwise invisible. A TRACK_VAULT exported once in a
// shell profile makes default_vault inert for every later command, and the name and path alone read
// as a perfectly ordinary configured vault — the user only finds out when a write lands somewhere
// unexpected.
func vaultSource(cfg *config.Config) string {
	switch {
	case selectedVault != "":
		return "flag"
	case os.Getenv("TRACK_VAULT") != "":
		return "env"
	case cfg.VaultName != "":
		// With a registry the active vault can only have been named by default_vault: vault_dir is
		// refused alongside one, and nothing else picked this path.
		return "default_vault"
	}
	// Nothing names the remaining two apart, so tell them apart by path: the fallback is exactly
	// $HOME/track. A vault_dir that spells out that same path reads as "default", which is what it is.
	if home, err := os.UserHomeDir(); err == nil {
		if fallback, err := config.CanonicalPath(filepath.Join(home, "track")); err == nil && fallback == cfg.VaultDir {
			return "default"
		}
	}
	return "vault_dir"
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

// requireVaultDir refuses to operate on a directory that is not a vault, however that vault was
// reached: --vault NAME, TRACK_VAULT, --path, or the configured default. Only `track init` creates a
// vault, because every note writer MkdirAlls its own parents (createTitledNote, the sidecar writer,
// the journal) and would otherwise populate whatever path it was handed.
//
// Two directories are let through. One that already carries part of the vault layout — a .track/ (ADR
// 0061's marker, the same rule --path derivation and the Neovim plugin use) or any of the kind
// directories — is a vault, whether `track init` laid it down, a sync restored it, or a test wrote a
// template into it. An empty one is what a caller means when it hands over a freshly made path, which
// is the shape CI's TRACK_VAULT="$(mktemp -d)" steps and agent scripts use; refusing those would make
// `track init` mandatory on the very flow ADR 0061 promotes for addressing an unregistered vault.
//
// Anything else already holds someone else's files. A missing path is a typo or an unmounted drive far
// more often than a first launch, and scattering note/ and .track/ through an unrelated directory is
// the same mistake one step later.
func requireVaultDir(cfg *config.Config) error {
	entries, err := os.ReadDir(cfg.VaultDir)
	switch {
	case os.IsNotExist(err):
		if name := activeVaultName(cfg, nil); name != "" {
			return fmt.Errorf("vault %q is registered at %s but the directory does not exist (unmounted? 'track init --vault %s' creates it)", name, cfg.VaultDirDisplay, name)
		}
		return fmt.Errorf("vault directory %s does not exist ('track init' creates it)", cfg.VaultDirDisplay)
	case err != nil:
		return fmt.Errorf("read vault directory %s: %w", cfg.VaultDirDisplay, err)
	}
	if holdsVaultLayout(cfg) || looksEmpty(entries) {
		return nil
	}
	return fmt.Errorf("%s is not a track vault and is not empty ('track init' makes it one)", cfg.VaultDirDisplay)
}

// holdsVaultLayout reports whether any part of the vault layout is already there: the .track/ marker
// on its own, or one of the directories init creates. VaultSkeleton is the enumeration, so a kind
// added later is covered without touching this.
func holdsVaultLayout(cfg *config.Config) bool {
	for _, dir := range append([]string{cfg.TrackDir()}, cfg.VaultSkeleton()...) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// looksEmpty reports whether a directory holds nothing its owner would call content. .DS_Store is
// Finder's, not theirs: a directory it is the sole occupant of is one they see as empty, and failing
// on it would turn "I made a folder for this" into a refusal nobody can explain.
func looksEmpty(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.Name() != ".DS_Store" {
			return false
		}
	}
	return true
}

// applyPathVault points the active vault at the one a --path argument lives in. A note always sits
// directly under <vault>/note/ or <vault>/journal/, so the path names its vault outright: the root is
// two levels up, confirmed by a .track/ beside it. This is the same rule the Neovim plugin resolves a
// buffer's vault by, so a command addressing a file and an editor editing it agree.
//
// It only ever turns a hard error into the right answer. A --path outside the active vault is refused
// today (KindFromPath anchors the path against the vault, so it is "not a vault note"), so nothing
// that works now changes meaning. An explicit --vault wins, and a path that names no vault is left
// alone for the command to reject as before.
//
// Commands that name no file — search, new --title, notes, query — derive nothing and still take
// --vault or TRACK_VAULT. That is deliberate: they are exactly the ones where a wrong guess would
// write to the wrong vault.
func applyPathVault(args []string) {
	if selectedVault != "" {
		return
	}
	path := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--path":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(args[i], "--path="):
			path = strings.TrimPrefix(args[i], "--path=")
		}
	}
	if strings.TrimSpace(path) == "" {
		return
	}
	root, ok := vaultRootOf(path)
	if !ok {
		return
	}
	// Already the active vault: leave the selection as configured, so user-facing paths keep the
	// spelling the config used rather than the one this argument happened to have.
	if cfg, err := config.Load(); err == nil {
		if same, err := config.CanonicalPath(root); err == nil && same == cfg.VaultDir {
			return
		}
	}
	os.Setenv("TRACK_VAULT", root)
}

// vaultRootOf returns the vault a note path belongs to: two directories up, if that directory holds a
// .track/. The marker is the directory, not its config.yml — a vault's config is optional.
func vaultRootOf(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	parent := filepath.Dir(abs)
	switch filepath.Base(parent) {
	case config.KindNote, config.KindJournal:
	default:
		return "", false
	}
	root := filepath.Dir(parent)
	if info, err := os.Stat(filepath.Join(root, ".track")); err != nil || !info.IsDir() {
		return "", false
	}
	return root, true
}
