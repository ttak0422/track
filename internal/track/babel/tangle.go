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

// ResolveTanglePath returns a canonical output path. Both the written path and its resolved
// destination must stay in the vault and outside track's metadata and directly managed files.
func ResolveTanglePath(noteDir, vaultDir, target string) (string, error) {
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(noteDir, candidate)
	}
	candidate = filepath.Clean(candidate)
	vaultDir = filepath.Clean(vaultDir)
	if err := validateTanglePath(candidate, vaultDir, target); err != nil {
		return "", err
	}
	resolved, err := resolveOutputPath(candidate)
	if err != nil {
		return "", fmt.Errorf(":tangle %q: %w", target, err)
	}
	resolvedVault, err := resolveOutputPath(vaultDir)
	if err != nil {
		return "", fmt.Errorf(":tangle %q: %w", target, err)
	}
	if err := validateTanglePath(resolved, resolvedVault, target); err != nil {
		return "", err
	}
	// Managed roots can themselves be symlinks, so protect their destinations too.
	for _, name := range []string{".track", "note", "journal", "template"} {
		root, err := resolveOutputPath(filepath.Join(vaultDir, name))
		if err != nil {
			return "", fmt.Errorf(":tangle %q: %w", target, err)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil {
			return "", fmt.Errorf(":tangle %q: %w", target, err)
		}
		if filepath.IsLocal(rel) && (name == ".track" || filepath.Dir(rel) == ".") {
			return "", fmt.Errorf(":tangle %q overwrites a path track manages in %s/", target, name)
		}
	}
	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return "", fmt.Errorf(":tangle %q is a directory", target)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf(":tangle %q: %w", target, err)
	}
	return resolved, nil
}

func validateTanglePath(candidate, vaultDir, target string) error {
	rel, err := filepath.Rel(vaultDir, candidate)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return fmt.Errorf(":tangle %q resolves outside the vault", target)
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	switch segments[0] {
	case ".track":
		return fmt.Errorf(":tangle %q writes into the vault's .track/ directory", target)
	case "note", "journal", "template":
		if len(segments) <= 2 {
			return fmt.Errorf(":tangle %q overwrites a path track manages in %s/", target, segments[0])
		}
	}
	return nil
}

// resolveOutputPath resolves the deepest existing ancestor and appends the missing suffix.
// Lstat distinguishes a missing output from a dangling symlink, which must fail closed.
func resolveOutputPath(candidate string) (string, error) {
	ancestor, suffix := candidate, ""
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			if suffix != "" {
				info, err := os.Stat(resolved)
				if err != nil {
					return "", err
				}
				if !info.IsDir() {
					return "", fmt.Errorf("%s is not a directory", ancestor)
				}
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(ancestor), suffix)
		ancestor = filepath.Dir(ancestor)
	}
}

func blockLabel(b Block) string {
	if b.Name != "" {
		return fmt.Sprintf("%q", b.Name)
	}
	return fmt.Sprintf("#%d", b.Ordinal)
}
