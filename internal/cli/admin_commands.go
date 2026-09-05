package cli

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/doctor"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/webui"
)

// cmdInit creates the vault directory skeleton (note/journal trees with their assets subdirectories,
// the template directory, the canonical-data directory, and the sidecar metadata directory). It is
// idempotent and reports the directories it created, so it is safe to run on an existing vault.
func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	created, err := cfg.EnsureVaultSkeleton()
	if err != nil {
		return fail("%v", err)
	}
	if created == nil {
		created = []string{}
	}
	return emit(map[string]any{"vault": cfg.VaultDir, "created": created})
}

// vaultTarget is one vault a maintenance command sweeps: its registry name ("" for the unregistered
// active vault) and its configured path.
type vaultTarget struct {
	Name string
	Path string
}

// crossVaultTargets decides what a maintenance command (reindex, doctor, refresh-all) operates on.
// With a --vault selection or no registry it reports single-vault mode (nil targets), keeping the
// established output contract. Otherwise the registry makes these commands the cross-vault
// maintenance entry: the active vault (when unregistered) plus every registered vault.
func crossVaultTargets() ([]vaultTarget, bool, error) {
	vaults, err := config.Vaults()
	if err != nil {
		return nil, false, err
	}
	if selectedVault != "" || len(vaults) == 0 {
		return nil, false, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, false, err
	}
	var targets []vaultTarget
	if activeVaultName(cfg, vaults) == "" {
		targets = append(targets, vaultTarget{Name: "", Path: cfg.VaultDirDisplay})
	}
	for _, name := range vaultNames(vaults) {
		targets = append(targets, vaultTarget{Name: name, Path: vaults[name]})
	}
	return targets, true, nil
}

// runInVault resolves one target's config and runs fn with it. It resolves through config.LoadAt so
// selecting a vault stays local to this call: a sweep must not leave the process pointed at whichever
// target happened to come last. The vault directory must be reachable first: maintenance must never
// lay down a skeleton for — or reset the cache index of — a vault that is merely unmounted or
// unreadable, so the sweep reports it instead.
func runInVault(tgt vaultTarget, fn func(*config.Config) (map[string]any, error)) (map[string]any, error) {
	cfg, err := config.LoadAt(tgt.Path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cfg.VaultDir); err != nil {
		return nil, fmt.Errorf("vault directory unavailable: %v", err)
	}
	return fn(cfg)
}

// sweepVaults runs fn over every target, folding each result (or its error) into a per-vault row.
// A vault's failure never aborts the sweep — the row carries the error and the aggregate ok drops.
func sweepVaults(targets []vaultTarget, fn func(*config.Config) (map[string]any, error)) (rows []map[string]any, ok bool) {
	ok = true
	for _, tgt := range targets {
		row := map[string]any{"name": tgt.Name, "path": tgt.Path}
		rep, err := runInVault(tgt, fn)
		if err != nil {
			row["error"] = err.Error()
			ok = false
		} else {
			maps.Copy(row, rep)
			if repOK, has := rep["ok"].(bool); has && !repOK {
				ok = false
			}
		}
		rows = append(rows, row)
	}
	return rows, ok
}

// reindexVault resets and rebuilds one vault's index.
func reindexVault(cfg *config.Config) (map[string]any, error) {
	if err := store.Reset(cfg.DBPath); err != nil {
		return nil, fmt.Errorf("reset index db: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	rep, err := index.New(cfg, s).Full()
	if err != nil {
		return nil, fmt.Errorf("reindex: %v", err)
	}
	return map[string]any{"indexed": rep.Indexed, "deleted": rep.Deleted, "links": rep.Links}, nil
}

// doctorVault runs the read-only doctor diagnosis for one vault.
func doctorVault(cfg *config.Config) (map[string]any, error) {
	rep, err := doctor.Diagnose(cfg)
	if err != nil {
		return nil, fmt.Errorf("doctor: %v", err)
	}
	return map[string]any{"scanned": rep.Scanned, "issues": rep.Issues, "ok": len(rep.Issues) == 0}, nil
}

// refreshVault is one vault's refresh-all pass: full rebuild, then a read-only doctor report.
func refreshVault(cfg *config.Config) (map[string]any, error) {
	rix, err := reindexVault(cfg)
	if err != nil {
		return nil, err
	}
	diag, err := doctorVault(cfg)
	if err != nil {
		return nil, err
	}
	return map[string]any{"reindex": rix, "doctor": diag, "ok": diag["ok"]}, nil
}

func cmdReindex(args []string) int {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	fs.Bool("full", false, "full rebuild (default and only mode for now). The index is deleted and rebuilt\nfrom the notes and sidecars on disk; with a vaults: registry and no --vault\nthis happens to every registered vault")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	targets, cross, err := crossVaultTargets()
	if err != nil {
		return fail("%v", err)
	}
	if cross {
		rows, ok := sweepVaults(targets, reindexVault)
		return emit(map[string]any{"vaults": rows, "ok": ok})
	}

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if err := requireVaultDir(cfg); err != nil {
		return fail("%v", err)
	}
	start := time.Now()
	rep, err := reindexVault(cfg)
	if err != nil {
		return fail("%v", err)
	}
	rep["took_ms"] = time.Since(start).Milliseconds()
	return emit(rep)
}

// cmdDoctor reports vault/sidecar divergence (missing or orphan sidecars, stray conflict copies,
// duplicate titles) without touching any file. Finding issues is not an error, so it still exits 0;
// callers branch on the issues array, reserving the {"error":...}/exit 1 contract for real failures.
// With a vault registry (and no --vault selection) it diagnoses every vault, one row each.
//
// With --fix it repairs the divergence by auto-numbered restore (see doctor.Fix), then rebuilds the
// index so the cache reflects the repaired vault.
func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "repair divergence by auto-numbered restore, then reindex. Irreversible and\nlossy: a missing sidecar comes back as \"Untitled N\" with its title, tags and\nprops gone, and duplicate titles are renumbered without rewriting backlinks.\nunreadable_sidecar, property_violation and shadowed_title are never fixed.\nRefused outright while a vaults: registry is in play - pass --vault NAME,\nwhich TRACK_VAULT does not substitute for. Read the plain report first, and\ntake a track gen increment before running it")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	targets, cross, err := crossVaultTargets()
	if err != nil {
		return fail("%v", err)
	}
	if cross {
		// --fix mutates a vault; a sweep must never repair every vault in one shot.
		if *fix {
			return fail("doctor --fix repairs one vault at a time; pass --vault NAME to choose it")
		}
		rows, ok := sweepVaults(targets, doctorVault)
		return emit(map[string]any{"vaults": rows, "ok": ok})
	}

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if err := requireVaultDir(cfg); err != nil {
		return fail("%v", err)
	}

	if *fix {
		startID := time.Now().Unix() * 1000
		rep, err := doctor.Fix(cfg, startID)
		if err != nil {
			return fail("doctor --fix: %v", err)
		}
		out := map[string]any{
			"changed": rep.Changed,
			"fixed":   rep.Fixed,
			"skipped": rep.Skipped,
		}
		if rep.Changed {
			if err := store.Reset(cfg.DBPath); err != nil {
				return fail("reset index db: %v", err)
			}
			s, err := store.Open(cfg.DBPath)
			if err != nil {
				return fail("%v", err)
			}
			defer s.Close()
			ix, err := index.New(cfg, s).Full()
			if err != nil {
				return fail("reindex: %v", err)
			}
			out["reindexed"] = ix.Indexed
		}
		return emit(out)
	}

	rep, err := doctorVault(cfg)
	if err != nil {
		return fail("%v", err)
	}
	return emit(rep)
}

// cmdRefreshAll runs the whole maintenance pipeline in one idempotent pass, suitable for cron/launchd:
// it rebuilds the cache index from the on-disk notes and sidecars (reconciling deletions), then reports
// vault/sidecar divergence in read-only doctor mode. It never edits notes, so repeated runs converge and
// a doctor finding is not a failure — only real errors use the {"error":...}/exit 1 contract. With a
// vault registry (and no --vault selection) it sweeps every vault, so one cron entry maintains them all.
func cmdRefreshAll(args []string) int {
	fs := flag.NewFlagSet("refresh-all", flag.ContinueOnError)
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	start := time.Now()
	targets, cross, err := crossVaultTargets()
	if err != nil {
		return fail("%v", err)
	}
	if cross {
		rows, ok := sweepVaults(targets, refreshVault)
		return emit(map[string]any{"vaults": rows, "ok": ok, "took_ms": time.Since(start).Milliseconds()})
	}

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if err := requireVaultDir(cfg); err != nil {
		return fail("%v", err)
	}
	rep, err := refreshVault(cfg)
	if err != nil {
		return fail("%v", err)
	}
	rep["took_ms"] = time.Since(start).Milliseconds()
	delete(rep, "ok") // the single-vault contract has no top-level ok; doctor.ok carries it
	return emit(rep)
}

func cmdWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	// Claim the PID file before binding: a live server on the same addr is refused here with a clear
	// message rather than surfacing as a bind error after the server is half up. The release removes
	// the file on normal exit.
	release, err := webui.AcquireWebPID(cfg, *addr)
	if err != nil {
		return fail("web: %v", err)
	}
	defer release()

	fmt.Fprintf(os.Stderr, "track web: http://%s\n", *addr)
	if err := webui.Serve(cfg, s, *addr); err != nil {
		return fail("web: %v", err)
	}
	return 0
}

func cmdWebStop(args []string) int {
	fs := flag.NewFlagSet("web stop", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	stopped, err := webui.StopWeb(cfg, *addr)
	if err != nil {
		return fail("web stop: %v", err)
	}
	return emit(map[string]any{"stopped": stopped})
}
