package babel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TangleSource is one input document. Path identifies the source and must be canonical.
type TangleSource struct {
	Path   string
	Blocks []Block
}

// TangleLocation identifies a source block: Line is 1-based, Block is its 0-based ordinal.
type TangleLocation struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Block int    `json:"block"`
	Name  string `json:"name,omitempty"`
}

// TangleTarget is one output file, with the winning block and any earlier overwritten blocks.
type TangleTarget struct {
	Path       string // resolved output path
	Content    string // winning (noweb-expanded) body, ending in a newline
	Blocks     int    // number of blocks targeting this output, including overwritten blocks
	Source     TangleLocation
	Overridden []TangleLocation
}

// TanglePlan plans one document, normalizing target paths without accessing the filesystem.
func TanglePlan(blocks []Block) ([]TangleTarget, error) {
	return PlanTangle([]TangleSource{{Blocks: blocks}}, func(_, target string) (string, error) {
		return filepath.Clean(target), nil
	})
}

// PlanTangle resolves and validates every output before returning a write plan in first-seen order.
// The last block wins within a source; collisions across sources and file/descendant conflicts fail.
// Blocks without :tangle (or with :tangle no) are skipped. Noweb expands source text only.
func PlanTangle(sources []TangleSource, resolve func(source, target string) (string, error)) ([]TangleTarget, error) {
	targets := make([]TangleTarget, 0)
	byPath := make(map[string]int)
	for _, source := range sources {
		if err := Validate(source.Blocks); err != nil {
			return nil, fmt.Errorf("%s: %w", source.Path, err)
		}
		for _, b := range source.Blocks {
			target := firstValue(b.HeaderArgs, "tangle")
			location := TangleLocation{Path: source.Path, Line: b.StartLine + 1, Block: b.Ordinal, Name: b.Name}
			switch target {
			case "", "no":
				continue
			case "yes":
				return nil, fmt.Errorf("%s: :tangle yes needs an explicit file name", tangleLocationLabel(location))
			}
			path, err := resolve(source.Path, target)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", tangleLocationLabel(location), err)
			}
			index, exists := byPath[path]
			if exists && targets[index].Source.Path != source.Path {
				return nil, fmt.Errorf("duplicate tangle target %q: %s and %s", path,
					tangleLocationLabel(targets[index].Source), tangleLocationLabel(location))
			}
			body := b.Body
			if NowebExpands(b, "tangle") {
				body, err = ExpandNoweb(body, source.Blocks)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", tangleLocationLabel(location), err)
				}
			}
			if !exists {
				index = len(targets)
				byPath[path] = index
				targets = append(targets, TangleTarget{Path: path})
			} else {
				targets[index].Overridden = append(targets[index].Overridden, targets[index].Source)
			}
			targets[index].Content = strings.TrimRight(body, "\n") + "\n"
			targets[index].Blocks++
			targets[index].Source = location
		}
	}
	for _, target := range targets {
		for parent := filepath.Dir(target.Path); ; parent = filepath.Dir(parent) {
			if index, exists := byPath[parent]; exists {
				return nil, fmt.Errorf("tangle target %q (%s) is a parent of %q (%s)", parent,
					tangleLocationLabel(targets[index].Source), target.Path, tangleLocationLabel(target.Source))
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
	}
	return targets, nil
}

func tangleLocationLabel(location TangleLocation) string {
	return fmt.Sprintf("%s:%d (block %s)", location.Path, location.Line,
		blockLabel(Block{Name: location.Name, Ordinal: location.Block}))
}

// ResolveTangleOutputPath confines an output to the caller's root, independent of a vault.
// Existing vaults remain protected if the caller deliberately selects a root inside one.
func ResolveTangleOutputPath(root, target string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	within := func(base, path string) bool {
		rel, err := filepath.Rel(base, path)
		return err == nil && rel != "." && filepath.IsLocal(rel)
	}
	if !within(root, candidate) {
		return "", fmt.Errorf(":tangle %q resolves outside the output directory", target)
	}
	resolvedRoot, err := resolveOutputPath(root)
	if err != nil {
		return "", err
	}
	resolved, err := resolveOutputPath(candidate)
	if err != nil {
		return "", err
	}
	if !within(resolvedRoot, resolved) {
		return "", fmt.Errorf(":tangle %q resolves outside the output directory", target)
	}
	if info, err := os.Stat(resolved); err == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf(":tangle %q is not a regular file", target)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	// Check both spellings: a managed directory can itself point outside its vault.
	for _, path := range []string{candidate, resolved} {
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			if info, err := os.Stat(filepath.Join(dir, ".track")); err == nil && info.IsDir() {
				if _, err := ResolveTanglePath(filepath.Dir(path), dir, path); err != nil {
					return "", err
				}
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return resolved, nil
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
