package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/note"
)

func cmdBabel(args []string) int {
	if len(args) == 0 {
		return fail("babel: expected a subcommand (exec)")
	}
	switch args[0] {
	case "exec":
		return cmdBabelExec(args[1:])
	default:
		return fail("babel: unknown subcommand %q", args[0])
	}
}

func cmdBabelExec(args []string) int {
	fs := flag.NewFlagSet("babel exec", flag.ContinueOnError)
	path := fs.String("path", "", "note path")
	id := fs.Int64("id", 0, "note id (alternative to --path)")
	name := fs.String("name", "", "block :name to run")
	ordinal := fs.Int("ordinal", -1, "0-based block index to run (alternative to --name)")
	yes := fs.Bool("yes", false, "confirm execution for blocks with :eval query")
	timeout := fs.Duration("timeout", 30*time.Second, "max run time per block (0 = no limit)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	notePath := *path
	if notePath == "" {
		if *id == 0 {
			return fail("--path or --id is required")
		}
		notePath = cfg.NotePath(*id)
	}

	n, err := note.ParseFile(notePath, cfg)
	if err != nil {
		return fail("read note: %v", err)
	}

	blocks := babel.ParseBlocks(n.Body)
	if err := babel.Validate(blocks); err != nil {
		return fail("%v", err)
	}

	block, err := selectBlock(blocks, *name, *ordinal)
	if err != nil {
		return fail("%v", err)
	}

	workDir, err := resolveDir(filepath.Dir(notePath), cfg.VaultDir, firstHeader(block, "dir"))
	if err != nil {
		return fail("%v", err)
	}

	res, err := babel.NewRunner(cfg.BabelLanguages).Run(block, babel.RunOptions{
		Dir:       workDir,
		Confirmed: *yes,
		Timeout:   *timeout,
	})
	if err != nil {
		switch {
		case errors.Is(err, babel.ErrEvalDisabled):
			return fail("block has :eval no; not executed")
		case errors.Is(err, babel.ErrConfirmRequired):
			return fail("block has :eval query; pass --yes to run it")
		case errors.Is(err, babel.ErrNoExecutor):
			return fail("no executor configured for language %q (set TRACK_BABEL_%s)", block.Language, strings.ToUpper(block.Language))
		default:
			return fail("run block: %v", err)
		}
	}

	blockID := block.ID(n.ID)
	stored := shouldStore(block.HeaderArgs["results"])
	if stored {
		bm := block.Meta()
		bm.LastRun = &res
		if n.Meta.Blocks == nil {
			n.Meta.Blocks = map[string]babel.BlockMeta{}
		}
		n.Meta.Blocks[blockID] = bm
		if err := note.WriteMetadata(cfg.MetadataPath(n.ID), n.Meta); err != nil {
			return fail("store result: %v", err)
		}
	}

	return emit(map[string]any{
		"id":        blockID,
		"language":  block.Language,
		"status":    res.Status,
		"exit_code": res.ExitCode,
		"stdout":    res.Stdout,
		"stderr":    res.Stderr,
		"stored":    stored,
	})
}

// selectBlock picks the block to run: by :name, by ordinal, or the sole block when neither is given.
func selectBlock(blocks []babel.Block, name string, ordinal int) (babel.Block, error) {
	if len(blocks) == 0 {
		return babel.Block{}, fmt.Errorf("note has no source blocks")
	}
	if name != "" {
		for _, b := range blocks {
			if b.Name == name {
				return b, nil
			}
		}
		return babel.Block{}, fmt.Errorf("no block named %q", name)
	}
	if ordinal >= 0 {
		for _, b := range blocks {
			if b.Ordinal == ordinal {
				return b, nil
			}
		}
		return babel.Block{}, fmt.Errorf("no block at ordinal %d", ordinal)
	}
	if len(blocks) == 1 {
		return blocks[0], nil
	}
	return babel.Block{}, fmt.Errorf("note has %d blocks; pass --name or --ordinal", len(blocks))
}

// resolveDir resolves a block's :dir relative to the note directory and refuses paths outside the vault.
func resolveDir(noteDir, vaultDir, dirArg string) (string, error) {
	if dirArg == "" {
		return noteDir, nil
	}
	candidate := dirArg
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(noteDir, candidate)
	}
	candidate = filepath.Clean(candidate)
	vaultClean := filepath.Clean(vaultDir)
	if candidate != vaultClean && !strings.HasPrefix(candidate, vaultClean+string(filepath.Separator)) {
		return "", fmt.Errorf(":dir %q resolves outside the vault", dirArg)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf(":dir %q: %v", dirArg, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf(":dir %q is not a directory", dirArg)
	}
	return candidate, nil
}

// shouldStore reports whether a run result should be written to the sidecar, honoring :results tokens.
func shouldStore(results []string) bool {
	for _, r := range results {
		switch r {
		case "none", "discard", "silent":
			return false
		}
	}
	return true
}

func firstHeader(b babel.Block, key string) string {
	if vs := b.HeaderArgs[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}
