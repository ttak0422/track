package webui

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func bootRequest(b *Bootstrap, method, target, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	b.placeholder.guard(b).ServeHTTP(w, httptest.NewRequest(method, "http://localhost"+target, strings.NewReader(body)))
	return w
}
func waitBoot(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !predicate() {
		select {
		case <-deadline:
			t.Fatal("controlled bootstrap condition did not complete")
		default:
			runtime.Gosched()
		}
	}
}
func TestBootstrapShellIsUsableWhileConfigurationIsBlocked(t *testing.T) {
	entered := make(chan struct{})
	returned := make(chan struct{})
	var opens atomic.Int32
	b := newBootstrap(context.Background(), func(ctx context.Context, _ string, _ bool) (*config.Config, error) {
		close(entered)
		<-ctx.Done()
		close(returned)
		return nil, ctx.Err()
	}, func(string) (*store.Store, error) { opens.Add(1); return nil, errors.New("unexpected") }, nil)
	b.start()
	<-entered
	if w := bootRequest(b, "GET", "/", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("shell blocked: %d %s", w.Code, w.Body.String())
	}
	if w := bootRequest(b, "GET", "/api/vaults", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "initializing") {
		t.Fatalf("ownership not ready: %d %s", w.Code, w.Body.String())
	}
	if w := bootRequest(b, "POST", "/api/note", `{"title":"Unsafe"}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
	b.Close()
	<-returned
	if opens.Load() != 0 {
		t.Fatal("unverified config opened writable storage")
	}
}
func TestBootstrapColdProgressSurvivesBlockedCacheAndBackendHandoff(t *testing.T) {
	vault := t.TempDir()
	writeVaultNote(t, vault, 1, "Healthy", "Readable while cache is blocked")
	cfg := &config.Config{VaultDir: vault, VaultDirDisplay: vault, DBPath: filepath.Join(t.TempDir(), "index.db"), Extensions: []string{".md"}, DateFormat: "2006-01-02", WebHome: "Healthy", JournalOff: true, GenOff: true}
	release := make(chan struct{})
	cacheReturned := make(chan struct{})
	b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) {
		copy := *cfg
		copy.Provisional = true
		return &copy, nil
	}, func(path string) (*store.Store, error) {
		<-release
		defer close(cacheReturned)
		return store.Open(path)
	}, nil)
	b.start()
	waitBoot(t, func() bool { return b.Status().Verified == 1 })
	if b.Status().Home != "1" {
		t.Fatal("configured home lost during early startup")
	}
	for _, target := range []string{"/api/note?id=1", "/api/search?q=Healthy", "/api/notes"} {
		w := bootRequest(b, "GET", target, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Healthy") || !strings.Contains(w.Body.String(), `"partial":true`) {
			t.Fatalf("cold progress hidden: %s %d %s", target, w.Code, w.Body.String())
		}
	}
	before, _ := os.ReadFile(cfg.NotePath(1))
	if w := bootRequest(b, "PUT", "/api/note?id=1", `{"body":"must-not-save","etag":""}`); w.Code < 400 {
		t.Fatal("cold snapshot became writable")
	}
	after, _ := os.ReadFile(cfg.NotePath(1))
	if string(before) != string(after) {
		t.Fatal("cold edit touched file")
	}
	// Install an empty cache deliberately before publish; progress must not become 404/complete [].
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	srv := New(cfg, st)
	srv.enableAsync()
	srv.active.progress.state.Partial = true
	srv.active.progress.running = true
	srv.active.progress.generation = 10
	b.server = srv
	b.serverGeneration = b.generation
	srv.active.progress.home = "1"
	b.mu.Unlock()
	if b.Status().Home != "1" {
		t.Fatal("backend progress lost configured home")
	}
	if w := bootRequest(b, "GET", "/api/note?id=1", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Readable while cache") {
		t.Fatalf("handoff lost confirmed body: %d %s", w.Code, w.Body.String())
	}
	revision, _ := st.Revision()
	generation := srv.active.progress.generation
	if w := bootRequest(b, "POST", "/api/render", `{"body":"## Confirmed heading"}`); w.Code != 200 {
		t.Fatalf("read-only preview treated as mutation: %d %s", w.Code, w.Body.String())
	}
	afterRevision, _ := st.Revision()
	if revision != afterRevision || generation != srv.active.progress.generation {
		t.Fatal("render invalidated read generation")
	}
	b.Close()
	close(release)
	<-cacheReturned
}
func TestBootstrapRejectsRootRebindingAndLateConfigAfterShutdown(t *testing.T) {
	old := t.TempDir()
	next := t.TempDir()
	var opens atomic.Int32
	b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) {
		return &config.Config{VaultDir: next, DBPath: filepath.Join(t.TempDir(), "other.db")}, nil
	}, func(string) (*store.Store, error) { opens.Add(1); return nil, errors.New("must not open rebound root") }, nil)
	b.cfg = &config.Config{VaultDir: old}
	b.cfgGeneration = 1
	b.generation = 1
	b.progress[1] = &note.Note{ID: 1, Body: "old vault"}
	b.start()
	waitBoot(t, func() bool { return b.Status().Phase == "unavailable" })
	if w := bootRequest(b, "GET", "/api/note?id=1", ""); w.Code == 200 || strings.Contains(w.Body.String(), "old vault") {
		t.Fatalf("old root leaked into new generation: %d %s", w.Code, w.Body.String())
	}
	if opens.Load() != 0 {
		t.Fatal("rebound config opened storage")
	}
	b.Close()
}

func TestBootstrapRetainsBlockedOpenBudgetThroughRetryAndShutdown(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "note"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{VaultDir: root, DBPath: filepath.Join(t.TempDir(), "cache.db"), Extensions: []string{".md"}, Provisional: true}
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	var calls atomic.Int32
	b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) { return cfg, nil }, func(string) (*store.Store, error) {
		calls.Add(1)
		close(entered)
		<-release
		close(exited)
		return nil, errors.New("controlled unavailable cache")
	}, nil)
	b.start()
	<-entered
	b.mu.Lock()
	b.cancel()
	generation := b.generation
	b.mu.Unlock()
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.running })
	for range 10 {
		b.start()
	}
	b.mu.Lock()
	same := b.generation == generation
	b.mu.Unlock()
	if !same || calls.Load() != 1 {
		t.Fatal("retry multiplied pending cache opens")
	}
	if w := bootRequest(b, "GET", "/", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	b.Close()
	close(release)
	<-exited
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.openPending })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.server != nil {
		t.Fatal("late cache installed after close")
	}
}

func TestBootstrapHealthyNamedPeerSurvivesPrimaryCacheStall(t *testing.T) {
	root := t.TempDir()
	writeVaultNote(t, root, 3, "Primary", "Primary")
	cfg := &config.Config{VaultDir: root, VaultName: "primary", DBPath: filepath.Join(t.TempDir(), "cache.db"), Extensions: []string{".md"}, Provisional: true}
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) { return cfg, nil }, func(string) (*store.Store, error) {
		close(entered)
		<-release
		close(exited)
		return nil, errors.New("controlled unavailable cache")
	}, nil)
	b.start()
	<-entered
	peerRoot := t.TempDir()
	writeVaultNote(t, peerRoot, 9, "Independent", "Healthy peer remains readable")
	peerCfg := &config.Config{VaultDir: peerRoot, VaultName: "healthy", DBPath: filepath.Join(t.TempDir(), "peer.db"), Extensions: []string{".md"}}
	st, err := store.Open(peerCfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	n, err := note.ReadFileContext(context.Background(), peerCfg.NotePath(9), peerCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertNote(n); err != nil {
		t.Fatal(err)
	}
	peer := &vaultView{name: "healthy", label: "healthy", cfg: peerCfg, store: st, async: true}
	peer.progress.state.Partial = true
	peer.progress.running = true
	b.mu.Lock()
	peers := b.peers
	b.mu.Unlock()
	if peers == nil {
		t.Fatal("no independent registry")
	}
	copyCfg := *peers.cfg
	copyCfg.Vaults = map[string]string{"healthy": peerRoot}
	peers.cfg = &copyCfg
	peers.active.cfg = &copyCfg
	peers.viewsMu.Lock()
	peers.views["healthy"] = peer
	peers.viewsMu.Unlock()
	w := bootRequest(b, "GET", "/api/note?id=9&vault=healthy", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Healthy peer remains readable") {
		t.Fatalf("primary blocked peer: %d %s", w.Code, w.Body.String())
	}
	b.Close()
	close(release)
	<-exited
}

func TestBootstrapShutdownRetainsLeaseUntilAuthoritativeWriterExits(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "cache.db")}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	b := newBootstrap(context.Background(), nil, nil, nil)
	srv := newServer(cfg, st, true)
	srv.enableAsync()
	b.server = srv
	released := make(chan struct{})
	b.release = func() { close(released) }
	srv.active.mutationGate().Lock()
	b.Close() // Bounded caller wait, even while an owned writer cannot finish yet.
	select {
	case <-released:
		t.Fatal("ownership released while old writer still held its gate")
	default:
	}
	if w := httptest.NewRecorder(); srv.asyncMutation(srv.active, w, httptest.NewRequest("PUT", "/api/note?id=1", nil)) {
		t.Fatal("retired generation admitted another write")
	}
	srv.active.mutationGate().Unlock()
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not finish after writer exited")
	}
}

func TestReadOnlyWatcherNeverCreatesMissingVaultDirectories(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	srv := newServer(cfg, nil, true)
	defer srv.closeViews()
	srv.startWatch()
	for _, path := range []string{cfg.NoteDir(), cfg.JournalDir(), cfg.DataDir()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only watcher created %s: %v", path, err)
		}
	}
}
