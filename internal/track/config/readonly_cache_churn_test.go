//go:build linux || darwin

package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type failedCacheEntry struct {
	fs.DirEntry
	err error
}

func (e failedCacheEntry) Info() (fs.FileInfo, error) { return nil, e.err }

func TestReadOnlyCacheDisappearingDescendant(t *testing.T) {
	f := newCacheFixture(t)
	namespace := filepath.Join(f.root, readOnlyCacheNamespace)
	if err := os.Mkdir(namespace, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(namespace, "index-v11.db-shm")
	if err := os.WriteFile(file, []byte("SQLite transient fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(namespace)
	if err != nil {
		t.Fatal(err)
	}
	entry := entries[0]
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	// This is the exact live-tree race: enumeration succeeded, then Lstat fails.
	if _, err := entry.Info(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected vanished entry, got %v", err)
	}
	if err := validateCacheEntry(namespace, file, entry, nil); err != nil {
		t.Fatalf("vanished SQLite child: %v", err)
	}
	gone := &os.PathError{Op: "readdir", Path: file, Err: os.ErrNotExist}
	if err := validateCacheEntry(namespace, file, nil, gone); err != nil {
		t.Fatalf("vanished walked child: %v", err)
	}
	if err := validateCacheEntry(namespace, namespace, nil, gone); err == nil {
		t.Fatal("namespace disappearance ignored during walk")
	}
	if err := validateCacheEntry(namespace, filepath.Dir(namespace), nil, gone); err == nil {
		t.Fatal("outside disappearance ignored")
	}
	denied := &os.PathError{Op: "lstat", Path: file, Err: os.ErrPermission}
	if err := validateCacheEntry(namespace, file, failedCacheEntry{entry, denied}, nil); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Info permission error lost: %v", err)
	}
	if err := validateCacheEntry(namespace, file, nil, denied); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("walk permission error lost: %v", err)
	}
	if _, err := f.path(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if _, err := f.path(); err == nil {
		t.Fatal("required missing private root accepted")
	}
}

func TestReadOnlyCacheDisappearingSymlinkIsStillRejected(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := validateCacheEntry(root, link, entries[0], nil); err == nil {
		t.Fatal("known symlink disappearance erased rejection")
	}
}

// staleCacheEntry models a successful stat that raced with unlink. Metadata is
// copied so the original observation cannot change during validation.
type staleCacheEntry struct {
	fs.DirEntry
	info fs.FileInfo
}

func (e staleCacheEntry) Info() (fs.FileInfo, error) { return e.info, nil }

type linkCacheInfo struct {
	fs.FileInfo
	stat syscall.Stat_t
}

func (i linkCacheInfo) Sys() any { return &i.stat }

func TestReadOnlyCacheZeroLinkReobservation(t *testing.T) {
	for _, replacement := range []string{"absent", "regular", "symlink", "hardlink", "shared", "fifo", "directory"} {
		t.Run(replacement, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "index-v11.db-journal")
			if err := os.WriteFile(path, []byte("transient"), 0600); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			info, err := entries[0].Info()
			if err != nil {
				t.Fatal(err)
			}
			stat := *info.Sys().(*syscall.Stat_t)
			stat.Nlink = 0
			stale := staleCacheEntry{entries[0], linkCacheInfo{info, stat}}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte("sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			switch replacement {
			case "regular":
				err = os.WriteFile(path, []byte("new"), 0600)
			case "symlink":
				err = os.Symlink(target, path)
			case "hardlink":
				err = os.Link(target, path)
			case "shared":
				err = os.WriteFile(path, []byte("new"), 0600)
				if err == nil {
					err = os.Chmod(path, 0666)
				}
			case "directory":
				err = os.Mkdir(path, 0700)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = validateCacheEntry(root, path, stale, nil)
			allowed := replacement == "absent" || replacement == "regular"
			if (err == nil) != allowed {
				t.Fatalf("replacement %s: %v", replacement, err)
			}
			if err := validateCacheEntry(path, path, stale, nil); err == nil {
				t.Fatal("zero-link namespace accepted")
			}
			if err := validateCacheEntry(filepath.Join(root, "other"), path, stale, nil); err == nil {
				t.Fatal("zero-link outside path accepted")
			}
			// A known hardlink must still fail even when the pathname is now safe.
			stat.Nlink = 2
			if err := validateCacheEntry(root, path, staleCacheEntry{entries[0], linkCacheInfo{info, stat}}, nil); err == nil {
				t.Fatal("known hardlink observation retried")
			}
		})
	}
}
