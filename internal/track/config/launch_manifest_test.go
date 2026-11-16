//go:build linux || darwin

package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type manifestFixture struct{ root, home, main, work, normal, private, config string }

func newManifestFixture(t *testing.T) manifestFixture {
	t.Helper()
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "TRACK_") {
			t.Setenv(key, "")
		}
	}
	root := t.TempDir()
	f := manifestFixture{root: root, home: filepath.Join(root, "home"), main: filepath.Join(root, "main"), work: filepath.Join(root, "work"), normal: filepath.Join(root, "normal"), private: filepath.Join(root, "private"), config: filepath.Join(root, "machine.yml")}
	for _, p := range []string{f.home, filepath.Join(f.main, ".track"), filepath.Join(f.work, ".track"), f.normal, f.private} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg"))
	t.Setenv("TRACK_CONFIG", f.config)
	f.write(t, f.normal, f.work)
	return f
}
func (f manifestFixture) write(t *testing.T, cache, work string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"default_vault": "main", "cache_dir": cache, "vaults": map[string]string{"main": f.main, "work": work}})
	if err := os.WriteFile(f.config, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f manifestFixture) resolve(t *testing.T, selector LaunchSelector) ReadOnlyLaunchManifest {
	t.Helper()
	m, e := ResolveReadOnlyLaunchManifest(context.Background(), selector, f.private)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestLaunchManifestStableAndContextBound(t *testing.T) {
	f := newManifestFixture(t)
	m := f.resolve(t, LaunchSelector{Name: "work"})
	if m.Active.Name != "work" || len(m.Peers) != 2 || len(m.Configurations) != 3 || len(m.Revision) != 64 || m.Active.OrdinaryDB == m.Active.PrivateDB {
		t.Fatalf("bad manifest %#v", m)
	}
	if _, e := VerifyReadOnlyLaunchManifest(context.Background(), m.Selector, f.private, m.Revision); e != nil {
		t.Fatal(e)
	}
	t.Setenv("TRACK_GEN_KEEP", "11")
	if e := verifyManifestInputs(context.Background(), m); !errors.Is(e, ErrLaunchManifestChanged) {
		t.Fatalf("context changed: %v", e)
	}
}
func TestLaunchManifestBindsPrivateAndNormalCacheIdentity(t *testing.T) {
	for _, kind := range []string{"private", "normal"} {
		t.Run(kind, func(t *testing.T) {
			f := newManifestFixture(t)
			m := f.resolve(t, LaunchSelector{Name: "work"})
			path := f.private
			if kind == "normal" {
				path = f.normal
			}
			if e := os.Rename(path, path+"-old"); e != nil {
				t.Fatal(e)
			}
			if e := os.Mkdir(path, 0700); e != nil {
				t.Fatal(e)
			}
			if _, e := VerifyReadOnlyLaunchManifest(context.Background(), m.Selector, f.private, m.Revision); !errors.Is(e, ErrLaunchManifestChanged) {
				t.Fatalf("replacement accepted: %v", e)
			}
		})
	}
}
func TestLaunchManifestRechecksDeclaredAliasAndNormalAlias(t *testing.T) {
	for _, kind := range []string{"registry", "cache"} {
		t.Run(kind, func(t *testing.T) {
			f := newManifestFixture(t)
			alias := filepath.Join(f.root, "alias")
			target := f.work
			if kind == "cache" {
				target = f.normal
			}
			if e := os.Symlink(target, alias); e != nil {
				t.Fatal(e)
			}
			if kind == "registry" {
				f.write(t, f.normal, alias)
			} else {
				f.write(t, alias, f.work)
			}
			// Selecting canonical work must not erase the declared registry alias.
			m := f.resolve(t, LaunchSelector{Path: f.work})
			if kind == "registry" {
				found := false
				for _, v := range m.Peers {
					if v.Name == "work" {
						found = v.DisplayPath == alias
					}
				}
				if !found {
					t.Fatal("declared alias lost")
				}
			}
			alternate := filepath.Join(f.root, "alternate")
			if e := os.MkdirAll(filepath.Join(alternate, ".track"), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.Remove(alias); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(alternate, alias); e != nil {
				t.Fatal(e)
			}
			if e := verifyManifestInputs(context.Background(), m); !errors.Is(e, ErrLaunchManifestChanged) {
				t.Fatalf("mid-resolution alias changed: %v", e)
			}
			if _, e := VerifyReadOnlyLaunchManifest(context.Background(), m.Selector, f.private, m.Revision); !errors.Is(e, ErrLaunchManifestChanged) {
				t.Fatalf("alias revision unchanged: %v", e)
			}
		})
	}
}
func TestLaunchManifestCapturedConfigsRecheckedAndAbsentTracked(t *testing.T) {
	f := newManifestFixture(t)
	m := f.resolve(t, LaunchSelector{Name: "work"})
	peer := VaultConfigPath(f.main)
	if e := os.WriteFile(peer, []byte("journal: false\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := verifyManifestInputs(context.Background(), m); !errors.Is(e, ErrLaunchManifestChanged) {
		t.Fatalf("absent config appeared: %v", e)
	}
	m = f.resolve(t, LaunchSelector{Name: "work"})
	if e := os.WriteFile(f.config, []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := verifyManifestInputs(context.Background(), m); !errors.Is(e, ErrLaunchManifestChanged) {
		t.Fatalf("machine changed: %v", e)
	}
}
func TestLaunchManifestRejectsBoundsSpecialsAndSelectors(t *testing.T) {
	f := newManifestFixture(t)
	for _, sel := range []LaunchSelector{{Name: "unknown"}, {Name: "work", Path: f.work}, {Name: "bad/name"}} {
		if _, e := ResolveReadOnlyLaunchManifest(context.Background(), sel, f.private); e == nil {
			t.Fatalf("selector accepted %+v", sel)
		}
	}
	f.write(t, "relative", f.work)
	if _, e := ResolveReadOnlyLaunchManifest(context.Background(), LaunchSelector{}, f.private); e == nil {
		t.Fatal("relative ordinary DB accepted")
	}
	f.write(t, f.normal, "relative")
	if _, e := ResolveReadOnlyLaunchManifest(context.Background(), LaunchSelector{}, f.private); e == nil {
		t.Fatal("relative peer accepted")
	}
	if e := os.Remove(f.config); e != nil {
		t.Fatal(e)
	}
	if e := syscall.Mkfifo(f.config, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ResolveReadOnlyLaunchManifest(context.Background(), LaunchSelector{}, f.private); e == nil {
		t.Fatal("FIFO accepted")
	}
	if e := os.Remove(f.config); e != nil {
		t.Fatal(e)
	}
	file, e := os.Create(f.config)
	if e != nil {
		t.Fatal(e)
	}
	if e = file.Truncate(manifestConfigBytes + 1); e != nil {
		t.Fatal(e)
	}
	file.Close()
	if _, e := ResolveReadOnlyLaunchManifest(context.Background(), LaunchSelector{}, f.private); e == nil {
		t.Fatal("oversize config accepted")
	}
	if e := os.Remove(f.config); e != nil {
		t.Fatal(e)
	}
	f.write(t, f.normal, f.work)
	if e := os.Link(f.config, f.config+"-link"); e != nil {
		t.Fatal(e)
	}
	if _, e := ResolveReadOnlyLaunchManifest(context.Background(), LaunchSelector{}, f.private); e == nil {
		t.Fatal("hardlinked config accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := ResolveReadOnlyLaunchManifest(ctx, LaunchSelector{}, "/absent"); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel %v", e)
	}
}

func TestLaunchManifestOriginalRegistryMappingCannotDrift(t *testing.T) {
	f := newManifestFixture(t)
	alias := filepath.Join(f.root, "peer-alias")
	if e := os.Symlink(f.work, alias); e != nil {
		t.Fatal(e)
	}
	registry, names, e := resolveVaults(map[string]string{"main": f.main, "work": alias})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = manifestDeclaredRoot("work", registry["work"], names, true); e != nil {
		t.Fatal(e)
	}
	// Non-active alias now resolves to another declared name's canonical root.
	if e = os.Remove(alias); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(f.main, alias); e != nil {
		t.Fatal(e)
	}
	if _, _, e = manifestDeclaredRoot("work", registry["work"], names, true); !errors.Is(e, ErrLaunchManifestChanged) {
		t.Fatalf("duplicate canonical root accepted: %v", e)
	}
	other := filepath.Join(f.root, "other")
	if e = os.Mkdir(other, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(alias); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(other, alias); e != nil {
		t.Fatal(e)
	}
	if _, _, e = manifestDeclaredRoot("work", registry["work"], names, true); !errors.Is(e, ErrLaunchManifestChanged) {
		t.Fatalf("unmapped root accepted: %v", e)
	}
}
