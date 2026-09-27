// Package staticapp contains the filesystem boundary shared by live app serving and static export.
package staticapp

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const DirName = "apps"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidName accepts one stable URL segment, not a path. App names are deliberately not derived from
// file contents: renaming an app is an author-controlled URL change.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// Open opens one app rooted inside the vault. Root.OpenRoot confines subsequent file operations to
// this app, including when a child path traverses a symlink.
func Open(vaultDir, name string) (*os.Root, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid app name %q", name)
	}
	vault, err := os.OpenRoot(vaultDir)
	if err != nil {
		return nil, err
	}
	defer vault.Close()
	appsInfo, err := vault.Lstat(DirName)
	if err != nil || !appsInfo.IsDir() || appsInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("apps directory is missing or not a real directory")
	}
	apps, err := vault.OpenRoot(DirName)
	if err != nil {
		return nil, err
	}
	defer apps.Close()
	appInfo, err := apps.Lstat(name)
	if err != nil || !appInfo.IsDir() || appInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("app %q is missing or not a real directory", name)
	}
	return apps.OpenRoot(name)
}

// CheckIndex requires a regular index.html at the app root. A directory, device, or symlink escaping
// the app root is not a launchable app.
func CheckIndex(root *os.Root) error {
	f, _, err := OpenFile(root, "index.html")
	if err != nil {
		return fmt.Errorf("app needs a regular index.html: %w", err)
	}
	return f.Close()
}

// OpenFile opens a regular file within an app. It rejects path syntax that can change the root, and
// Root.Stat/Open prevent symlinks from escaping the app even if they are encountered below it.
func OpenFile(root *os.Root, rel string) (*os.File, fs.FileInfo, error) {
	if !validRelativePath(rel) {
		return nil, nil, fmt.Errorf("invalid app file path %q", rel)
	}
	info, err := root.Stat(filepath.FromSlash(rel))
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("app path %q is not a regular file", rel)
	}
	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, nil, err
	}
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = fmt.Errorf("app path %q is not a regular file", rel)
		}
		return nil, nil, err
	}
	return f, info, nil
}

// Copy copies an app's files byte-for-byte, keeping the relative paths and stable app name. In-root
// symlinks to regular files are copied as their target bytes; links escaping the app or pointing at a
// directory/special file fail rather than publishing outside content or an incomplete app.
func Copy(root *os.Root, dst string) error {
	return copyDir(root, ".", dst)
}

func copyDir(root *os.Root, src, dst string) error {
	entries, err := fs.ReadDir(root.FS(), src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		rel := entry.Name()
		if src != "." {
			rel = path.Join(src, rel)
		}
		info, err := root.Lstat(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		out := filepath.Join(dst, entry.Name())
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := root.Stat(filepath.FromSlash(rel))
			if err != nil {
				return fmt.Errorf("resolve app symlink %q: %w", rel, err)
			}
			if !resolved.Mode().IsRegular() {
				return fmt.Errorf("app symlink %q does not point to a regular file", rel)
			}
			if err := copyRegular(root, rel, out); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			if err := copyDir(root, rel, out); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("app path %q is not a regular file or directory", rel)
		}
		if err := copyRegular(root, rel, out); err != nil {
			return err
		}
	}
	return nil
}

func copyRegular(root *os.Root, rel, dst string) error {
	in, _, err := OpenFile(root, rel)
	if err != nil {
		return fmt.Errorf("read app file %q: %w", rel, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func validRelativePath(rel string) bool {
	if rel == "" || strings.ContainsAny(rel, "\\\x00") || strings.HasPrefix(rel, "/") || path.Clean(rel) != rel {
		return false
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// ValidRelativePath reports whether rel is a clean, non-empty app-relative slash path.
func ValidRelativePath(rel string) bool { return validRelativePath(rel) }
