//go:build linux || darwin

package webui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/vaultfile"
)

const boundProcessEnv = "GO_BOUND_MANIFEST_CASE"
const boundProcessNonce = "0123456789abcdef0123456789abcdef"

// Fresh processes isolate config worker environments and global working dirs.
// No public CLI/HTTP startup endpoint or native ordinary admission is introduced.
func boundProcessChild(t *testing.T) bool {
	t.Helper()
	if os.Getenv(boundProcessEnv) == t.Name() {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.v")
	coverageDir := ""
	if testing.CoverMode() != "" {
		coverageDir = t.TempDir()
		// Coverage output is test infrastructure, outside the child-owned fixture.
		// Never filter it out of the canonical/private sentinel inventory.
		cmd.Args = append(cmd.Args, "-test.gocoverdir="+coverageDir)
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "TMPDIR=" + os.TempDir(), boundProcessEnv + "=" + t.Name()}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = kill
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = kill()
	}
	if cmd.Process != nil {
		deadline := time.Now().Add(2 * time.Second)
		for {
			groupErr := syscall.Kill(-cmd.Process.Pid, 0)
			if errors.Is(groupErr, syscall.ESRCH) {
				break
			}
			if groupErr != nil || time.Now().After(deadline) {
				_ = kill()
				t.Fatalf("owned test group did not exit: %v; %v\n%s", groupErr, err, output)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err != nil {
		t.Fatalf("bound manifest subprocess: %v\n%s", err, output)
	}
	if coverageDir != "" {
		for _, pattern := range []string{"covmeta.*", "covcounters.*"} {
			matches, globErr := filepath.Glob(filepath.Join(coverageDir, pattern))
			if globErr != nil || len(matches) == 0 {
				t.Fatalf("missing external coverage output %s: %v", pattern, globErr)
			}
		}
	}
	t.Log(string(output))
	return true
}

type boundFixture struct {
	root, home, cwd, xdg, normal, private, machine, main, work string
	selector                                                   config.LaunchSelector
}

func newBoundFixture(t *testing.T, peers bool) boundFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := boundFixture{root: root, home: filepath.Join(root, "home"), cwd: filepath.Join(root, "cwd"), xdg: filepath.Join(root, "xdg"), normal: filepath.Join(root, "normal"), private: filepath.Join(root, "private"), machine: filepath.Join(root, "machine.json"), main: filepath.Join(root, "home", "main"), work: filepath.Join(root, "home", "work"), selector: config.LaunchSelector{Name: "main"}}
	for _, dir := range []string{f.home, f.cwd, f.xdg, f.normal, f.private, f.main, f.work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, vault := range []string{f.main, f.work} {
		if err := os.MkdirAll(filepath.Join(vault, ".track", "notes"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(vault, "note"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vault, "note", "42.md"), []byte(filepath.Base(vault)+" bound sentinel\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vault, ".track", "notes", "42.yaml"), []byte("version: 1\ntitle: Sentinel\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vault, ".track", "config.yml"), []byte("link_format: title\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry := map[string]string{"main": f.main}
	if peers {
		registry["work"] = f.work
		f.selector.Name = "work"
	}
	raw, err := json.Marshal(map[string]any{"default_vault": "main", "cache_dir": f.normal, "vaults": registry})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.machine, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_CONFIG_HOME", f.home)
	t.Setenv("XDG_CACHE_HOME", f.xdg)
	t.Setenv("TMPDIR", f.private)
	t.Setenv("TRACK_CONFIG", f.machine)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(f.cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := vaultfile.CloseWorkers(ctx); err != nil {
			t.Error(err)
		}
		if err := os.Chdir(cwd); err != nil {
			t.Error(err)
		}
	})
	return f
}

type boundInventoryEntry struct {
	Link             string
	Mode             fs.FileMode
	Size, ModifiedNS int64
	Hash             [32]byte
}

func boundInventory(t *testing.T, f boundFixture, includePrivate bool) map[string]boundInventoryEntry {
	t.Helper()
	out := map[string]boundInventoryEntry{}
	err := filepath.WalkDir(f.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !includePrivate && path == f.private {
			return filepath.SkipDir
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		row := boundInventoryEntry{Mode: info.Mode(), Size: info.Size(), ModifiedNS: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			row.Hash = sha256.Sum256(raw)
		} else if info.Mode()&os.ModeSymlink != 0 {
			row.Link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		} else if !info.IsDir() {
			t.Fatalf("unexpected fixture kind: %s", path)
		}
		rel, e := filepath.Rel(f.root, path)
		if e != nil {
			return e
		}
		out[rel] = row
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func boundUnchanged(t *testing.T, f boundFixture, before map[string]boundInventoryEntry, private bool) {
	t.Helper()
	after := boundInventory(t, f, private)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("bound fixture inventory changed\nbefore=%+v\nafter=%+v", before, after)
	}
}
func boundRevision(t *testing.T, f boundFixture) string {
	t.Helper()
	m, err := config.ResolveReadOnlyLaunchManifest(context.Background(), f.selector, f.private)
	if err != nil {
		t.Fatal(err)
	}
	return m.Revision
}
func boundRequest(t *testing.T, s *httptest.Server, method, path string) (int, string) {
	t.Helper()
	r, err := http.NewRequest(method, s.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}
func boundWait(t *testing.T, label string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", label)
		}
		time.Sleep(time.Millisecond)
	}
}
func boundCapability(t *testing.T, s *httptest.Server, revision string, version int) {
	t.Helper()
	code, raw := boundRequest(t, s, "GET", "/api/capabilities")
	want := map[string]any{"protocol": "track-canonical-read-only", "version": float64(version), "canonical_read_only": true}
	if version == 2 {
		want["manifest_revision"] = revision
		want["process_nonce"] = boundProcessNonce
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if code != 200 || !reflect.DeepEqual(got, want) {
		t.Fatalf("capability %d %s", code, raw)
	}
}
func boundEmptyPrivate(t *testing.T, f boundFixture) {
	t.Helper()
	entries, err := os.ReadDir(f.private)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected private files: %v", entries)
	}
}

func TestManifestBoundProcessRejectsInitialMismatchBeforeLease(t *testing.T) {
	if boundProcessChild(t) {
		return
	}
	f := newBoundFixture(t, true)
	before := boundInventory(t, f, true)
	var leases atomic.Int32
	_, err := newManifestBoundBootstrap(context.Background(), f.selector, f.private, strings.Repeat("0", 64), boundProcessNonce, func(*config.Config) (func(), error) { leases.Add(1); return func() {}, nil })
	if !errors.Is(err, config.ErrLaunchManifestChanged) {
		t.Fatalf("wrong initial rejection: %v", err)
	}
	if leases.Load() != 0 {
		t.Fatal("invalid binding acquired lease")
	}
	boundEmptyPrivate(t, f)
	boundUnchanged(t, f, before, true)
}
func TestManifestBoundProcessKeepsV1Unchanged(t *testing.T) {
	if boundProcessChild(t) {
		return
	}
	f := newBoundFixture(t, true)
	before := boundInventory(t, f, true)
	b, err := NewCanonicalReadOnlyBootstrap(context.Background(), f.private, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s := httptest.NewServer(b)
	defer s.Close()
	boundCapability(t, s, "", 1)
	boundEmptyPrivate(t, f)
	boundUnchanged(t, f, before, true)
}
func TestManifestBoundProcessConfigDriftIsSticky(t *testing.T) {
	for _, change := range []string{"machine", "peer-config", "selected-work-alias"} {
		t.Run(change, func(t *testing.T) {
			if boundProcessChild(t) {
				return
			}
			f := newBoundFixture(t, true)
			if change == "selected-work-alias" {
				alias := filepath.Join(f.home, "work-alias")
				if err := os.Symlink(f.work, alias); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(map[string]any{"default_vault": "main", "cache_dir": f.normal, "vaults": map[string]string{"main": f.main, "work": alias}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.machine, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := boundInventory(t, f, false)
			revision := boundRevision(t, f)
			var releases atomic.Int32
			b, err := newManifestBoundBootstrap(context.Background(), f.selector, f.private, revision, boundProcessNonce, func(cfg *config.Config) (func(), error) {
				if !strings.HasPrefix(cfg.DBPath, f.private+string(os.PathSeparator)) {
					t.Errorf("lease outside private cache: %s", cfg.DBPath)
				}
				return func() { releases.Add(1) }, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			s := httptest.NewServer(b)
			defer s.Close()
			boundCapability(t, s, revision, 2)
			b.start()
			defer func() {
				if !t.Failed() {
					return
				}
				// Observe only: do not trigger a refresh or retry after failure.
				t.Logf("bound startup status: %+v", b.Status())
				b.mu.Lock()
				srv := b.server
				b.mu.Unlock()
				if srv != nil {
					t.Logf("bound active read state: %+v", srv.active.asyncState())
				}
				b.readPolicy.binding.mu.Lock()
				t.Logf("bound manifest invalidation: %v", b.readPolicy.binding.invalid)
				b.readPolicy.binding.mu.Unlock()
			}()
			boundWait(t, "bound active ready", func() bool { return b.Status().Phase == "ready" })
			for _, name := range []string{"main", "work"} {
				boundWait(t, "bound peer read", func() bool {
					code, raw := boundRequest(t, s, "GET", "/api/note?id=42&vault="+name)
					if code != 200 {
						return false
					}
					var document struct {
						Note struct {
							NoteID int64  `json:"note_id"`
							Vault  string `json:"vault"`
							Body   string `json:"body"`
							Path   string `json:"path"`
						} `json:"note"`
					}
					if err := json.Unmarshal([]byte(raw), &document); err != nil {
						t.Fatal(err)
					}
					wantLabel := name
					if name == "work" {
						wantLabel = ""
					}
					canonical, err := filepath.EvalSymlinks(document.Note.Path)
					return err == nil && document.Note.NoteID == 42 && document.Note.Vault == wantLabel && document.Note.Body == name+" bound sentinel" && canonical == filepath.Join(f.home, name, "note", "42.md")
				})
			}
			code, raw := boundRequest(t, s, "GET", "/api/vaults")
			var registry map[string]any
			if err := json.Unmarshal([]byte(raw), &registry); err != nil {
				t.Fatal(err)
			}
			active, ok := registry["active"].(map[string]any)
			if code != 200 || !ok || active["name"] != "work" {
				t.Fatalf("bound selector ignored: %s", raw)
			}
			boundUnchanged(t, f, before, false)
			boundCapability(t, s, revision, 2)
			startedGeneration := b.Status().Generation
			path := f.machine
			if change == "peer-config" {
				path = filepath.Join(f.main, ".track", "config.yml")
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rel, _ := filepath.Rel(f.root, path)
			old := before[rel]
			expected := make(map[string]boundInventoryEntry, len(before))
			for k, v := range before {
				expected[k] = v
			}
			replacement := append(append([]byte(nil), original...), []byte("\n# deliberate bound drift\n")...)
			row := old
			row.Size = int64(len(replacement))
			row.ModifiedNS = 1578052800 * 1e9
			row.Hash = sha256.Sum256(replacement)
			expected[rel] = row
			if err := os.WriteFile(path, replacement, 0600); err != nil {
				t.Fatal(err)
			}
			stamp := time.Unix(0, row.ModifiedNS)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			for _, request := range [][2]string{{"GET", "/api/capabilities"}, {"GET", "/api/note?id=42&vault=main"}, {"GET", "/api/note?id=42&vault=work"}, {"POST", "/api/storage"}} {
				code, raw := boundRequest(t, s, request[0], request[1])
				if code != 503 || !strings.Contains(raw, config.ErrLaunchManifestChanged.Error()) {
					t.Fatalf("drift request accepted/rebound: %v -> %d %s", request, code, raw)
				}
			}
			boundUnchanged(t, f, expected, false)
			if b.Status().Generation != startedGeneration {
				t.Fatal("rejected drift retry advanced startup generation")
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			stamp = time.Unix(0, old.ModifiedNS)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if _, err := config.VerifyReadOnlyLaunchManifest(context.Background(), f.selector, f.private, revision); err != nil {
				t.Fatalf("fixture restore did not actually restore the original revision: %v", err)
			}
			for _, request := range [][2]string{{"GET", "/api/capabilities"}, {"GET", "/api/note?id=42&vault=main"}, {"GET", "/api/note?id=42&vault=work"}, {"POST", "/api/storage"}} {
				code, raw = boundRequest(t, s, request[0], request[1])
				if code != 503 || !strings.Contains(raw, config.ErrLaunchManifestChanged.Error()) {
					t.Fatalf("restored inputs silently rebound: %v -> %d %s", request, code, raw)
				}
			}
			if b.Status().Generation != startedGeneration {
				t.Fatal("restored sticky retry advanced startup generation")
			}
			s.Close()
			b.Close()
			boundWait(t, "lease cleanup", func() bool { return releases.Load() == 1 })
			boundUnchanged(t, f, before, false)
		})
	}
}
func TestManifestBoundProcessDriftDuringLeaseNeverOpensStore(t *testing.T) {
	if boundProcessChild(t) {
		return
	}
	f := newBoundFixture(t, false)
	before := boundInventory(t, f, false)
	revision := boundRevision(t, f)
	entered, allow := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseGate := func() { once.Do(func() { close(allow) }) }
	defer releaseGate()
	var releases atomic.Int32
	b, err := newManifestBoundBootstrap(context.Background(), f.selector, f.private, revision, boundProcessNonce, func(*config.Config) (func(), error) {
		close(entered)
		select {
		case <-allow:
		case <-time.After(5 * time.Second):
			return nil, errors.New("test lease gate deadline")
		}
		return func() { releases.Add(1) }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s := httptest.NewServer(b)
	defer s.Close()
	b.start()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("lease not entered")
	}
	original, err := os.ReadFile(f.machine)
	if err != nil {
		t.Fatal(err)
	}
	replacement := append(append([]byte(nil), original...), []byte("\n# drift before store open\n")...)
	rel, _ := filepath.Rel(f.root, f.machine)
	row := before[rel]
	row.Size = int64(len(replacement))
	row.ModifiedNS = 1578052800 * 1e9
	row.Hash = sha256.Sum256(replacement)
	before[rel] = row
	if err := os.WriteFile(f.machine, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(0, row.ModifiedNS)
	if err := os.Chtimes(f.machine, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	releaseGate()
	boundWait(t, "pre-open drift rejection", func() bool { return b.Status().Phase == "unavailable" })
	if !strings.Contains(b.Status().Reason, config.ErrLaunchManifestChanged.Error()) {
		t.Fatalf("wrong drift reason: %+v", b.Status())
	}
	code, raw := boundRequest(t, s, "GET", "/api/capabilities")
	if code != 503 || !strings.Contains(raw, config.ErrLaunchManifestChanged.Error()) {
		t.Fatalf("drift handshake: %d %s", code, raw)
	}
	s.Close()
	b.Close()
	boundWait(t, "race lease cleanup", func() bool { return releases.Load() == 1 })
	boundWait(t, "late store-open completion", func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.openPending })
	boundEmptyPrivate(t, f)
	boundUnchanged(t, f, before, false)
}
