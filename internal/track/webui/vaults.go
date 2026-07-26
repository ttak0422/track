package webui

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
)

// vaultView is one vault the workspace reads and writes: the registry name that labels it on the
// wire, its config (every filesystem path derives from it), and its index handle. Note ids are
// vault-local and journal ids collide across vaults by construction, so a view — never a bare id —
// is what decides which files a request touches.
type vaultView struct {
	// name is the vault's registry identity, used to address it (?vault=) and to list it.
	name string
	// label is what the vault is called on the wire: empty for the launch vault, the registry name
	// for every other. An unlabelled id therefore means "the vault you are already in", exactly as
	// an unqualified [[title]] does — which is what keeps a single-vault workspace's URLs, stored
	// tabs, and responses byte-identical to before it could serve several.
	label string
	cfg   *config.Config
	store *store.Store
	// reindexMu serializes this vault's reindexes and lastStale throttles its read-path freshness
	// scan. Both are per vault: each vault has its own index, so one vault's rebuild must not block
	// or throttle another's.
	reindexMu sync.Mutex
	lastStale time.Time
}

// refresh reconciles one vault's index with the notes on disk before a read, so the workspace
// reflects edits made by another process or an external sync even when no filesystem event arrived.
// Only the active vault is watched, so for every other vault this scan is the only freshness signal.
func (s *Server) refresh(v *vaultView) {
	v.reindexMu.Lock()
	defer v.reindexMu.Unlock()
	if time.Since(v.lastStale) < staleCheckInterval {
		return
	}
	v.lastStale = time.Now()
	changed, err := index.New(v.cfg, v.store).RefreshIfStale()
	if err != nil {
		fmt.Fprintf(os.Stderr, "track web: refresh-if-stale failed for vault %q: %v\n", v.name, err)
		return
	}
	if changed {
		s.events.broadcastChange(v.name)
	}
}

// write runs fn while holding this vault's reindex lock, so a note's rewrite and the reindex that
// follows cannot interleave with a read-path refresh. Without it, Full could read a file mid-write
// and still stamp it with the post-write mtime — after which staleness detection sees nothing to do
// and the torn content stays indexed until the note is edited again.
func (v *vaultView) write(fn func() error) error {
	v.reindexMu.Lock()
	defer v.reindexMu.Unlock()
	return fn()
}

// noteByID finds a note in this vault's index. It is also the gate that keeps a foreign id from
// acting on a same-numbered note here: an id is only ever looked up in the vault the request named.
func (v *vaultView) noteByID(id int64) (store.SearchResult, error) {
	notes, err := v.store.SearchRefs()
	if err != nil {
		return store.SearchResult{}, err
	}
	for _, n := range notes {
		if n.NoteID == id {
			return n, nil
		}
	}
	return store.SearchResult{}, fmt.Errorf("note %d is not indexed", id)
}

// activeName is the wire label for the vault track web was launched in: its registry name when it
// is registered, otherwise "" — the same convention the CLI's cross-vault output uses for an
// unregistered active vault.
func activeName(cfg *config.Config) string {
	for _, name := range sortedVaultNames(cfg.Vaults) {
		if canonical, err := config.CanonicalPath(cfg.Vaults[name]); err == nil && canonical == cfg.VaultDir {
			return name
		}
	}
	return ""
}

func sortedVaultNames(vaults map[string]string) []string {
	names := make([]string, 0, len(vaults))
	for name := range vaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// vaultHandler is a request handler that knows which vault it acts on. Routing every id-taking
// endpoint through withVault is what keeps a foreign id from writing into the active vault: the
// view is resolved once, at the seam, instead of each handler reaching for the server's own config.
type vaultHandler func(*vaultView, http.ResponseWriter, *http.Request)

// withVault resolves the addressed vault before the handler runs. An unknown or unreachable vault
// fails the request rather than falling back to the active vault — a typo must never land a write
// somewhere else (the rule ADR 0051 set for --vault).
func (s *Server) withVault(fn vaultHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.requestVault(r)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		fn(v, w, r)
	}
}

// requestVault picks the vault a request addresses from ?vault=<registry name>; an absent or empty
// value means the vault the workspace was launched in.
func (s *Server) requestVault(r *http.Request) (*vaultView, error) {
	return s.viewByName(strings.TrimSpace(r.URL.Query().Get("vault")))
}

// viewByName returns the view for a registry name, opening its config and index on first use. The
// empty name is the active vault. Views are cached for the server's lifetime because a vault's
// config and index handle are process-scoped, not request-scoped.
func (s *Server) viewByName(name string) (*vaultView, error) {
	if name == "" || name == s.active.name {
		return s.active, nil
	}
	path, ok := s.active.cfg.Vaults[name]
	if !ok {
		return nil, fmt.Errorf("vault %q is not registered", name)
	}
	if v, ok := s.cachedView(name); ok {
		return v, nil
	}
	// Opening reads config files, stats the vault directory, and opens its index — all filesystem
	// work, and on a vault that lives on an unreachable mount it blocks for as long as that mount
	// takes to fail. None of it happens under the lock: a dead vault would otherwise stall every
	// request for the healthy ones, including cache hits that need no I/O at all.
	cfg, err := config.LoadAt(path)
	if err != nil {
		return nil, err
	}
	// The registry gives a vault exactly one name (config.resolveVaults refuses a second), so the
	// only way two names reach one vault is the launch vault being registered — it is addressable
	// both as the default and by its name, and must be the same view either way. Two views would
	// mean two labels for the same notes, two ids, and two reindex locks over one directory.
	if cfg.VaultDir == s.active.cfg.VaultDir {
		return s.adopt(name, s.active), nil
	}
	// A registered vault that is merely unmounted must be reported, not indexed: laying down an
	// index for an unreachable vault would record it as empty.
	if _, err := os.Stat(cfg.VaultDir); err != nil {
		return nil, fmt.Errorf("vault %q is unavailable: %v", name, err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open vault %q index: %w", name, err)
	}

	s.viewsMu.Lock()
	defer s.viewsMu.Unlock()
	// Another request may have opened this vault while this one was doing its filesystem work; keep
	// the view that got there first and discard this one's handle rather than leaking it.
	if existing, ok := s.views[name]; ok {
		st.Close()
		return existing, nil
	}
	v := &vaultView{name: name, label: name, cfg: cfg, store: st}
	s.views[name] = v
	return v, nil
}

func (s *Server) cachedView(name string) (*vaultView, bool) {
	s.viewsMu.Lock()
	defer s.viewsMu.Unlock()
	v, ok := s.views[name]
	return v, ok
}

// adopt records that a registry name reaches an already-open view, so the next request for that
// name is a cache hit rather than another round of filesystem work.
func (s *Server) adopt(name string, v *vaultView) *vaultView {
	s.viewsMu.Lock()
	defer s.viewsMu.Unlock()
	s.views[name] = v
	return v
}

// viewByPath returns the view whose vault directory is path, for callers that know a vault by where
// it lives rather than by name — the Neovim plugin reports the buffer's vault that way. An
// unregistered path that is not the active vault is refused: the workspace serves the vault it was
// launched in plus the registry, and nothing else.
func (s *Server) viewByPath(path string) (*vaultView, error) {
	canonical, err := config.CanonicalPath(path)
	if err != nil {
		return nil, fmt.Errorf("resolve vault path %q: %w", path, err)
	}
	if canonical == s.active.cfg.VaultDir {
		return s.active, nil
	}
	for _, name := range sortedVaultNames(s.active.cfg.Vaults) {
		registered, err := config.CanonicalPath(s.active.cfg.Vaults[name])
		if err != nil || registered != canonical {
			continue
		}
		return s.viewByName(name)
	}
	return nil, fmt.Errorf("vault %s is not served by this workspace", path)
}

// vaultInfo is one entry of the served vault list: the wire name, the path to show, whether it is
// the launch vault, and whether it could be opened. An unavailable vault stays listed with its
// error so the workspace can show the gap instead of silently omitting the vault.
type vaultInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Active    bool   `json:"active"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// handleVaults lists every vault this workspace serves, so the frontend can label notes by vault
// and offer the others as search and navigation targets.
func (s *Server) handleVaults(w http.ResponseWriter, r *http.Request) {
	active := s.active
	out := []vaultInfo{{Name: active.name, Path: active.cfg.VaultDirDisplay, Active: true, Available: true}}
	for _, name := range sortedVaultNames(active.cfg.Vaults) {
		if name == active.name {
			continue
		}
		info := vaultInfo{Name: name, Path: active.cfg.Vaults[name]}
		if v, err := s.viewByName(name); err != nil {
			info.Error = err.Error()
		} else {
			info.Available = true
			info.Path = v.cfg.VaultDirDisplay
		}
		out = append(out, info)
	}
	writeJSON(w, map[string]any{"active": active.name, "vaults": out})
}

// servedViews returns every vault the workspace can currently read, active first, skipping the ones
// that cannot be opened. Cross-vault reads use it; each caller decides how to report the gaps.
func (s *Server) servedViews() (views []*vaultView, unavailable []vaultInfo) {
	views = append(views, s.active)
	// One vault, one view — a vault registered under two names would otherwise be read twice, and a
	// federated query would attach its index twice and return each of its notes twice.
	seen := map[*vaultView]bool{s.active: true}
	for _, name := range sortedVaultNames(s.active.cfg.Vaults) {
		v, err := s.viewByName(name)
		// A name that reaches a vault already in the set adds nothing — including a name for the
		// launch vault, which is always served. Skipping before the reachability check also keeps
		// the workspace from warning that the vault whose results are on screen is unavailable.
		if err == nil && seen[v] {
			continue
		}
		if err == nil {
			// A view is cached for the server's lifetime, but the vault behind it can go away while
			// the workspace runs — an unmounted drive, a cloud folder that stopped syncing. Its index
			// lives in the cache directory and would keep answering, so re-check that the vault is
			// still there and report it as a gap rather than serving notes from a vault that is gone.
			if _, statErr := os.Stat(v.cfg.VaultDir); statErr != nil {
				err = fmt.Errorf("vault %q is unavailable: %v", name, statErr)
			}
		}
		if err != nil {
			unavailable = append(unavailable, vaultInfo{Name: name, Path: s.active.cfg.Vaults[name], Error: err.Error()})
			continue
		}
		seen[v] = true
		views = append(views, v)
	}
	return views, unavailable
}

// closeViews releases the index handles opened for other vaults. The active vault's store belongs
// to the caller that handed it to New.
func (s *Server) closeViews() {
	s.viewsMu.Lock()
	defer s.viewsMu.Unlock()
	for name, v := range s.views {
		// The launch vault's store belongs to the caller that handed it to New; only the vaults this
		// server opened are closed here.
		if v != s.active {
			v.store.Close()
		}
		delete(s.views, name)
	}
}
