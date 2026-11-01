package webui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

type StorageStatus struct {
	Phase      string `json:"phase"`
	ReadOnly   bool   `json:"read_only"`
	Reason     string `json:"reason,omitempty"`
	Verified   int    `json:"verified"`
	Home       string `json:"home_note_id,omitempty"`
	Generation uint64 `json:"generation"`
}
type Bootstrap struct {
	mu               sync.Mutex
	status           StorageStatus
	server           *Server
	placeholder      *Server
	cfg              *config.Config
	progress         map[int64]*note.Note
	bytes            int
	running          bool
	openPending      bool
	peers            *Server
	serverGeneration uint64
	generation       uint64
	cfgGeneration    uint64
	closed           bool
	cancel           context.CancelFunc
	root             context.Context
	boundary         *readBoundary
	inboxBoundary    *readBoundary
	open             func(string) (*store.Store, error)
	load             func(context.Context, string, bool) (*config.Config, error)
	lease            func(*config.Config) (func(), error)
	release          func()
}

func NewBootstrap(ctx context.Context, lease func(*config.Config) (func(), error)) *Bootstrap {
	return newBootstrap(ctx, config.LoadBounded, store.Open, lease)
}
func newBootstrap(ctx context.Context, load func(context.Context, string, bool) (*config.Config, error), open func(string) (*store.Store, error), lease func(*config.Config) (func(), error)) *Bootstrap {
	return &Bootstrap{status: StorageStatus{Phase: "initializing", ReadOnly: true, Reason: "Workspace listeners are initializing"}, root: ctx, placeholder: New(&config.Config{Provisional: true}, nil), boundary: newReadBoundary(), inboxBoundary: newReadBoundary(), progress: map[int64]*note.Note{}, load: load, open: open, lease: lease}
}

func (b *Bootstrap) start() {
	b.mu.Lock()
	if b.running || b.openPending || b.closed || b.root.Err() != nil {
		b.mu.Unlock()
		return
	}
	var held []*vaultView
	if b.server != nil {
		views := []*vaultView{b.server.active}
		b.server.viewsMu.Lock()
		for _, view := range b.server.views {
			views = append(views, view)
		}
		b.server.viewsMu.Unlock()
		for _, view := range views {
			if !view.mutationGate().TryLock() {
				for _, old := range held {
					old.mutationGate().Unlock()
				}
				b.status.Reason = "A storage mutation is still pending"
				b.mu.Unlock()
				return
			}
			held = append(held, view)
		}
		for _, view := range held {
			view.invalidateRead()
		}
	}
	oldServer, oldRelease := b.server, b.release
	b.running = true
	b.generation++
	generation := b.generation
	b.boundary.advance(generation)
	ctx, cancel := context.WithCancel(b.root)
	b.cancel = cancel
	b.status = StorageStatus{Phase: "initializing", ReadOnly: true, Reason: "Configuration and storage are being verified", Generation: generation}
	b.mu.Unlock()
	go func() {
		defer func() {
			for _, view := range held {
				view.mutationGate().Unlock()
			}
		}()
		defer cancel()
		defer func() {
			b.mu.Lock()
			if b.generation == generation {
				b.running = false
			}
			b.mu.Unlock()
		}()
		configCtx, cancelLoad := context.WithTimeout(ctx, index.StageBudget)
		cfg, err := b.load(configCtx, "", false)
		cancelLoad()
		if err != nil {
			b.fail(generation, err)
			return
		}
		b.mu.Lock()
		if b.generation != generation || ctx.Err() != nil {
			b.mu.Unlock()
			return
		}
		if b.cfg != nil && b.cfg.VaultDir != cfg.VaultDir {
			b.mu.Unlock()
			b.fail(generation, errors.New("active vault changed; restart after preserving drafts"))
			return
		}
		b.cfgGeneration = generation
		b.cfg = cfg
		b.openPending = true
		b.progress = map[int64]*note.Note{}
		b.bytes = 0
		b.mu.Unlock()
		type openResult struct {
			store      *store.Store
			release    func()
			freshLease bool
			err        error
		}
		// A separate read-only registry lets healthy peers open while the primary cache stalls.
		b.mu.Lock()
		peers := b.peers
		b.mu.Unlock()
		if peers != nil && !reflect.DeepEqual(*peers.cfg, *cfg) {
			peers.closeViews()
			peers = nil
		}
		if peers == nil {
			peers = newServer(cfg, nil, true)
			peers.enableAsync()
			b.mu.Lock()
			if b.generation == generation && !b.closed {
				b.peers = peers
			}
			b.mu.Unlock()
		}
		for _, name := range sortedVaultNames(cfg.Vaults) {
			if name != cfg.VaultName {
				peers.kickView(name)
			}
		}
		openedDB := make(chan openResult)
		go func() {
			defer func() { b.mu.Lock(); b.openPending = false; b.mu.Unlock() }()
			release := oldRelease
			freshLease := false
			var err error
			if release == nil && b.lease != nil {
				var rawRelease func()
				rawRelease, err = b.lease(cfg)
				freshLease = err == nil
				if rawRelease != nil {
					var once sync.Once
					release = func() { once.Do(rawRelease) }
				}
			}
			if ctx.Err() != nil {
				if freshLease && release != nil {
					release()
				}
				return
			}
			if err == nil {
				b.mu.Lock()
				current := b.generation == generation && !b.closed
				if current {
					b.release = release
					// Ownership now belongs to the live listener, including retries.
					// A late read-only cache open must not release the published lease.
					freshLease = false
				}
				b.mu.Unlock()
				if !current {
					if freshLease && release != nil {
						release()
					}
					return
				}
			}
			// Ownership depends on verified configuration, not a responsive cache.
			// Stop/retry can identify this server even while database opening stalls.
			var st *store.Store
			if err == nil {
				st, err = b.open(cfg.DBPath)
			}
			if ctx.Err() != nil {
				if freshLease && release != nil {
					release()
				}
				if st != nil {
					st.Close()
				}
				return
			}
			select {
			case openedDB <- openResult{st, release, freshLease, err}:
			case <-ctx.Done():
				if freshLease && release != nil {
					release()
				}
				if st != nil {
					st.Close()
				}
			}
		}()
		strictResult := make(chan *config.Config, 1)
		go func() {
			strictCtx, stop := context.WithTimeout(ctx, index.StageBudget)
			defer stop()
			verified, err := b.load(strictCtx, "", true)
			if err != nil {
				b.fail(generation, err)
				strictResult <- nil
				return
			}
			strictResult <- verified
		}()
		stageCtx, stopStage := context.WithTimeout(ctx, index.StageBudget)
		_, stageErr := index.New(cfg, nil).Stage(stageCtx, func(n *note.Note) {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.generation != generation || ctx.Err() != nil {
				return
			}
			if b.bytes+len(n.Body)+len(n.Snapshot.Metadata) > maxProgressBytes || len(b.progress) >= 1024 {
				return
			}
			copy := *n
			copy.Meta.Days = append([]string(nil), n.Meta.Days...)
			b.progress[n.ID] = &copy
			b.bytes += len(n.Body) + len(n.Snapshot.Metadata)
			b.status.Verified = len(b.progress)
			if cfg.WebHome == n.Meta.Title || cfg.WebHome == strconv.FormatInt(n.ID, 10) {
				b.status.Home = strconv.FormatInt(n.ID, 10)
			}
		})
		stopStage()
		if stageErr != nil {
			b.fail(generation, stageErr)
		}
		waitCtx, stopWait := context.WithTimeout(ctx, index.StageBudget)
		defer stopWait()
		var opened openResult
		select {
		case opened = <-openedDB:
		case <-waitCtx.Done():
			b.fail(generation, errors.New("cache initialization has not completed; confirmed files remain read-only"))
			return
		}
		if opened.err != nil {
			if opened.store != nil {
				opened.store.Close()
			}
			b.fail(generation, opened.err)
			return
		}
		currentCfg := cfg
		select {
		case verified := <-strictResult:
			if verified != nil {
				provisional := *cfg
				provisional.Provisional = false
				provisional.VaultName = verified.VaultName
				if reflect.DeepEqual(provisional, *verified) {
					currentCfg = verified
				} else {
					b.fail(generation, errors.New("configuration changed during initialization; retry to verify it"))
				}
			}
		case <-waitCtx.Done():
			b.fail(generation, errors.New("configuration verification is pending"))
		}
		if ctx.Err() != nil {
			opened.store.Close()
			if opened.freshLease && opened.release != nil {
				opened.release()
			}
			return
		}
		srv := newServer(currentCfg, opened.store, true)
		srv.enableAsync()
		_ = opened.store.SetReadState(store.ReadState{Partial: true, CheckedAt: time.Now().UnixMilli(), Issues: []store.ReadIssue{{Path: currentCfg.VaultDir, Reason: "index verification pending"}}})
		b.mu.Lock()
		if b.generation != generation {
			b.mu.Unlock()
			opened.store.Close()
			if opened.freshLease && opened.release != nil {
				opened.release()
			}
			return
		}
		srv.bindHost = b.placeholder.bindHost
		srv.appsBindHost = b.placeholder.appsBindHost
		srv.appsPort = b.placeholder.appsPort
		b.serverGeneration = generation
		b.cfg = currentCfg
		b.server = srv
		b.release = opened.release
		b.status.Phase = "partial"
		if !currentCfg.Provisional {
			b.status.Reason = "Index verification is in progress"
		}
		b.mu.Unlock()
		srv.refreshAsync(srv.active)
		go srv.startWatch()
		b.mu.Lock()
		if b.peers == peers {
			b.peers = nil
		}
		b.mu.Unlock()
		peers.closeViews()
		if oldServer != nil {
			// Retire the old generation while its write gates are still held.
			oldServer.viewsMu.Lock()
			oldServer.closed = true
			oldServer.viewsMu.Unlock()
			for _, v := range held {
				v.mutationGate().Unlock()
			}
			held = nil
			oldServer.closeViews()
			oldServer.store.Close()
		}
	}()
}
func (b *Bootstrap) fail(generation uint64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generation == generation {
		b.status.Phase = "unavailable"
		b.status.ReadOnly = true
		b.status.Reason = err.Error()
	}
}
func (b *Bootstrap) Status() StorageStatus {
	b.mu.Lock()
	status := b.status
	srv := b.server
	serverGeneration := b.serverGeneration
	b.mu.Unlock()
	if srv != nil && status.Generation == serverGeneration {
		srv.active.progress.mu.Lock()
		if home := srv.active.progress.home; home != "" {
			status.Home = home
		}
		srv.active.progress.mu.Unlock()
		state := srv.active.asyncState()
		available := srv.active.mutationGate().TryLock()
		if available {
			srv.active.mutationGate().Unlock()
		}
		if !state.Partial && !srv.cfg.Provisional && available {
			status.Phase = "ready"
			status.ReadOnly = false
			status.Reason = ""
		} else {
			status.ReadOnly = true
			if !available {
				status.Phase = "partial"
				status.Reason = "A storage mutation is pending"
			}
		}
	}
	return status
}
func (b *Bootstrap) progressResponse(w http.ResponseWriter, r *http.Request, reason string) bool {
	b.mu.Lock()
	cfg := b.cfg
	notes := make([]*note.Note, 0, len(b.progress))
	for _, n := range b.progress {
		notes = append(notes, n)
	}
	status := b.status
	peers := b.peers
	generation := b.generation
	srv := b.server
	serverCurrent := b.serverGeneration == b.generation
	cfgValid := b.cfgGeneration == b.generation
	b.mu.Unlock()
	if srv != nil && serverCurrent {
		latest := map[int64]*note.Note{}
		for _, n := range notes {
			latest[n.ID] = n
		}
		srv.active.progress.mu.Lock()
		for id, n := range srv.active.progress.notes {
			latest[id] = n
		}
		srv.active.progress.mu.Unlock()
		notes = notes[:0]
		for _, n := range latest {
			notes = append(notes, n)
		}
	}
	if cfg == nil || !cfgValid {
		return false
	}
	if name := r.URL.Query().Get("vault"); name != "" && name != cfg.VaultName {
		return false
	}
	valid := func() bool { b.mu.Lock(); defer b.mu.Unlock(); return generation == b.generation && !b.closed }
	state := store.ReadState{Partial: true, CheckedAt: 0, Issues: []store.ReadIssue{{Path: cfg.VaultDir, Reason: reason}}}
	switch r.URL.Path {
	case "/api/note":
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if id > 0 {
			found := false
			for _, n := range notes {
				if n.ID == id {
					found = true
					break
				}
			}
			if !found {
				if n, err := note.ReadFileContext(r.Context(), cfg.NotePath(id), cfg); err == nil {
					notes = append(notes, n)
				}
			}
		}
		for _, n := range notes {
			if n.ID == id {
				if !valid() {
					writeError(w, errors.New("storage generation changed; retry this read"), 503)
					return true
				}
				writeJSON(w, map[string]any{"note": map[string]any{"note_id": n.ID, "vault": "", "file_kind": n.Kind, "title": n.Meta.Title, "body": n.Body, "tags": n.Meta.Tags, "path": n.Path, "copy_path": cfg.DisplayPathForKind(n.Kind, n.ID), "etag": "", "read_only": true}, "backlinks": []any{}, "read_state": state})
				return true
			}
		}
	case "/api/search", "/api/notes":
		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		hits := []store.SearchResult{}
		for _, n := range notes {
			if query != "" && !strings.Contains(strings.ToLower(n.Meta.Title), query) && !strings.Contains(strings.ToLower(n.Body), query) {
				continue
			}
			hits = append(hits, store.SearchResult{NoteID: n.ID, FileKind: n.Kind, Title: n.Meta.Title, Path: n.Path, Tags: n.Meta.Tags})
		}
		gaps := []vaultInfo{}
		if r.URL.Path == "/api/search" && r.URL.Query().Get("vault") == "" && peers != nil {
			views, pending := peers.readViews()
			pages, unavailable, _ := peers.searchAcrossBounded(views, query, parseLimit(r.URL.Query().Get("limit"), 50))
			hits = append(hits, pages...)
			gaps = append(pending, unavailable...)
		}
		if !valid() {
			writeError(w, errors.New("storage generation changed; retry this read"), 503)
			return true
		}
		slices.SortFunc(hits, func(a, b store.SearchResult) int { return strings.Compare(a.Title, b.Title) })
		key := "results"
		if r.URL.Path == "/api/notes" {
			key = "notes"
		}
		writeJSON(w, map[string]any{key: hits, "read_state": state, "partial": []any{map[string]any{"name": cfg.VaultName, "read_state": state}}, "unavailable": gaps, "storage_state": status.Phase})
		return true
	}
	return false
}
func (b *Bootstrap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// External Markdown is a loopback capability inbox, independent of vault
	// configuration/index readiness. Keep one inbox across backend generations.
	if r.URL.Path == "/api/markdown" || r.URL.Path == "/api/markdown/consume" {
		b.inboxBoundary.serve(w, r, b.placeholder.Handler(), func(http.ResponseWriter, string) bool { return false })
		return
	}
	if r.URL.Path == "/api/storage" {
		if r.Method == http.MethodPost {
			b.start()
		}
		writeJSON(w, b.Status())
		return
	}
	if r.URL.Path != "/api/events" && (r.Method == http.MethodGet || r.Method == http.MethodHead || readOnlyPost(r.URL.Path)) {
		ctx, cancel := context.WithTimeout(r.Context(), readResponseBudget)
		defer cancel()
		r = r.WithContext(ctx)
	}
	b.mu.Lock()
	srv := b.server
	if b.serverGeneration != b.generation {
		srv = nil
	}
	if srv == nil && b.cfgGeneration == b.generation && b.peers != nil {
		name := r.URL.Query().Get("vault")
		if name != "" && name != b.peers.active.name {
			srv = b.peers
		}
	}
	placeholder := b.placeholder
	cfg := b.cfg
	b.mu.Unlock()
	if r.URL.Path == "/api/events" && srv != nil {
		srv.Handler().ServeHTTP(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if srv != nil {
			b.boundary.serve(w, r, srv.Handler(), func(w http.ResponseWriter, _ string) bool { placeholder.Handler().ServeHTTP(w, r); return true })
		} else {
			placeholder.Handler().ServeHTTP(w, r)
		}
		return
	}
	if srv == nil {
		if r.URL.Path == "/api/vaults" {
			active := map[string]any{"name": "", "path": ""}
			vaults := []vaultEntry{}
			if cfg != nil {
				active = map[string]any{"name": cfg.VaultName, "path": cfg.VaultDirDisplay}
				vaults = append(vaults, vaultEntry{Name: cfg.VaultName, Path: cfg.VaultDirDisplay, Active: true})
			}
			writeJSON(w, map[string]any{"active": active, "vaults": vaults, "unavailable": []vaultInfo{{Error: b.Status().Reason}}, "storage_state": b.Status().Phase})
			return
		}
		if r.Method == http.MethodGet && b.progressResponse(w, r, "initialization is incomplete; showing confirmed files read-only") {
			return
		}
		writeError(w, errors.New(b.Status().Reason), http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet && b.statusFor(r).ReadOnly && b.progressResponse(w, r, "storage is not fully verified; showing confirmed information read-only") {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !readOnlyPost(r.URL.Path) {
		if b.statusFor(r).ReadOnly {
			writeError(w, errors.New("storage is not verified; writes are disabled"), http.StatusConflict)
			return
		}
		srv.Handler().ServeHTTP(w, r)
		return
	}
	b.boundary.serve(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := &limitedResponse{ResponseRecorder: httptest.NewRecorder()}
		srv.Handler().ServeHTTP(response, r)
		status := b.statusFor(r)
		if status.ReadOnly && response.Code == 200 && response.err == nil {
			var document map[string]any
			if decodeDocument(response.Body.Bytes(), &document) == nil {
				if n, ok := document["note"].(map[string]any); ok {
					n["read_only"] = true
				}
				if _, exists := document["read_state"]; !exists {
					state := store.ReadState{Partial: true, Issues: []store.ReadIssue{{Reason: status.Reason}}}
					document["read_state"] = state
					if _, exists := document["results"]; exists {
						document["partial"] = []any{map[string]any{"name": "", "read_state": state}}
					}
				}

				writeJSON(w, document)
				return
			}
		}
		for name, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	}), func(w http.ResponseWriter, reason string) bool { return b.progressResponse(w, r, reason) })
}
func (b *Bootstrap) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.generation++
	b.boundary.advance(b.generation)
	if b.cancel != nil {
		b.cancel()
	}
	srv := b.server
	peers := b.peers
	release := b.release
	b.mu.Unlock()
	if srv != nil {
		srv.active.invalidateRead()
	}
	// Cleanup may itself be blocked by cache storage. Keep one owned cleanup task,
	// but never make the listener/shutdown control path wait without a deadline.
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if peers != nil {
			peers.closeViews()
		}
		if srv != nil {
			srv.closeViews()
			srv.store.Close()
		}
		if release != nil {
			release()
		}
	}()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-finished:
	case <-timer.C:
	}
}

// ServeBootstrapWithLifecycle binds the owned listeners before touching vault I/O.
func ServeBootstrapWithLifecycle(ctx context.Context, b *Bootstrap, addr string, onReady func()) error {
	defer b.Close()
	serveCtx, cancelRequests := context.WithCancel(ctx)
	defer cancelRequests()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	apps, appHost, appPort, err := listenStaticApps(addr, listener)
	if err != nil {
		listener.Close()
		return err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		listener.Close()
		apps.Close()
		return err
	}
	b.mu.Lock()
	b.placeholder.bindHost = host
	b.placeholder.appsBindHost = appHost
	b.placeholder.appsPort = appPort
	if b.server != nil {
		b.server.bindHost = host
		b.server.appsBindHost = appHost
		b.server.appsPort = appPort
	}
	b.mu.Unlock()
	main := &http.Server{Handler: b.placeholder.guard(b), BaseContext: func(net.Listener) context.Context { return serveCtx }}
	secondary := &http.Server{BaseContext: func(net.Listener) context.Context { return serveCtx }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		srv := b.server
		b.mu.Unlock()
		if srv == nil {
			writeError(w, errors.New("storage is initializing"), 503)
			return
		}
		srv.staticAppsHandler().ServeHTTP(w, r)
	})}
	errs := make(chan error, 2)
	go func() { errs <- main.Serve(listener) }()
	go func() { errs <- secondary.Serve(apps) }()
	b.start()
	if onReady != nil {
		onReady()
	}
	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	// End owned SSE/read requests before graceful drain. Otherwise a healthy
	// open event stream consumes the entire shutdown timeout on every quit.
	cancelRequests()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	_ = main.Shutdown(shutdownCtx)
	_ = secondary.Shutdown(shutdownCtx)
	cancelShutdown()
	_ = main.Close()
	_ = secondary.Close()
	b.Close()
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve workspace: %w", err)
	}
	return nil
}

func readOnlyPost(path string) bool {
	return path == "/api/render" || path == "/api/viewspec" || path == "/api/markdown" || path == "/api/markdown/consume"
}

func (b *Bootstrap) statusFor(r *http.Request) StorageStatus {
	status := b.Status()
	name := r.URL.Query().Get("vault")
	b.mu.Lock()
	srv := b.server
	current := b.serverGeneration == b.generation
	b.mu.Unlock()
	if name == "" || srv == nil || !current || name == srv.active.name {
		return status
	}
	view, ok := srv.cachedView(name)
	if !ok {
		return status
	}
	state := view.asyncState()
	available := view.mutationGate().TryLock()
	if available {
		view.mutationGate().Unlock()
	}
	if !view.cfg.Provisional && !state.Partial && available {
		status.ReadOnly = false
		status.Phase = "ready"
		status.Reason = ""
	} else {
		status.ReadOnly = true
		status.Phase = "partial"
		status.Reason = "Selected vault is not fully verified"
	}
	return status
}
