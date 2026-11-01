package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	var claimed, released atomic.Int32
	b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) { return cfg, nil }, func(string) (*store.Store, error) {
		calls.Add(1)
		close(entered)
		<-release
		close(exited)
		return nil, errors.New("controlled unavailable cache")
	}, func(*config.Config) (func(), error) { claimed.Add(1); return func() { released.Add(1) }, nil })
	b.start()
	<-entered
	if claimed.Load() != 1 {
		t.Fatal("ownership waited for blocked cache")
	}
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
	if released.Load() != 1 {
		t.Fatal("ownership not released after owned read-only shutdown")
	}
	close(release)
	<-exited
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.openPending })
	b.mu.Lock()
	defer b.mu.Unlock()
	if released.Load() != 1 {
		t.Fatal("late cleanup released ownership twice")
	}
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

func TestBootstrapMarkdownInboxIsIndependentAndSurvivesBackendHandoff(t *testing.T) {
	b := newBootstrap(context.Background(), nil, nil, nil)
	if !b.boundary.admit("") || !b.boundary.admit("") {
		t.Fatal("could not reserve controlled blocked vault reads")
	}
	defer b.boundary.release("")
	defer b.boundary.release("")
	defer b.Close()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://127.0.0.1:8765"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		b.placeholder.guard(b).ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/markdown", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "track-markdown-v1") {
		t.Fatalf("independent probe blocked: %d %s", w.Code, w.Body.String())
	}
	body := `{"active":"note.md","files":[{"path":"note.md","data":"IyBIZWxsbw=="}]}`
	w := call("POST", "/api/markdown", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var document struct {
		Route string `json:"route"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{VaultDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "index.db"), Provisional: true}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(cfg, st, true)
	srv.enableAsync()
	b.mu.Lock()
	b.server = srv
	b.serverGeneration = b.generation
	b.mu.Unlock()
	consume := `{"token":"` + strings.TrimPrefix(document.Route, "/markdown#handoff=") + `"}`
	if w := call("POST", "/api/markdown/consume", consume); w.Code != 200 || strings.TrimSpace(w.Body.String()) != body {
		t.Fatalf("handoff lost or modified snapshot: %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", "/api/markdown/consume", consume); w.Code != 410 {
		t.Fatal("capability replay accepted", w.Code)
	}
}

func TestMarkdownConsumeSurvivesStorageRetryAfterTokenRemoval(t *testing.T) {
	b := newBootstrap(context.Background(), func(ctx context.Context, _ string, _ bool) (*config.Config, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	defer b.Close()
	body := []byte(`{"active":"note.md","files":[{"path":"note.md","data":"IyBIZWxsbw=="}]}`)
	token := strings.Repeat("a", 64)
	b.placeholder.markdown.grants = map[string]markdownGrant{token: {body: body, expires: time.Now().Add(time.Minute)}}
	consumed := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "http://127.0.0.1:8765/api/markdown/consume", strings.NewReader(`{"token":"`+token+`"}`))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		b.inboxBoundary.serve(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b.placeholder.Handler().ServeHTTP(w, r)
			close(consumed)
			<-release
		}), func(http.ResponseWriter, string) bool { return false })
		finished <- w
	}()
	<-consumed
	b.placeholder.markdown.mu.Lock()
	remaining := len(b.placeholder.markdown.grants)
	b.placeholder.markdown.mu.Unlock()
	if remaining != 0 {
		t.Fatal("fixture token was not consumed")
	}
	b.start() // Advances the storage generation while the successful consume is paused.
	close(release)
	w := <-finished
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != string(body) {
		t.Fatalf("storage retry discarded consumed capability: %d %s", w.Code, w.Body.String())
	}
}

func TestBootstrapLateOpenRetainsPublishedOwnershipForRetry(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "note"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{VaultDir: root, DBPath: filepath.Join(t.TempDir(), "cache.db"), Extensions: []string{".md"}}
	entered := make(chan struct{})
	late := make(chan struct{})
	var opens, claims, releases atomic.Int32
	var owned atomic.Bool
	b := newBootstrap(context.Background(), func(_ context.Context, _ string, strict bool) (*config.Config, error) {
		copy := *cfg
		copy.Provisional = !strict
		return &copy, nil
	}, func(path string) (*store.Store, error) {
		if opens.Add(1) == 1 {
			close(entered)
			<-late
			return nil, errors.New("late cache error")
		}
		return store.Open(path)
	}, func(*config.Config) (func(), error) {
		claims.Add(1)
		owned.Store(true)
		return func() { releases.Add(1); owned.Store(false) }, nil
	})
	b.start()
	<-entered
	b.mu.Lock()
	b.cancel()
	b.mu.Unlock()
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.running })
	close(late)
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.openPending })
	if !owned.Load() || claims.Load() != 1 || releases.Load() != 0 {
		t.Fatal("late cache cleanup dropped live listener ownership")
	}
	b.start()
	waitBoot(t, func() bool { return b.Status().Phase == "ready" })
	if !owned.Load() || claims.Load() != 1 || opens.Load() != 2 {
		t.Fatal("retry reused released ownership", claims.Load(), releases.Load(), opens.Load())
	}
	b.Close()
	if owned.Load() || releases.Load() != 1 {
		t.Fatal("ownership was not released exactly once at final close")
	}
}

func TestMutationInvalidatesStartupProgressWithoutHidingCurrentCache(t *testing.T) {
	vault := t.TempDir()
	writeVaultNote(t, vault, 1, "One", "old startup body")
	cfg := &config.Config{VaultDir: vault, DBPath: filepath.Join(t.TempDir(), "cache.db"), Extensions: []string{".md"}}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	original, err := note.ReadFileContext(context.Background(), cfg.NotePath(1), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b := newBootstrap(context.Background(), nil, nil, nil)
	defer b.Close()
	srv := newServer(cfg, st, true)
	srv.enableAsync()
	b.cfg = cfg
	b.server = srv
	b.progress[1] = original
	srv.active.progress.state.Partial = true
	srv.active.invalidateRead() // The same boundary every authoritative mutation crosses before writing.
	if err := os.WriteFile(cfg.NotePath(1), []byte("new saved body"), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := note.ReadFileContext(context.Background(), cfg.NotePath(1), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertNote(current); err != nil {
		t.Fatal(err)
	}
	srv.active.progress.running = true
	w := bootRequest(b, "GET", "/api/note?id=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "new saved body") || strings.Contains(w.Body.String(), "old startup body") {
		t.Fatalf("stale startup snapshot returned after mutation: %d %s", w.Code, w.Body.String())
	}
	w = bootRequest(b, "GET", "/api/search?q=One", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"note_id":1`) {
		t.Fatalf("current cache hidden by invalid initial snapshot: %d %s", w.Code, w.Body.String())
	}
}

func TestCloseWaitsForInFlightOwnershipCleanupWithoutOpeningCache(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "note"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{VaultDir: root, DBPath: filepath.Join(t.TempDir(), "cache.db"), Extensions: []string{".md"}}
	entered := make(chan struct{})
	finish := make(chan struct{})
	closed := make(chan struct{})
	var owned atomic.Bool
	var releases, opens atomic.Int32
	b := newBootstrap(context.Background(), func(context.Context, string, bool) (*config.Config, error) { return cfg, nil }, func(string) (*store.Store, error) {
		opens.Add(1)
		return nil, errors.New("must not open after shutdown")
	}, func(*config.Config) (func(), error) {
		owned.Store(true)
		close(entered)
		<-finish
		return func() { owned.Store(false); releases.Add(1) }, nil
	})
	b.start()
	<-entered
	go func() { b.Close(); close(closed) }()
	waitBoot(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.closed })
	select {
	case <-closed:
		t.Fatal("shutdown escaped acquisition cleanup")
	default:
	}
	close(finish)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("ownership cleanup did not finish")
	}
	if owned.Load() || releases.Load() != 1 || opens.Load() != 0 {
		t.Fatal("late ownership side effects survived close", owned.Load(), releases.Load(), opens.Load())
	}
}
