package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const readOnlyCacheNamespace = "canonical-read-only-v1"
const readOnlyCacheEntryLimit = 4096

// ReadOnlyCachePath validates a dedicated cache location without creating or
// changing anything. The caller must supply a pre-existing, private, absolute
// root, the unmodified loaded config (including every known peer), and all
// protected configuration file paths. It returns an isolated derived DB path;
// it does not change cfg or enable a server mode.
//
// Vault roots must resolve to existing directories. A configuration file may be
// absent only when its parent resolves. Cache ancestors and existing namespace
// entries must not be symlinks; files must have one link. The namespace must be
// disjoint from all vault roots, configuration files and normal index paths.
// The root must deny all group/other permissions; namespace entries must be
// owned and not group/other-writable (SQLite/PID 0644 files are allowed inside
// the private root). Only Linux/macOS file identity checks are currently supported.
// Callers on macOS must resolve system aliases such as /tmp before choosing root.
//
// This is a point-in-time check, not a filesystem sandbox or a write capability.
// Callers must revalidate before opening/creating cache and runtime files and on
// every config/peer replacement. Concurrent hostile path replacement, mount
// aliases and writes by other processes are not prevented by this helper.
func ReadOnlyCachePath(root string, cfg *Config, configurationFiles []string) (string, error) {
	if !cacheIdentitySupported {
		return "", errors.New("read-only cache file identity validation is unsupported on this platform")
	}
	if cfg == nil || cfg.VaultDir == "" || cfg.DBPath == "" || len(configurationFiles) == 0 {
		return "", errors.New("read-only cache: active vault, normal DB and configuration paths are required")
	}
	if err := absoluteClean(root); err != nil {
		return "", err
	}
	if err := cacheAncestors(root); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("read-only cache root: %w", err)
	}
	if !info.IsDir() || !cacheOwned(info) || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("read-only cache root must be a private directory owned by the current user")
	}
	namespace := filepath.Join(root, readOnlyCacheNamespace)
	vaults := []string{cfg.VaultDir}
	for _, peer := range cfg.Vaults {
		vaults = append(vaults, peer)
	}
	var active string
	for i, path := range vaults {
		canonical, err := canonicalExistingDirectory(path)
		if err != nil {
			return "", fmt.Errorf("read-only cache protected vault %q: %w", path, err)
		}
		if overlaps(namespace, canonical) {
			return "", fmt.Errorf("read-only cache namespace overlaps vault %q", path)
		}
		if i == 0 {
			active = canonical
		}
	}
	for _, path := range configurationFiles {
		canonical, err := protectedFile(path)
		if err != nil {
			return "", fmt.Errorf("read-only cache protected config %q: %w", path, err)
		}
		if overlaps(namespace, canonical) {
			return "", fmt.Errorf("read-only cache namespace overlaps configuration %q", path)
		}
	}
	// Config derives every peer DB from this same cache root and its vault key.
	// Resolve existing ancestors even when the ordinary cache does not exist.
	if err := absoluteClean(cfg.DBPath); err != nil {
		return "", fmt.Errorf("read-only cache normal DB: %w", err)
	}
	normalRoot := filepath.Dir(filepath.Dir(cfg.DBPath))
	for _, vault := range vaults {
		canonical, err := canonicalExistingDirectory(vault)
		if err != nil {
			return "", err
		}
		path := filepath.Join(normalRoot, vaultCacheKey(canonical), indexDBName())
		resolved, err := resolveCacheAlias(path)
		if err != nil {
			return "", fmt.Errorf("read-only cache normal index: %w", err)
		}
		if overlaps(namespace, resolved) {
			return "", errors.New("read-only cache namespace overlaps a normal index path")
		}
	}
	normalDB, err := resolveCacheAlias(cfg.DBPath)
	if err != nil {
		return "", err
	}
	if overlaps(namespace, normalDB) {
		return "", errors.New("read-only cache namespace overlaps the active normal index")
	}
	if err := validateCacheTree(namespace); err != nil {
		return "", err
	}
	return filepath.Join(namespace, vaultCacheKey(active), indexDBName()), nil
}

func absoluteClean(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("read-only cache requires a clean absolute path: %q", path)
	}
	return nil
}

// Check every existing ancestor, not only the final root. A missing root is an
// error: choosing/creating an approved private parent is the caller's job.
func cacheAncestors(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("read-only cache ancestor %q: %w", path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("read-only cache ancestor must be a non-symlink directory: %q", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func canonicalExistingDirectory(path string) (string, error) {
	if err := absoluteClean(path); err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return canonical, nil
}

func protectedFile(path string) (string, error) {
	if err := absoluteClean(path); err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path) // Broken symlinks fail, not an absent config.
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent, err := canonicalExistingDirectory(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func overlaps(a, b string) bool { return containsPath(a, b) || containsPath(b, a) }
func containsPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func validateCacheTree(namespace string) error {
	if _, err := os.Lstat(namespace); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read-only cache namespace: %w", err)
	}
	info, err := os.Lstat(namespace)
	if err != nil || !info.IsDir() {
		return errors.New("read-only cache namespace must be a directory")
	}
	count := 0
	return filepath.WalkDir(namespace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("read-only cache entry %q: %w", path, walkErr)
		}
		count++
		if count > readOnlyCacheEntryLimit {
			return errors.New("read-only cache validation entry limit exceeded")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !cacheOwned(info) || info.Mode().Perm()&0022 != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("read-only cache entry must be owned, not shared-writable and a regular file/directory: %q", path)
		}
		if !info.IsDir() {
			if !cacheSingleLink(info) {
				return fmt.Errorf("read-only cache file must have exactly one link: %q", path)
			}
		}
		return nil
	})
}

// Resolve an ordinary cache's existing ancestors without creating missing ones.
// Broken links or inaccessible ancestors cannot establish separation.
func resolveCacheAlias(path string) (string, error) {
	suffix := []string{}
	for {
		if _, err := os.Lstat(path); err == nil {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", errors.New("read-only cache cannot resolve normal cache ancestor")
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}
