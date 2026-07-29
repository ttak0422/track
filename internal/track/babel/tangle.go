package babel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tangling (docs/spec/babel.md) writes source blocks out to files: every block carrying
// ":tangle <path>" contributes to that file, and blocks naming the same path concatenate in note
// order, separated by one blank line. This file is the pure planning layer; the CLI resolves paths
// against the note and vault and performs the writes.

// TangleTarget is one output file of a tangle plan.
type TangleTarget struct {
	Path    string // the :tangle value as written in the note
	Content string // concatenated (noweb-expanded) block bodies, ending in a newline
	Blocks  int    // number of contributing blocks
}

// TanglePlan assembles the tangle targets of a note's blocks, in first-seen order.
// Blocks without :tangle (or with :tangle no) are skipped. ":tangle yes" is rejected: track has no
// derived output naming, so a tangled block must name its file. <<name>> references expand per each
// block's :noweb policy before concatenation.
func TanglePlan(blocks []Block) ([]TangleTarget, error) {
	byName := make(map[string]Block)
	for _, b := range blocks {
		if b.Name != "" {
			byName[b.Name] = b
		}
	}

	var order []string
	bodies := make(map[string][]string)
	for _, b := range blocks {
		target := firstValue(b.HeaderArgs, "tangle")
		switch target {
		case "", "no":
			continue
		case "yes":
			return nil, fmt.Errorf("block %s: :tangle yes needs an explicit file name", blockLabel(b))
		}
		body := b.Body
		if NowebExpands(b, "tangle") {
			var err error
			body, err = expandNoweb(b.Body, byName, nil)
			if err != nil {
				return nil, fmt.Errorf("block %s: %w", blockLabel(b), err)
			}
		}
		if _, ok := bodies[target]; !ok {
			order = append(order, target)
		}
		bodies[target] = append(bodies[target], strings.TrimRight(body, "\n"))
	}

	targets := make([]TangleTarget, 0, len(order))
	for _, p := range order {
		targets = append(targets, TangleTarget{
			Path:    p,
			Content: strings.Join(bodies[p], "\n\n") + "\n",
			Blocks:  len(bodies[p]),
		})
	}
	return targets, nil
}

// ResolveTanglePath resolves a :tangle target against the note's directory and refuses any path that
// lands outside the vault, so a note can never tangle over files beyond the working tree it lives in.
// Inside the vault, track's own files are off limits too: anything under .track/ (index, config,
// sidecars), and the files sitting directly in note/, journal/, and template/ — those directories are
// flat, so a direct child is a note track manages, while a subdirectory (note/scripts/…) is user
// territory. A symlinked ancestor could point back out of the vault, so before anything writes the
// deepest existing ancestor is resolved and containment re-checked against the resolved vault root.
func ResolveTanglePath(noteDir, vaultDir, target string) (string, error) {
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(noteDir, candidate)
	}
	candidate = filepath.Clean(candidate)
	vaultClean := filepath.Clean(vaultDir)
	if !strings.HasPrefix(candidate, vaultClean+string(filepath.Separator)) {
		return "", fmt.Errorf(":tangle %q resolves outside the vault", target)
	}
	rel, err := filepath.Rel(vaultClean, candidate)
	if err != nil {
		return "", fmt.Errorf(":tangle %q: %v", target, err)
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	switch segments[0] {
	case ".track":
		return "", fmt.Errorf(":tangle %q writes into the vault's .track/ directory", target)
	case "note", "journal", "template": // config.KindNote/KindJournal/KindTemplate; config imports babel, so no import back

		if len(segments) == 2 {
			return "", fmt.Errorf(":tangle %q overwrites a file track manages in %s/", target, segments[0])
		}
	}
	if err := verifyResolvedInsideVault(candidate, vaultClean, target); err != nil {
		return "", err
	}
	return candidate, nil
}

// verifyResolvedInsideVault resolves the deepest existing ancestor of candidate (or candidate itself,
// catching a symlinked target file) and requires it to still sit inside the resolved vault root. A
// vault that does not exist on disk has nothing to escape from, so it is left to the write to fail.
func verifyResolvedInsideVault(candidate, vaultDir, target string) error {
	resolvedVault, err := filepath.EvalSymlinks(vaultDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(":tangle %q: %v", target, err)
	}
	dir := candidate
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			if resolved != resolvedVault && !strings.HasPrefix(resolved, resolvedVault+string(filepath.Separator)) {
				return fmt.Errorf(":tangle %q escapes the vault through a symlink", target)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf(":tangle %q: %v", target, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func blockLabel(b Block) string {
	if b.Name != "" {
		return fmt.Sprintf("%q", b.Name)
	}
	return fmt.Sprintf("#%d", b.Ordinal)
}
