// Package gen manages vault generations: full-copy snapshots of note bodies and their sidecar
// metadata, with a cursor for undo/redo. The model is a git release: a generation is an immutable
// save point, the working vault is a disposable working tree, increment cuts a release, and
// undo/redo check one out. Unsaved working changes are only auto-saved by undo at the head; every
// other cursor move discards them, which is the expected release-checkout behavior.
//
// Snapshots are complete file copies of a fixed set of vault-relative roots (note/, journal/, and
// the .track sidecars). assets/ and data/ are deliberately excluded so binary bulk never multiplies
// into cloud storage, and the generation store itself is never snapshotted or indexed. Retention is
// count-based: increment prunes the oldest generations beyond Config.GenKeep.
package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"gopkg.in/yaml.v3"
)

type Manager struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Manager { return &Manager{cfg: cfg} }

// snapshotDirs are the vault-relative directories captured in every generation.
func snapshotDirs() []string {
	return []string{config.KindNote, config.KindJournal, filepath.Join(".track", "notes")}
}

// snapshotFiles are the vault-relative single files captured in every generation.
func snapshotFiles() []string {
	return []string{filepath.Join(".track", "renames.yaml")}
}

type state struct {
	Cursor int `yaml:"cursor"`
}

type genMeta struct {
	Created string `yaml:"created"`
	Label   string `yaml:"label,omitempty"`
}

// Info describes one generation for listing.
type Info struct {
	Gen     int    `json:"gen"`
	Created string `json:"created,omitempty"`
	Label   string `json:"label,omitempty"`
	Notes   int    `json:"notes"`
}

type ListResult struct {
	Generations []Info `json:"generations"`
	Cursor      int    `json:"cursor"`
	Dirty       bool   `json:"dirty"`
}

// StatusResult lists how the working vault diverged from the cursor generation, git-status style.
// Paths are vault-relative (forward-slash) snapshot paths. With no generations yet, Cursor is 0 and
// every working file is reported under Added.
type StatusResult struct {
	Cursor  int      `json:"cursor"`
	Dirty   bool     `json:"dirty"`
	Added   []string `json:"added"`
	Changed []string `json:"changed"`
	Deleted []string `json:"deleted"`
}

type IncrementResult struct {
	Gen     int   `json:"gen"`
	Changed bool  `json:"changed"`
	Dropped []int `json:"dropped,omitempty"`
	Pruned  []int `json:"pruned,omitempty"`
}

// MoveResult reports an undo/redo. Saved is the generation an undo at a dirty head auto-created
// before stepping back (0 when none); redo back onto it revisits the discarded working state.
type MoveResult struct {
	Gen   int `json:"gen"`
	Saved int `json:"saved,omitempty"`
}

func (m *Manager) genDir(n int) string { return filepath.Join(m.cfg.GenDir(), strconv.Itoa(n)) }
func (m *Manager) statePath() string   { return filepath.Join(m.cfg.GenDir(), "state.yaml") }

// generations lists existing generation numbers in ascending order.
func (m *Manager) generations() ([]int, error) {
	entries, err := os.ReadDir(m.cfg.GenDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var gens []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(e.Name()); err == nil && n > 0 {
			gens = append(gens, n)
		}
	}
	slices.Sort(gens)
	return gens, nil
}

// cursor reads the persisted cursor, normalized against the generations that actually exist: a
// missing state file or a cursor pointing at a pruned/synced-away generation falls back to the head.
func (m *Manager) cursor(gens []int) (int, error) {
	if len(gens) == 0 {
		return 0, nil
	}
	raw, err := os.ReadFile(m.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return gens[len(gens)-1], nil
		}
		return 0, err
	}
	var st state
	if err := yaml.Unmarshal(raw, &st); err != nil {
		return 0, fmt.Errorf("parse %s: %w", m.statePath(), err)
	}
	if slices.Contains(gens, st.Cursor) {
		return st.Cursor, nil
	}
	return gens[len(gens)-1], nil
}

func (m *Manager) saveCursor(n int) error {
	if err := os.MkdirAll(m.cfg.GenDir(), 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(state{Cursor: n})
	if err != nil {
		return err
	}
	return os.WriteFile(m.statePath(), raw, 0o644)
}

// Increment saves the working vault as a new generation. Generations after the cursor are dropped
// first (linear history), and when the working vault already equals the cursor generation no new
// one is created. Old generations beyond Config.GenKeep are pruned. label is stored in the
// generation's metadata so dream save points can be told apart from manual ones; it is dropped when
// no new generation is cut (nothing changed).
func (m *Manager) Increment(label string) (IncrementResult, error) {
	var res IncrementResult
	gens, err := m.generations()
	if err != nil {
		return res, err
	}
	cur, err := m.cursor(gens)
	if err != nil {
		return res, err
	}

	for _, g := range gens {
		if g > cur {
			if err := os.RemoveAll(m.genDir(g)); err != nil {
				return res, err
			}
			res.Dropped = append(res.Dropped, g)
		}
	}
	gens = gens[:len(gens)-len(res.Dropped)]

	if cur > 0 {
		dirty, err := m.dirty(cur)
		if err != nil {
			return res, err
		}
		if !dirty {
			res.Gen = cur
			return res, nil
		}
	}

	next := cur + 1
	if err := m.snapshot(next, label); err != nil {
		return res, err
	}
	if err := m.saveCursor(next); err != nil {
		return res, err
	}
	res.Gen = next
	res.Changed = true
	gens = append(gens, next)

	keep := max(m.cfg.GenKeep, 1)
	for len(gens) > keep {
		if err := os.RemoveAll(m.genDir(gens[0])); err != nil {
			return res, err
		}
		res.Pruned = append(res.Pruned, gens[0])
		gens = gens[1:]
	}
	return res, nil
}

// Undo moves the cursor back one generation and restores it. At the head with unsaved changes it
// first auto-saves them as a new generation, so a later Redo can revisit them; anywhere else,
// unsaved changes are discarded like a release checkout.
func (m *Manager) Undo() (MoveResult, error) {
	var res MoveResult
	gens, err := m.generations()
	if err != nil {
		return res, err
	}
	if len(gens) == 0 {
		return res, fmt.Errorf("no generations; run `track gen increment` first")
	}
	cur, err := m.cursor(gens)
	if err != nil {
		return res, err
	}
	head := gens[len(gens)-1]

	target := 0
	if cur == head {
		dirty, err := m.dirty(cur)
		if err != nil {
			return res, err
		}
		if dirty {
			// Auto-save the working state as a new head so undo never destroys it; the step back
			// then lands on the generation the cursor was on.
			res.Saved = head + 1
			if err := m.snapshot(res.Saved, ""); err != nil {
				return res, err
			}
			target = cur
		}
	}
	if target == 0 {
		i := slices.Index(gens, cur)
		if i == 0 {
			return res, fmt.Errorf("no older generation")
		}
		target = gens[i-1]
	}

	if err := m.restore(target); err != nil {
		return res, err
	}
	if err := m.saveCursor(target); err != nil {
		return res, err
	}
	res.Gen = target
	return res, nil
}

// Redo moves the cursor forward one generation and restores it, discarding unsaved working changes.
func (m *Manager) Redo() (MoveResult, error) {
	var res MoveResult
	gens, err := m.generations()
	if err != nil {
		return res, err
	}
	if len(gens) == 0 {
		return res, fmt.Errorf("no generations; run `track gen increment` first")
	}
	cur, err := m.cursor(gens)
	if err != nil {
		return res, err
	}
	i := slices.Index(gens, cur)
	if i == len(gens)-1 {
		return res, fmt.Errorf("no newer generation")
	}
	target := gens[i+1]
	if err := m.restore(target); err != nil {
		return res, err
	}
	if err := m.saveCursor(target); err != nil {
		return res, err
	}
	res.Gen = target
	return res, nil
}

// List reports every generation, the cursor, and whether the working vault diverged from it.
func (m *Manager) List() (ListResult, error) {
	res := ListResult{Generations: []Info{}}
	gens, err := m.generations()
	if err != nil {
		return res, err
	}
	cur, err := m.cursor(gens)
	if err != nil {
		return res, err
	}
	res.Cursor = cur
	for _, g := range gens {
		info := Info{Gen: g}
		if raw, err := os.ReadFile(filepath.Join(m.genDir(g), "gen.yaml")); err == nil {
			var meta genMeta
			if yaml.Unmarshal(raw, &meta) == nil {
				info.Created = meta.Created
				info.Label = meta.Label
			}
		}
		for _, d := range []string{config.KindNote, config.KindJournal} {
			entries, err := os.ReadDir(filepath.Join(m.genDir(g), d))
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					info.Notes++
				}
			}
		}
		res.Generations = append(res.Generations, info)
	}
	if cur > 0 {
		dirty, err := m.dirty(cur)
		if err != nil {
			return res, err
		}
		res.Dirty = dirty
	} else {
		// Nothing saved yet: any existing content counts as unsaved work.
		sums, err := treeSums(m.cfg.VaultDir)
		if err != nil {
			return res, err
		}
		res.Dirty = len(sums) > 0
	}
	return res, nil
}

// Status reports which snapshot files the working vault added, changed, or deleted relative to the
// cursor generation — the machine-readable basis for a dream report, so the changed set no longer
// depends on the agent's self-report. It is the file-level detail behind List's Dirty bool.
func (m *Manager) Status() (StatusResult, error) {
	res := StatusResult{Added: []string{}, Changed: []string{}, Deleted: []string{}}
	gens, err := m.generations()
	if err != nil {
		return res, err
	}
	cur, err := m.cursor(gens)
	if err != nil {
		return res, err
	}
	res.Cursor = cur

	work, err := treeSums(m.cfg.VaultDir)
	if err != nil {
		return res, err
	}
	var saved map[string]string
	if cur > 0 {
		if saved, err = treeSums(m.genDir(cur)); err != nil {
			return res, err
		}
	} else {
		saved = map[string]string{}
	}

	for rel, sum := range work {
		switch prev, ok := saved[rel]; {
		case !ok:
			res.Added = append(res.Added, rel)
		case prev != sum:
			res.Changed = append(res.Changed, rel)
		}
	}
	for rel := range saved {
		if _, ok := work[rel]; !ok {
			res.Deleted = append(res.Deleted, rel)
		}
	}
	slices.Sort(res.Added)
	slices.Sort(res.Changed)
	slices.Sort(res.Deleted)
	res.Dirty = len(res.Added)+len(res.Changed)+len(res.Deleted) > 0
	return res, nil
}

// Peek returns a note's content as of generation n (0 means the cursor generation). rel is the
// note's vault-relative path. The cursor does not move.
func (m *Manager) Peek(n int, rel string) (string, error) {
	gens, err := m.generations()
	if err != nil {
		return "", err
	}
	if n == 0 {
		if n, err = m.cursor(gens); err != nil {
			return "", err
		}
		if n == 0 {
			return "", fmt.Errorf("no generations; run `track gen increment` first")
		}
	}
	if !slices.Contains(gens, n) {
		return "", fmt.Errorf("no generation %d", n)
	}
	raw, err := os.ReadFile(filepath.Join(m.genDir(n), rel))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("note %s not in generation %d", rel, n)
		}
		return "", err
	}
	return string(raw), nil
}

// snapshot copies the snapshot roots of the working vault into a new generation directory. label is
// recorded in the generation metadata (empty for auto-saves and unlabeled increments).
func (m *Manager) snapshot(n int, label string) error {
	dst := m.genDir(n)
	for _, d := range snapshotDirs() {
		if err := copyTree(filepath.Join(m.cfg.VaultDir, d), filepath.Join(dst, d)); err != nil {
			return err
		}
	}
	for _, f := range snapshotFiles() {
		if err := copyFileIfExists(filepath.Join(m.cfg.VaultDir, f), filepath.Join(dst, f)); err != nil {
			return err
		}
	}
	raw, err := yaml.Marshal(genMeta{Created: time.Now().Format(time.RFC3339), Label: label})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "gen.yaml"), raw, 0o644)
}

// restore replaces the snapshot roots of the working vault with generation n's copies, so files
// created after the snapshot disappear and deleted ones come back. The SQLite index is not touched
// here; callers rebuild it (or RefreshIfStale self-heals on the next read).
func (m *Manager) restore(n int) error {
	src := m.genDir(n)
	for _, d := range snapshotDirs() {
		vaultDir := filepath.Join(m.cfg.VaultDir, d)
		if err := os.RemoveAll(vaultDir); err != nil {
			return err
		}
		if err := copyTree(filepath.Join(src, d), vaultDir); err != nil {
			return err
		}
	}
	for _, f := range snapshotFiles() {
		vaultFile := filepath.Join(m.cfg.VaultDir, f)
		if err := os.Remove(vaultFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := copyFileIfExists(filepath.Join(src, f), vaultFile); err != nil {
			return err
		}
	}
	return nil
}

// dirty reports whether the working vault's snapshot roots differ from generation n's.
func (m *Manager) dirty(n int) (bool, error) {
	work, err := treeSums(m.cfg.VaultDir)
	if err != nil {
		return false, err
	}
	saved, err := treeSums(m.genDir(n))
	if err != nil {
		return false, err
	}
	if len(work) != len(saved) {
		return true, nil
	}
	for rel, sum := range work {
		if saved[rel] != sum {
			return true, nil
		}
	}
	return false, nil
}

// treeSums maps vault-relative snapshot paths to content hashes under base (a vault or a generation
// directory). Missing roots simply contribute nothing, so an empty vault and an empty snapshot compare equal.
func treeSums(base string) (map[string]string, error) {
	out := map[string]string{}
	for _, d := range snapshotDirs() {
		root := filepath.Join(base, d)
		err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && p == root {
					return nil
				}
				return err
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			sum, err := fileSum(p)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = sum
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, f := range snapshotFiles() {
		p := filepath.Join(base, f)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		sum, err := fileSum(p)
		if err != nil {
			return nil, err
		}
		out[filepath.ToSlash(f)] = sum
	}
	return out, nil
}

func fileSum(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// copyTree copies every regular file under src into dst, creating dst even when src is missing or
// empty so a restore of an empty root still yields the directory the vault layout expects.
func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == src {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}

func copyFileIfExists(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}
