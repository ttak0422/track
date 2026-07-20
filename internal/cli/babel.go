package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	yes := fs.Bool("yes", false, "confirm execution for blocks with :eval query")
	timeout := fs.Duration("timeout", 30*time.Second, "max run time per block (0 = no limit)")
	var cliVars varsFlag
	fs.Var(&cliVars, "var", "k=v passed to the block's environment (repeatable); overrides the block's :var")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
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

	// The runnable copy may differ from the parsed block (noweb expansion); identity, stored header
	// args, and the body hash always come from the block as written, so restore keeps matching the file.
	runBlock := block
	if babel.NowebExpands(block, "eval") {
		expanded, err := babel.ExpandNoweb(block.Body, blocks)
		if err != nil {
			return fail("%v", err)
		}
		runBlock.Body = expanded
	}

	vars, err := resolveVars(block, cliVars, blocks, n)
	if err != nil {
		return fail("%v", err)
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

	return emit(blockRunPayload(blockID, block, res, map[string]any{
		"stored": stored,
	}))
}

func cmdBabelRestore(args []string) int {
	fs := flag.NewFlagSet("babel restore", flag.ContinueOnError)
	path := fs.String("path", "", "note path")
	id := fs.Int64("id", 0, "note id (alternative to --path)")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
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

	blocks := babel.ParseBlocks(n.Body)
	if err := babel.Validate(blocks); err != nil {
		return fail("%v", err)
	}

	restored := []map[string]any{}
	for _, block := range blocks {
		blockID := block.ID(n.ID)
		meta, ok := n.Meta.Blocks[blockID]
		if !ok || meta.LastRun == nil {
			continue
		}
		if meta.Language != block.Language || meta.BodyHash != block.BodyHash {
			continue
		}
		restored = append(restored, blockRunPayload(blockID, block, *meta.LastRun, map[string]any{
			"stored":   true,
			"restored": true,
		}))
	}

	return emit(map[string]any{"blocks": restored})
}

func cmdBabelTangle(args []string) int {
	fs := flag.NewFlagSet("babel tangle", flag.ContinueOnError)
	path := fs.String("path", "", "note path")
	id := fs.Int64("id", 0, "note id (alternative to --path)")
	dryRun := fs.Bool("dry-run", false, "print the tangle plan without writing files")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
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

	blocks := babel.ParseBlocks(n.Body)
	if err := babel.Validate(blocks); err != nil {
		return fail("%v", err)
	}

	plan, err := babel.TanglePlan(blocks)
	if err != nil {
		return fail("%v", err)
	}

	noteDir := filepath.Dir(n.Path)
	targets := make([]map[string]any, 0, len(plan))
	for _, t := range plan {
		abs, err := babel.ResolveTanglePath(noteDir, cfg.VaultDir, t.Path)
		if err != nil {
			return fail("%v", err)
		}
		if !*dryRun {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return fail("tangle %s: %v", t.Path, err)
			}
			if err := os.WriteFile(abs, []byte(t.Content), 0o644); err != nil {
				return fail("tangle %s: %v", t.Path, err)
			}
		}
		targets = append(targets, map[string]any{
			"path":   abs,
			"blocks": t.Blocks,
			"bytes":  len(t.Content),
		})
	}

	return emit(map[string]any{"targets": targets, "dry_run": *dryRun})
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
	n, err := note.ParseFile(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("read note: %v", err)
	}
	return n, nil
}

// envName is what a variable key must look like, since variables reach the block as environment entries.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// resolveVars merges a block's :var headers with CLI --var overrides into the environment map the
// runner injects (CLI wins on the same key). A value that names another named block resolves to that
// block's stored result; any other value is a literal, with one pair of surrounding double quotes
// stripped so ':var greeting="hello world"' keeps its spaces without inventing a quoting language.
func resolveVars(block babel.Block, cliVars []string, blocks []babel.Block, n *note.Note) (map[string]string, error) {
	specs := append(append([]string{}, block.HeaderArgs["var"]...), cliVars...)
	if len(specs) == 0 {
		return nil, nil
	}
	named := make(map[string]bool)
	for _, b := range blocks {
		if b.Name != "" {
			named[b.Name] = true
		}
	}
	vars := make(map[string]string, len(specs))
	for _, spec := range specs {
		k, v, ok := strings.Cut(spec, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("var %q: want key=value", spec)
		}
		if !envName.MatchString(k) {
			return nil, fmt.Errorf("var %q: key must be a valid environment variable name", spec)
		}
		if named[v] && v != block.Name {
			meta, ok := n.Meta.Blocks[v]
			if !ok || meta.LastRun == nil {
				return nil, fmt.Errorf("var %s references block %q, which has no stored result; run 'track babel exec --name %s' first", k, v, v)
			}
			vars[k] = storedResultText(meta.LastRun)
			continue
		}
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		vars[k] = v
	}
	return vars, nil
}

// storedResultText is the text a stored run feeds into a variable: the captured value when the run
// has one, otherwise stdout, without the trailing newline.
func storedResultText(r *babel.RunResult) string {
	if r.Value != "" {
		return strings.TrimRight(r.Value, "\n")
	}
	return strings.TrimRight(r.Stdout, "\n")
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
