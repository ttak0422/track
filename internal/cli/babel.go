package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func cmdBabel(args []string) int {
	if len(args) == 0 {
		return fail("babel: expected a subcommand (exec, run, tangle, restore)")
	}
	switch args[0] {
	// "run" is "exec" under the name the calls surface documents: run a named block, optionally
	// with --var inputs. One code path keeps the two exactly equivalent.
	case "exec", "run":
		return cmdBabelExec(args[1:])
	case "tangle":
		return cmdBabelTangle(args[1:])
	case "restore":
		return cmdBabelRestore(args[1:])
	default:
		return fail("babel: unknown subcommand %q", args[0])
	}
}

// varsFlag collects repeatable --var key=value assignments. Values may contain "=" and ",".
type varsFlag []string

func (v *varsFlag) String() string { return strings.Join(*v, ",") }

func (v *varsFlag) Set(s string) error { *v = append(*v, s); return nil }

func cmdBabelExec(args []string) int {
	fs := flag.NewFlagSet("babel exec", flag.ContinueOnError)
	path := fs.String("path", "", "note path")
	id := fs.Int64("id", 0, "note id (alternative to --path)")
	name := fs.String("name", "", "block :name to run")
	ordinal := fs.Int("ordinal", -1, "0-based block index to run (alternative to --name)")
	line := fs.Int("line", -1, "0-based line inside the block to run (e.g. the editor cursor row)")
	bodyStdin := fs.Bool("body-stdin", false, "read note body from stdin instead of disk")
	dryRun := fs.Bool("dry-run", false, "preview expanded source and resolved variables without executing or storing")
	yes := fs.Bool("yes", false, "confirm execution for blocks with :eval query")
	timeout := fs.Duration("timeout", 30*time.Second, "max run time per block (0 = no limit)")
	var cliVars varsFlag
	fs.Var(&cliVars, "var", "k=v passed to the block's environment (repeatable); overrides the block's :var")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	n, err := loadNoteArg(cfg, s, *path, *id)
	if err != nil {
		return fail("%v", err)
	}
	if *bodyStdin {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail("read stdin body: %v", err)
		}
		n.Body = string(body)
	}

	blocks := babel.ParseBlocks(n.Body)
	if err := babel.Validate(blocks); err != nil {
		return fail("%v", err)
	}

	block, err := selectBlock(blocks, *name, *ordinal, *line)
	if err != nil {
		return fail("%v", err)
	}

	workDir, err := resolveDir(filepath.Dir(n.Path), cfg.VaultDir, firstHeader(block, "dir"))
	if err != nil {
		return fail("%v", err)
	}

	if !*dryRun {
		if err := babel.CheckEval(block, *yes); err != nil {
			if errors.Is(err, babel.ErrConfirmRequired) {
				return fail("block has :eval query; pass --yes to run it")
			}
			return fail("block has :eval no; not executed")
		}
	}

	runBlock := block
	runBlock.Body, err = babel.EvaluationBody(block, blocks)
	if err != nil {
		return fail("%v", err)
	}
	vars, err := babel.ResolveVars(block, cliVars, blocks, n.ID, n.Meta.Blocks)
	if err != nil {
		return fail("%v", err)
	}
	if *dryRun {
		eval := firstHeader(block, "eval")
		if eval == "" {
			eval = "yes"
		}
		return emit(map[string]any{
			"dry_run": true, "id": block.ID(n.ID), "language": block.Language,
			"body": runBlock.Body, "vars": vars, "dir": workDir,
			"eval": eval,
		})
	}
	inputHash := babel.InputHash(block, runBlock.Body, vars)
	cacheDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return fail("resolve execution directory: %v", err)
	}
	executionKey := babel.ExecutionKey(inputHash, cacheDir, cfg.BabelLanguages[block.Language])
	blockID := block.ID(n.ID)
	stored := shouldStore(block.HeaderArgs["results"])
	meta := n.Meta.Blocks[blockID]
	if firstHeader(block, "cache") == "yes" && stored && meta.LastRun != nil && meta.LastRun.Status == "success" && meta.ExecutionKey == executionKey {
		return emit(blockRunPayload(blockID, block, *meta.LastRun, map[string]any{"stored": true, "cached": true}))
	}

	res, err := babel.NewRunner(cfg.BabelLanguages).Run(runBlock, babel.RunOptions{
		Dir:       workDir,
		Confirmed: *yes,
		Timeout:   *timeout,
		Vars:      vars,
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

	if stored {
		bm := block.Meta()
		bm.LastRun = &res
		bm.InputHash = inputHash
		bm.ExecutionKey = executionKey
		if n.Meta.Blocks == nil {
			n.Meta.Blocks = map[string]babel.BlockMeta{}
		}
		n.Meta.Blocks[blockID] = bm
		if err := note.WriteMetadata(cfg.MetadataPath(n.ID), n.Meta); err != nil {
			return fail("store result: %v", err)
		}
	}

	return emit(blockRunPayload(blockID, block, res, map[string]any{
		"stored": stored,
		"cached": false,
	}))
}

func cmdBabelRestore(args []string) int {
	fs := flag.NewFlagSet("babel restore", flag.ContinueOnError)
	path := fs.String("path", "", "note path")
	id := fs.Int64("id", 0, "note id (alternative to --path)")
	bodyStdin := fs.Bool("body-stdin", false, "read note body from stdin instead of disk")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}

	cfg, s, err := open()
	if err != nil {
		return fail("%v", err)
	}
	defer s.Close()

	n, err := loadNoteArg(cfg, s, *path, *id)
	if err != nil {
		return fail("%v", err)
	}

	if *bodyStdin {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail("read stdin body: %v", err)
		}
		n.Body = string(body)
	}
	blocks := babel.ParseBlocks(n.Body)
	if err := babel.Validate(blocks); err != nil {
		return fail("%v", err)
	}

	restored := []map[string]any{}
	for _, block := range blocks {
		blockID := block.ID(n.ID)
		result := babel.StoredResult(block, blocks, n.ID, n.Meta.Blocks)
		if result == nil {
			continue
		}

		restored = append(restored, blockRunPayload(blockID, block, *result, map[string]any{
			"stored":   true,
			"restored": true,
		}))
	}

	return emit(map[string]any{"blocks": restored})
}

func cmdBabelTangle(args []string) int {
	fs := flag.NewFlagSet("babel tangle", flag.ContinueOnError)
	path := fs.String("path", "", "source file path (alternative to --file)")
	id := fs.Int64("id", 0, "note id in the configured vault")
	outDir := fs.String("out-dir", "", "output root; defaults to a new temporary directory")
	dryRun := fs.Bool("dry-run", false, "print the tangle plan without writing files")
	var files []string
	fs.Func("file", "source file path (repeatable; no vault or config required)", func(s string) error {
		if s == "" {
			return fmt.Errorf("--file needs a path")
		}
		files = append(files, s)
		return nil
	})
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	selectors := 0
	for _, selected := range []bool{len(files) > 0, *path != "", *id != 0} {
		if selected {
			selectors++
		}
	}
	if selectors != 1 {
		return fail("select exactly one of --file (repeatable), --path, or --id")
	}
	if *path != "" {
		files = append(files, *path)
	}
	if *id != 0 {
		cfg, s, err := open()
		if err != nil {
			return fail("%v", err)
		}
		defer s.Close()
		n, err := loadNoteArg(cfg, s, "", *id)
		if err != nil {
			return fail("%v", err)
		}
		files = append(files, n.Path)
	}

	sources := make([]babel.TangleSource, 0, len(files))
	seen := make(map[string]bool)
	for _, file := range files {
		abs, err := filepath.Abs(file)
		if err != nil {
			return fail("%v", err)
		}
		abs, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return fail("source %s: %v", file, err)
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		info, err := os.Stat(abs)
		if err != nil {
			return fail("source %s: %v", file, err)
		}
		if !info.Mode().IsRegular() {
			return fail("source %s is not a regular file", file)
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			return fail("source %s: %v", file, err)
		}
		sources = append(sources, babel.TangleSource{Path: abs, Blocks: babel.ParseBlocks(string(body))})
	}

	temporary := *outDir == ""
	keep := false
	if temporary {
		dir, err := os.MkdirTemp("", "track-tangle-*")
		if err != nil {
			return fail("temporary output directory: %v", err)
		}
		*outDir = dir
		defer func() {
			if !keep {
				os.RemoveAll(dir)
			}
		}()
	}
	root, err := filepath.Abs(*outDir)
	if err != nil {
		return fail("output directory: %v", err)
	}
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		return fail("output directory %s is not a directory", root)
	} else if err != nil && !os.IsNotExist(err) {
		return fail("output directory: %v", err)
	}
	existing := make(map[string]os.FileInfo)
	plan, err := babel.PlanTangle(sources, func(_, target string) (string, error) {
		path, err := babel.ResolveTangleOutputPath(root, target)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		// ponytail: quadratic existing-file checks; index inode identities if large plans need it.
		for previous, other := range existing {
			if os.SameFile(info, other) {
				return previous, nil
			}
		}
		existing[path] = info
		return path, nil
	})
	if err != nil {
		return fail("%v", err)
	}
	// Inputs remain intact, including when an existing output is a hard link to an input.
	for _, target := range plan {
		outputInfo, err := os.Stat(target.Path)
		if err != nil && !os.IsNotExist(err) {
			return fail("%v", err)
		}
		for _, source := range sources {
			inputInfo, err := os.Stat(source.Path)
			if err != nil {
				return fail("%v", err)
			}
			if target.Path == source.Path || (outputInfo != nil && os.SameFile(outputInfo, inputInfo)) {
				return fail("tangle %s overwrites input %s", target.Path, source.Path)
			}
		}
	}

	targets := make([]map[string]any, 0, len(plan))
	for _, t := range plan {
		targets = append(targets, map[string]any{
			"path": t.Path, "blocks": t.Blocks, "bytes": len(t.Content),
			"source": t.Source, "overridden": t.Overridden,
		})
	}
	// Validate the entire plan before creating output directories or truncating any file.
	if !*dryRun {
		for _, t := range plan {
			if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
				return fail("tangle %s: %v", t.Path, err)
			}
			if err := os.WriteFile(t.Path, []byte(t.Content), 0o644); err != nil {
				return fail("tangle %s: %v", t.Path, err)
			}
		}
		keep = true
	}
	return emit(map[string]any{"targets": targets, "dry_run": *dryRun, "output_dir": root, "temporary": temporary})
}

// loadNoteArg resolves the shared --path / --id note selection of the babel subcommands.
func loadNoteArg(cfg *config.Config, s *store.Store, path string, id int64) (*note.Note, error) {
	if path == "" {
		if id == 0 {
			return nil, fmt.Errorf("--path or --id is required")
		}
		var err error
		path, err = resolveNotePath(cfg, s, id, "", "")
		if err != nil {
			return nil, err
		}
	}
	// Config canonicalizes the vault; explicit note paths must use the same spelling
	// (for example /var versus /private/var on macOS) before containment checks.
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve note path: %w", err)
	}
	n, err := note.ParseFile(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("read note: %v", err)
	}
	return n, nil
}

// selectBlock picks the block to run: by :name, by ordinal, by a line inside it, or the sole block.
func selectBlock(blocks []babel.Block, name string, ordinal, line int) (babel.Block, error) {
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
	if line >= 0 {
		for _, b := range blocks {
			if line >= b.StartLine && line <= b.EndLine {
				return b, nil
			}
		}
		return babel.Block{}, fmt.Errorf("no source block at line %d", line)
	}
	if len(blocks) == 1 {
		return blocks[0], nil
	}
	return babel.Block{}, fmt.Errorf("note has %d blocks; pass --name, --ordinal, or --line", len(blocks))
}

// resolveDir resolves a block's :dir relative to the note directory and refuses paths outside the vault.
func resolveDir(noteDir, vaultDir, dirArg string) (string, error) {
	noteDir, err := filepath.Abs(noteDir)
	if err != nil {
		return "", fmt.Errorf(":dir %q: %w", dirArg, err)
	}
	vaultClean, err := filepath.Abs(vaultDir)
	if err != nil {
		return "", fmt.Errorf(":dir %q: %w", dirArg, err)
	}
	candidate := dirArg
	if candidate == "" {
		candidate = noteDir
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(noteDir, candidate)
	}
	candidate = filepath.Clean(candidate)
	rel, err := filepath.Rel(vaultClean, candidate)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf(":dir %q resolves outside the vault", dirArg)
	}
	resolvedVault, err := filepath.EvalSymlinks(vaultClean)
	if err != nil {
		return "", fmt.Errorf(":dir %q: %w", dirArg, err)
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf(":dir %q: %w", dirArg, err)
	}
	rel, err = filepath.Rel(resolvedVault, candidate)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf(":dir %q escapes the vault through a symlink", dirArg)
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

func blockRunPayload(blockID string, block babel.Block, res babel.RunResult, extra map[string]any) map[string]any {
	payload := map[string]any{
		"id":          blockID,
		"language":    block.Language,
		"status":      res.Status,
		"exit_code":   res.ExitCode,
		"stdout":      res.Stdout,
		"stderr":      res.Stderr,
		"value":       res.Value,
		"files":       res.Files,
		"display":     babel.DisplayResult(block.HeaderArgs["results"]),
		"started_at":  res.StartedAt,
		"finished_at": res.FinishedAt,
		"start_line":  block.StartLine,
		"end_line":    block.EndLine,
	}
	for k, v := range extra {
		payload[k] = v
	}
	return payload
}
