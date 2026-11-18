//go:build linux || darwin

package webui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// These sentinels deliberately include directories and mtimes, not just note
// bytes. Creating/removing a sidecar or rewriting identical metadata must fail.
// Access time is excluded because reading a file can legitimately update it.
type canonicalSentinel struct {
	Mode   fs.FileMode
	Size   int64
	Mtime  int64
	Digest [32]byte
}

func canonicalInventory(t *testing.T, paths ...string) map[string]canonicalSentinel {
	t.Helper()
	result := map[string]canonicalSentinel{}
	for i, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			row := canonicalSentinel{Mode: info.Mode(), Size: info.Size(), Mtime: info.ModTime().UnixNano()}
			if info.Mode().IsRegular() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				row.Digest = sha256.Sum256(data)
			} else if !info.IsDir() {
				return fmt.Errorf("unexpected sentinel file kind: %s", path)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			result[fmt.Sprintf("%d/%s", i, rel)] = row
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}
func assertCanonicalInventory(t *testing.T, before map[string]canonicalSentinel, paths ...string) {
	t.Helper()
	after := canonicalInventory(t, paths...)
	if !reflect.DeepEqual(before, after) {
		for p, want := range before {
			if got, ok := after[p]; !ok || want != got {
				t.Errorf("canonical changed/disappeared %s: before=%+v after=%+v present=%v", p, want, got, ok)
			}
		}
		for p := range after {
			if _, ok := before[p]; !ok {
				t.Errorf("canonical entry created: %s", p)
			}
		}
		t.FailNow()
	}
}

type sentinelFixture struct {
	active, peer, cache, ordinary, configuration string
	cfg                                          *config.Config
}

func newSentinelFixture(t *testing.T) sentinelFixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := func(name string) string {
		p := filepath.Join(base, name)
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	f := sentinelFixture{active: dir("active"), peer: dir("peer"), cache: dir("isolated"), ordinary: dir("ordinary")}
	f.configuration = filepath.Join(base, "machine.yml")
	for _, root := range []string{f.active, f.peer} {
		writeVaultNote(t, root, 100, "Sentinel", "initial body\n- [ ] a task [due:2020-01-01]\n")
		cfg := &config.Config{VaultDir: root, Extensions: []string{".md"}}
		old := time.Date(2020, 1, 1, 12, 0, 0, 0, time.Local)
		if err := os.Chtimes(cfg.NotePath(100), old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "assets", "sentinel.bin"), []byte{0, 1, 2, 3}, 0644); err != nil {
			t.Fatal(err)
		}
	}
	machine := fmt.Sprintf("cache_dir: %s\nvaults:\n  main: %s\n  work: %s\n", f.ordinary, f.active, f.peer)
	if err := os.WriteFile(f.configuration, []byte(machine), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", f.configuration)
	t.Setenv("TRACK_VAULT", f.active)
	t.Setenv("TRACK_CACHE_DIR", "")
	f.cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f sentinelFixture) canonical() []string {
	return []string{f.active, f.peer, f.configuration, f.ordinary}
}
func sentinelEdit(t *testing.T, root, body string, day int) {
	t.Helper()
	cfg := &config.Config{VaultDir: root, Extensions: []string{".md"}}
	if err := os.WriteFile(cfg.NotePath(100), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2020, 1, day, 12, 0, 0, 0, time.Local)
	if err := os.Chtimes(cfg.NotePath(100), stamp, stamp); err != nil {
		t.Fatal(err)
	}
}
func sentinelWait(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("sentinel condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
func sentinelViewReady(t *testing.T, v *vaultView) {
	t.Helper()
	sentinelWait(t, func() bool {
		p := &v.progress
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.verified && !p.running && !p.state.Partial
	})
}
func sentinelResetRefresh(v *vaultView) {
	v.progress.mu.Lock()
	v.progress.last = time.Time{}
	v.progress.mu.Unlock()
	v.reindexMu.Lock()
	v.lastStale = time.Time{}
	v.reindexMu.Unlock()
}
func sentinelCachedMtime(t *testing.T, v *vaultView, root string) {
	t.Helper()
	times, err := v.store.NoteMtimes()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{VaultDir: root, Extensions: []string{".md"}}
	info, err := os.Stat(cfg.NotePath(100))
	if err != nil {
		t.Fatal(err)
	}
	if times[100] != info.ModTime().Unix() {
		t.Fatalf("derived cache stale: got %d want %d", times[100], info.ModTime().Unix())
	}
}

func TestDefaultPolicyStillRecordsCanonicalActivity(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	f := newSentinelFixture(t)
	st, err := store.Open(f.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(f.cfg, st)
	defer s.closeViews()
	s.reconcileAfterChange()
	sentinelEdit(t, f.active, "default mode edited\n", 2)
	before := canonicalInventory(t, f.active, f.peer, f.configuration)
	s.reconcileAfterChange()
	cfg := &config.Config{VaultDir: f.active}
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(100))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(meta.Days, ","), "2020-01-02") {
		t.Fatalf("default activity no longer writes: %v", meta.Days)
	}
	if reflect.DeepEqual(before, canonicalInventory(t, f.active, f.peer, f.configuration)) {
		t.Fatal("default positive control did not alter canonical inventory")
	}
}

// Config workers keep their launch environment. Run fixture cases in isolated
// test processes rather than leaking TRACK_CONFIG across an existing worker pool.
func sentinelChild(t *testing.T) bool {
	t.Helper()
	if os.Getenv("TRACK_SENTINEL_CASE") == t.Name() {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "TRACK_SENTINEL_CASE="+t.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sentinel child: %v\n%s", err, output)
	}
	t.Log(string(output))
	return true
}
func TestReadPolicyCanonicalSentinelRefreshPaths(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%v", async), func(t *testing.T) {
			f := newSentinelFixture(t)
			before := canonicalInventory(t, f.canonical()...)
			cacheBefore := canonicalInventory(t, f.cache)
			policy, err := newReadPolicy(f.cache, []string{f.configuration})
			if err != nil {
				t.Fatal(err)
			}
			st, err := policy.open(f.cfg, store.Open)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			s := newServerWithReadPolicy(f.cfg, st, true, policy)
			defer s.closeViews()
			if async {
				s.enableAsync()
			}
			s.refresh(s.active)
			if async {
				sentinelViewReady(t, s.active)
			}
			var peer *vaultView
			if async {
				s.kickView("work")
				sentinelWait(t, func() bool {
					v, ok := s.cachedView("work")
					if !ok || v.cfg.Provisional {
						return false
					}
					peer = v
					return true
				})
				sentinelPeerReadReady(t, s, peer)
			} else {
				peer, err = s.viewByName("work")
				if err != nil {
					t.Fatal(err)
				}
				s.refresh(peer)
			}
			sentinelCachedMtime(t, s.active, f.active)
			sentinelCachedMtime(t, peer, f.peer)
			if !async {
				// Settle the first warm pass (cold initialization has no version memo yet).
				sentinelResetRefresh(s.active)
				s.refresh(s.active)
				revision, err := st.Revision()
				if err != nil {
					t.Fatal(err)
				}
				for range 3 {
					sentinelResetRefresh(s.active)
					s.refresh(s.active)
					s.reconcileAfterChange()
				}
				after, err := st.Revision()
				if err != nil {
					t.Fatal(err)
				}
				if after != revision {
					t.Fatalf("unchanged sync reads/watch rewrote derived cache: %d -> %d", revision, after)
				}
			}
			assertCanonicalInventory(t, before, f.canonical()...)
			if reflect.DeepEqual(cacheBefore, canonicalInventory(t, f.cache)) {
				t.Fatal("startup did not build isolated derived cache")
			}

			for i, step := range []struct {
				name  string
				view  *vaultView
				root  string
				watch bool
			}{
				{"active read refresh", s.active, f.active, false},
				{"foreign read refresh", peer, f.peer, false},
				{"active watch reconciliation", s.active, f.active, true},
			} {
				sentinelEdit(t, step.root, step.name+" changed body\n", i+2)
				baseline := canonicalInventory(t, f.canonical()...)
				cache := canonicalInventory(t, f.cache)
				sentinelResetRefresh(step.view)
				if step.watch {
					s.reconcileAfterChange()
				} else {
					s.refresh(step.view)
				}
				if async {
					sentinelViewReady(t, step.view)
				}
				sentinelCachedMtime(t, step.view, step.root)
				assertCanonicalInventory(t, baseline, f.canonical()...)
				if reflect.DeepEqual(cache, canonicalInventory(t, f.cache)) {
					t.Fatalf("%s did not change derived cache", step.name)
				}
			}
			// Close views explicitly before the final sentinel, including foreign stores.
			final := canonicalInventory(t, f.canonical()...)
			s.closeViews()
			assertCanonicalInventory(t, final, f.canonical()...)
		})
	}
}

func TestReadPolicyCanonicalSentinelBootstrapRetryAndPromotion(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	f := newSentinelFixture(t)
	before := canonicalInventory(t, f.canonical()...)
	cacheBefore := canonicalInventory(t, f.cache)
	p, err := newReadPolicy(f.cache, []string{f.configuration})
	if err != nil {
		t.Fatal(err)
	}
	var allowStrict atomic.Bool
	loader := func(_ context.Context, _ string, strict bool) (*config.Config, error) {
		if strict && !allowStrict.Load() {
			return nil, errors.New("controlled strict validation pending")
		}
		copy := *f.cfg
		copy.Provisional = !strict
		return &copy, nil
	}
	var leasePath string
	b := newBootstrapWithReadPolicy(context.Background(), loader, store.Open, func(cfg *config.Config) (func(), error) { leasePath = cfg.DBPath; return func() {}, nil }, p)
	defer b.Close()
	b.start()
	var provisional *Server
	sentinelWait(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.server == nil || b.running {
			return false
		}
		provisional = b.server
		return true
	})
	if !provisional.cfg.Provisional {
		t.Fatal("initial server skipped controlled provisional phase")
	}
	sentinelViewReady(t, provisional.active)
	sentinelCachedMtime(t, provisional.active, f.active)
	assertCanonicalInventory(t, before, f.canonical()...)
	if !strings.HasPrefix(leasePath, f.cache+string(filepath.Separator)) || leasePath == f.cfg.DBPath {
		t.Fatalf("lease not isolated: %s", leasePath)
	}
	if provisional.cfg.DBPath != f.cfg.DBPath {
		t.Fatal("original config DBPath was mutated")
	}

	allowStrict.Store(true)
	b.start()
	sentinelWait(t, func() bool { return b.Status().Phase == "ready" })
	b.mu.Lock()
	verified := b.server
	generation := b.generation
	b.mu.Unlock()
	if verified == provisional || verified.cfg.Provisional || generation < 2 {
		t.Fatal("retry did not replace provisional generation")
	}
	sentinelViewReady(t, verified.active)
	var peer *vaultView
	verified.kickView("work")
	sentinelWait(t, func() bool {
		v, ok := verified.cachedView("work")
		if !ok || v.cfg.Provisional {
			return false
		}
		peer = v
		return true
	})
	sentinelPeerReadReady(t, verified, peer)
	sentinelCachedMtime(t, peer, f.peer)
	for _, target := range []string{"/api/notes", "/api/notes?vault=work", "/api/note?id=100", "/api/note?id=100&vault=work"} {
		response := bootRequest(b, "GET", target, "")
		if response.Code != 200 || !strings.Contains(response.Body.String(), "Sentinel") {
			t.Fatalf("read %s: %d %s", target, response.Code, response.Body.String())
		}
	}
	assertCanonicalInventory(t, before, f.canonical()...)
	if reflect.DeepEqual(cacheBefore, canonicalInventory(t, f.cache)) {
		t.Fatal("bootstrap/retry did not create derived cache")
	}
	b.Close()
	assertCanonicalInventory(t, before, f.canonical()...)
}

func sentinelPeerReadReady(t *testing.T, s *Server, v *vaultView) {
	t.Helper()
	// Strict promotion exposes the replacement while holding its mutation gate.
	// Drive the read-time refresh path rather than assuming its initial TryLock won.
	sentinelWait(t, func() bool {
		s.refresh(v)
		p := &v.progress
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.verified && !p.running && !p.state.Partial
	})
}
func TestReadPolicyCanonicalSentinelRejectsOrdinaryStore(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	f := newSentinelFixture(t)
	normal, err := store.Open(f.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer normal.Close()
	p, err := newReadPolicy(f.cache, []string{f.configuration})
	if err != nil {
		t.Fatal(err)
	}
	baseline := canonicalInventory(t, f.canonical()...)
	s := newServerWithReadPolicy(f.cfg, normal, true, p)
	defer s.closeViews()
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest("GET", "http://localhost/api/notes", nil))
	if r.Code < 400 {
		t.Fatalf("ordinary cache accepted: %d %s", r.Code, r.Body.String())
	}
	s.refresh(s.active)
	s.reconcileAfterChange()
	assertCanonicalInventory(t, baseline, f.canonical()...)
	rows, err := normal.AllNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatal("mismatched ordinary cache indexed")
	}
	entries, err := os.ReadDir(f.cache)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected store created cache entries: %v %v", entries, err)
	}
}

func TestReadPolicyCanonicalSentinelBootstrapRejectsWrongOpenedStore(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	f := newSentinelFixture(t)
	st, err := store.Open(f.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := store.ReadState{CheckedAt: 12345, Partial: true, Issues: []store.ReadIssue{{Reason: "ordinary-cache sentinel"}}}
	if err := st.SetReadState(sentinel); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	before := canonicalInventory(t, f.active, f.peer, f.configuration)
	p, err := newReadPolicy(f.cache, []string{f.configuration})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	loader := func(_ context.Context, _ string, strict bool) (*config.Config, error) {
		copy := *f.cfg
		copy.Provisional = !strict
		return &copy, nil
	}
	opener := func(string) (*store.Store, error) { calls.Add(1); return store.Open(f.cfg.DBPath) }
	b := newBootstrapWithReadPolicy(context.Background(), loader, opener, nil, p)
	defer b.Close()
	b.start()
	sentinelWait(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return calls.Load() > 0 && !b.running && !b.openPending })
	b.mu.Lock()
	installed := b.server != nil
	reason := b.status.Reason
	b.mu.Unlock()
	if installed {
		t.Fatal("bootstrap installed an ordinary store returned by injected opener")
	}
	if !strings.Contains(reason, "store") {
		t.Fatalf("missing store-policy failure: %s", reason)
	}
	b.mu.Lock()
	early := b.peers
	b.mu.Unlock()
	b.Close()
	if early != nil {
		sentinelWait(t, func() bool { early.viewsMu.Lock(); defer early.viewsMu.Unlock(); return len(early.opening) == 0 })
	}
	// Opening and closing SQLite changes WAL/SHM bytes itself. Compare semantic
	// ReadState after reopen, not those incidental writes made by this test.
	reopened, err := store.Open(f.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sentinel) {
		t.Fatalf("bootstrap altered ordinary ReadState: got %+v want %+v", got, sentinel)
	}
	assertCanonicalInventory(t, before, f.active, f.peer, f.configuration)
}

func TestReadPolicyCanonicalSentinelEarlyPeerAndCancelledOpen(t *testing.T) {
	if sentinelChild(t) {
		return
	}
	f := newSentinelFixture(t)
	sentinelEdit(t, f.peer, "foreign early-peer body\n", 2)
	before := canonicalInventory(t, f.canonical()...)
	cacheBefore := canonicalInventory(t, f.cache)
	p, err := newReadPolicy(f.cache, []string{f.configuration})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	opened := make(chan *store.Store, 1)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var acquired, released atomic.Int32
	loader := func(_ context.Context, _ string, strict bool) (*config.Config, error) {
		copy := *f.cfg
		copy.Provisional = !strict
		return &copy, nil
	}
	opener := func(path string) (*store.Store, error) {
		close(entered)
		<-release // Deliberately held until after cancellation; cleanup always releases it.
		st, err := store.Open(path)
		opened <- st
		return st, err
	}
	lease := func(*config.Config) (func(), error) { acquired.Add(1); return func() { released.Add(1) }, nil }
	b := newBootstrapWithReadPolicy(context.Background(), loader, opener, lease, p)
	defer b.Close()
	defer unblock()
	b.start()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("active opener not entered")
	}
	var early *Server
	sentinelWait(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); early = b.peers; return early != nil })
	var peer *vaultView
	sentinelWait(t, func() bool {
		v, ok := early.cachedView("work")
		if !ok || v.cfg.Provisional {
			return false
		}
		peer = v
		return true
	})
	sentinelPeerReadReady(t, early, peer)
	b.mu.Lock()
	installed := b.server != nil
	b.mu.Unlock()
	if installed {
		t.Fatal("active server installed before held cache open completed")
	}
	response := bootRequest(b, "GET", "/api/note?id=100&vault=work", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "foreign early-peer body") || !strings.Contains(response.Body.String(), `"vault":"work"`) {
		t.Fatalf("early peer not served independently: %d %s", response.Code, response.Body.String())
	}
	sentinelCachedMtime(t, peer, f.peer)
	assertCanonicalInventory(t, before, f.canonical()...)
	if reflect.DeepEqual(cacheBefore, canonicalInventory(t, f.cache)) {
		t.Fatal("early peer did not create isolated cache")
	}

	// Close before the late cache open returns. Neither that completion nor its
	// cleanup may install a new backend or touch canonical files/ordinary caches.
	b.Close()
	if acquired.Load() != 1 || released.Load() != 1 {
		t.Fatalf("lease lifecycle before late return: %d/%d", acquired.Load(), released.Load())
	}
	unblock()
	var late *store.Store
	select {
	case late = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("late cache opener did not return")
	}
	if late == nil {
		t.Fatal("fixture did not exercise a successful late open")
	}
	sentinelWait(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.openPending })
	if _, err := late.Revision(); err == nil {
		t.Fatal("late cache store not closed")
	}
	b.mu.Lock()
	installed = b.server != nil
	b.mu.Unlock()
	if installed || released.Load() != 1 {
		t.Fatal("late completion installed server or released lease twice")
	}
	assertCanonicalInventory(t, before, f.canonical()...)
}
