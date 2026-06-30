package cli

import (
	"flag"
	"fmt"
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
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
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

func cmdReindex(args []string) int {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	fs.Bool("full", false, "full rebuild (default and only mode for now)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fail("%v", err)
	}
	if err := store.Reset(cfg.DBPath); err != nil {
		return fail("reset index db: %v", err)
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	start := time.Now()
	rep, err := index.New(cfg, s).Full()
	if err != nil {
		return fail("reindex: %v", err)
	}
	return emit(map[string]any{
		"indexed": rep.Indexed,
		"deleted": rep.Deleted,
		"links":   rep.Links,
		"took_ms": time.Since(start).Milliseconds(),
	})
}

// cmdDoctor reports vault/sidecar divergence (missing or orphan sidecars, stray conflict copies,
// duplicate titles) without touching any file. Finding issues is not an error, so it still exits 0;
// callers branch on the issues array, reserving the {"error":...}/exit 1 contract for real failures.
//
// With --fix it repairs the divergence by auto-numbered restore (see doctor.Fix), then rebuilds the
// index so the cache reflects the repaired vault.
func cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "repair divergence by auto-numbered restore, then reindex")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
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

	rep, err := doctor.Diagnose(cfg)
	if err != nil {
		return fail("doctor: %v", err)
	}
	return emit(map[string]any{
		"scanned": rep.Scanned,
		"issues":  rep.Issues,
		"ok":      len(rep.Issues) == 0,
	})
}

func cmdWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	fmt.Fprintf(os.Stderr, "track web: http://%s\n", *addr)
	if err := webui.Serve(cfg, s, *addr); err != nil {
		return fail("web: %v", err)
	}
	return 0
}
