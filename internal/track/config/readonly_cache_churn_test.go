//go:build linux || darwin

package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
