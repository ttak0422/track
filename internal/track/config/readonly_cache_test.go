//go:build linux || darwin

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type cacheFixture struct {
	root  string
	cfg   *Config
	files []string
}

func newCacheFixture(t *testing.T) cacheFixture {
	t.Helper()
	// Canonicalize the harness temp parent, e.g. /var -> /private/var on macOS.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mkdir := func(name string) string {
		p := filepath.Join(base, name)
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	root := mkdir("cache")
	vault := mkdir("vault")
	peer := mkdir("peer")
	normal := mkdir("normal")
	file := filepath.Join(base, "config.yml")
	if err := os.WriteFile(file, []byte("config sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	return cacheFixture{root, &Config{VaultDir: vault, Vaults: map[string]string{"peer": peer}, DBPath: filepath.Join(normal, vaultCacheKey(vault), indexDBName())}, []string{file}}
}
func (f cacheFixture) path() (string, error) { return ReadOnlyCachePath(f.root, f.cfg, f.files) }
func TestReadOnlyCachePathDoesNotWriteOrChangeConfig(t *testing.T) {
	f := newCacheFixture(t)
	before := *f.cfg
	got, err := f.path()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(f.root, readOnlyCacheNamespace, vaultCacheKey(f.cfg.VaultDir), indexDBName())
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("validation wrote: %v %v", entries, err)
	}
	if !reflect.DeepEqual(before, *f.cfg) {
		t.Fatal("config changed")
	}
	if _, err := os.Stat(filepath.Dir(f.cfg.DBPath)); !os.IsNotExist(err) {
		t.Fatalf("normal cache changed: %v", err)
	}
}
func TestReadOnlyCacheRejectsInvalidRootsAndInputs(t *testing.T) {
	cases := map[string]func(*cacheFixture){
		"relative": func(f *cacheFixture) { f.root = "relative" },
		"unclean":  func(f *cacheFixture) { f.root += "/../cache" },
		"missing":  func(f *cacheFixture) { f.root = filepath.Join(f.root, "absent") },
		"shared": func(f *cacheFixture) {
			if err := os.Chmod(f.root, 0755); err != nil {
				t.Fatal(err)
			}
		},
		"nil-config":            func(f *cacheFixture) { f.cfg = nil },
		"no-config-files":       func(f *cacheFixture) { f.files = nil },
		"relative-config":       func(f *cacheFixture) { f.files = []string{"config.yml"} },
		"missing-config-parent": func(f *cacheFixture) { f.files = []string{filepath.Join(f.root, "missing", "config.yml")} },
		"missing-peer":          func(f *cacheFixture) { f.cfg.Vaults["peer"] = filepath.Join(f.root, "absent") },
		"relative-peer":         func(f *cacheFixture) { f.cfg.Vaults["peer"] = "peer" },
		"missing-active":        func(f *cacheFixture) { f.cfg.VaultDir = filepath.Join(f.root, "absent") },
		"missing-normal-db":     func(f *cacheFixture) { f.cfg.DBPath = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCacheFixture(t)
			mutate(&f)
			if got, err := f.path(); err == nil || got != "" {
				t.Fatalf("accepted %q %v", got, err)
			}
		})
	}
}
func TestReadOnlyCacheRejectsVaultConfigAndNormalCacheOverlap(t *testing.T) {
	for _, kind := range []string{"active", "peer", "config", "normal", "namespace-inside-vault", "vault-inside-namespace"} {
		t.Run(kind, func(t *testing.T) {
			f := newCacheFixture(t)
			ns := filepath.Join(f.root, readOnlyCacheNamespace)
			if err := os.Mkdir(ns, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "active":
				f.cfg.VaultDir = f.root
			case "peer":
				f.cfg.Vaults["peer"] = f.root
			case "config":
				f.files = []string{filepath.Join(ns, "not-created.yml")}
			case "normal":
				f.cfg.DBPath = filepath.Join(ns, "ordinary", "index.db")
			case "namespace-inside-vault":
				f.root = f.cfg.VaultDir
			case "vault-inside-namespace":
				p := filepath.Join(ns, "vault")
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
				f.cfg.Vaults["peer"] = p
			}
			if _, err := f.path(); err == nil {
				t.Fatal("overlap accepted")
			}
		})
	}
}
func TestReadOnlyCacheRejectsSymlinksAndHardlinks(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "namespace", "nested-dir", "db", "wal", "pid", "hardlink-db", "hardlink-pid", "normal-alias", "broken-peer", "broken-config"} {
		t.Run(kind, func(t *testing.T) {
			f := newCacheFixture(t)
			ns := filepath.Join(f.root, readOnlyCacheNamespace)
			link := func(target, path string) {
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "root":
				p := f.root + "-link"
				link(f.root, p)
				f.root = p
			case "ancestor":
				p := f.root + "-link"
				link(filepath.Dir(f.root), p)
				f.root = filepath.Join(p, filepath.Base(f.root))
			case "namespace":
				link(f.cfg.VaultDir, ns)
			case "normal-alias":
				if err := os.Mkdir(ns, 0700); err != nil {
					t.Fatal(err)
				}
				link(ns, filepath.Join(f.root, "ordinary-link"))
				f.cfg.DBPath = filepath.Join(f.root, "ordinary-link", "v", "index.db")
			case "broken-peer":
				p := filepath.Join(f.root, "peer-link")
				link(filepath.Join(f.root, "missing"), p)
				f.cfg.Vaults["peer"] = p
			case "broken-config":
				p := filepath.Join(f.root, "config-link")
				link(filepath.Join(f.root, "missing"), p)
				f.files = []string{p}
			default:
				if err := os.Mkdir(ns, 0700); err != nil {
					t.Fatal(err)
				}
				p := filepath.Join(ns, kind)
				if kind == "hardlink-db" || kind == "hardlink-pid" {
					if err := os.Link(f.files[0], p); err != nil {
						t.Fatal(err)
					}
				} else {
					link(f.cfg.VaultDir, p)
				}
			}
			if _, err := f.path(); err == nil {
				t.Fatal("alias accepted")
			}
		})
	}
}
func TestReadOnlyCacheAcceptsExistingOrdinaryCacheFileModes(t *testing.T) {
	f := newCacheFixture(t)
	path, err := f.path()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite/store and PID routines currently create 0755 dirs and 0644 files.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm", path + ".init.lock", filepath.Join(f.root, readOnlyCacheNamespace, "web-test.pid")} {
		if err := os.WriteFile(p, []byte("derived"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.path(); err != nil {
		t.Fatal(err)
	}
	// A config absent at a verified parent is protected without being created.
	f.files = append(f.files, filepath.Join(filepath.Dir(f.files[0]), "absent.yml"))
	if _, err := f.path(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := f.path(); err == nil {
		t.Fatal("shared-writable cache file accepted")
	}
}
func TestReadOnlyCacheValidatesWholeNamespaceAndEntryBound(t *testing.T) {
	f := newCacheFixture(t)
	ns := filepath.Join(f.root, readOnlyCacheNamespace)
	if err := os.Mkdir(ns, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < readOnlyCacheEntryLimit; i++ {
		p := filepath.Join(ns, fmtName(i))
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.path(); err == nil {
		t.Fatal("unbounded validation accepted")
	}
}
func fmtName(i int) string { return fmt.Sprintf("file-%04d", i) }
func TestReadOnlyCacheContainmentUsesPathComponents(t *testing.T) {
	if overlaps("/a/cache", "/a/cache-other") || overlaps("/a/cache", "/a/cache/../else") {
		t.Fatal("sibling treated as contained")
	}
	for _, p := range []string{"/a/cache", "/a/cache/x", "/a/cache/..x"} {
		if !overlaps("/a/cache", p) {
			t.Fatalf("missed %s", p)
		}
	}
}
