package webui

// Internal-only startup binding. No CLI/native admission caller. Verification
// observes filesystem inputs synchronously; it is not an atomic sandbox or a
// readiness proof, and does not promise a hard HTTP latency bound. Admitted
// private-cache/lease work may finish after invalidation; finite responses are
// revalidated before publication, and cleanup remains permitted.
import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

type manifestBinding struct {
	mu       sync.Mutex
	manifest config.ReadOnlyLaunchManifest
	nonce    string
	invalid  error
}

func newManifestBoundBootstrap(ctx context.Context, selector config.LaunchSelector, root, revision, nonce string, lease func(*config.Config) (func(), error)) (*Bootstrap, error) {
	if len(nonce) < 32 || len(nonce) > 128 {
		return nil, errors.New("invalid process correlation nonce")
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return nil, errors.New("invalid process correlation nonce")
	}
	m, err := config.VerifyReadOnlyLaunchManifest(ctx, selector, root, revision)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(m.Configurations))
	for _, f := range m.Configurations {
		files = append(files, f.Path)
	}
	p, err := newReadPolicy(root, files)
	if err != nil {
		return nil, err
	}
	p.binding = &manifestBinding{manifest: m, nonce: nonce}
	load := func(ctx context.Context, path string, strict bool) (*config.Config, error) {
		if err := p.validateBinding(ctx); err != nil {
			return nil, err
		}
		if path == "" {
			path = m.Active.DisplayPath
		}
		cfg, err := config.LoadBounded(ctx, path, strict)
		if err != nil {
			return nil, err
		}
		if err = p.binding.checkConfig(ctx, cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	boundLease := lease
	if lease != nil {
		boundLease = func(cfg *config.Config) (func(), error) {
			if err := p.validateBinding(ctx); err != nil {
				return nil, err
			}
			// Lease is a caller callback and may reenter the handler. Never hold the
			// binding mutex across it. Work admitted here may finish after invalidation;
			// the subsequent store-open check rejects publication and Close releases it.
			return lease(cfg)
		}
	}

	return newBootstrapWithReadPolicy(ctx, load, store.Open, boundLease, p), nil
}
func (p *readPolicy) validateBinding(ctx context.Context) error {
	if p == nil || p.binding == nil {
		return nil
	}
	return p.binding.verify(ctx)
}
func (b *manifestBinding) verify(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.invalid != nil {
		return b.invalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := config.VerifyReadOnlyLaunchManifest(ctx, b.manifest.Selector, b.manifest.PrivateRoot, b.manifest.Revision)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		b.invalid = fmt.Errorf("%w: %v", config.ErrLaunchManifestChanged, err)
		return b.invalid
	}
	return err
}
func (b *manifestBinding) reject() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.invalid == nil {
		b.invalid = fmt.Errorf("%w: configuration differs from bound launch", config.ErrLaunchManifestChanged)
	}
	return b.invalid
}
func (b *manifestBinding) checkConfig(ctx context.Context, cfg *config.Config) error {
	if err := b.verify(ctx); err != nil {
		return err
	}
	if cfg == nil {
		return b.reject()
	}
	path, err := config.CanonicalPath(cfg.VaultDir)
	if err != nil {
		return b.reject()
	}
	found := false
	declared := map[string]string{}
	for _, v := range b.manifest.Peers {
		if v.Name != "" {
			declared[v.Name] = v.CanonicalPath
		}
		if v.Name == cfg.VaultName && v.CanonicalPath == path && v.OrdinaryDB == cfg.DBPath {
			found = true
		}
	}
	if !found || len(declared) != len(cfg.Vaults) {
		return b.reject()
	}
	for name, raw := range cfg.Vaults {
		canonical, e := config.CanonicalPath(raw)
		expected, ok := declared[name]
		if e != nil || !ok || canonical != expected {
			return b.reject()
		}
	}
	return nil
}
func (p *readPolicy) capabilities(w http.ResponseWriter, r *http.Request) {
	if p == nil || p.binding == nil {
		writeCapabilities(w, r, p != nil)
		return
	}
	if err := p.validateBinding(r.Context()); err != nil {
		writeError(w, err, http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, struct {
		Protocol          string `json:"protocol"`
		Version           int    `json:"version"`
		CanonicalReadOnly bool   `json:"canonical_read_only"`
		ManifestRevision  string `json:"manifest_revision"`
		ProcessNonce      string `json:"process_nonce"`
	}{"track-canonical-read-only", 2, true, p.binding.manifest.Revision, p.binding.nonce})
}

// Bound instances do not support long-lived streams. Buffer finite responses
// through the existing byte limit, revalidate after the handler (including its
// cached fallback), and serialize publication against known invalidation.
type manifestResponseKey struct{}

func (p *readPolicy) boundResponse(next http.Handler) http.Handler {
	if p == nil || p.binding == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(manifestResponseKey{}) == p.binding {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/events" {
			writeError(w, errors.New("bound startup contract does not support event streams"), http.StatusForbidden)
			return
		}
		if err := p.validateBinding(r.Context()); err != nil {
			writeError(w, err, http.StatusServiceUnavailable)
			return
		}
		response := &limitedResponse{ResponseRecorder: httptest.NewRecorder()}
		next.ServeHTTP(response, r.WithContext(context.WithValue(r.Context(), manifestResponseKey{}, p.binding)))
		if err := p.validateBinding(r.Context()); err != nil {
			writeError(w, err, http.StatusServiceUnavailable)
			return
		}
		if response.err != nil {
			writeError(w, response.err, http.StatusServiceUnavailable)
			return
		}
		p.binding.mu.Lock()
		defer p.binding.mu.Unlock()
		if p.binding.invalid != nil {
			writeError(w, p.binding.invalid, http.StatusServiceUnavailable)
			return
		}
		for name, values := range response.Header() {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	})
}
