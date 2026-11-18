package webui

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

// NewCanonicalReadOnlyBootstrap selects an immutable process policy. It does not
// create the caller's private root or read canonical vault contents. Subsequent
// configuration/peer loading must pass complete cache separation checks before
// opening the cache or acquiring a PID lease. The lease receives the derived
// cache config, not the ordinary index location.
func NewCanonicalReadOnlyBootstrap(ctx context.Context, root string, lease func(*config.Config) (func(), error)) (*Bootstrap, error) {
	if err := config.ValidateReadOnlyCacheRoot(root); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(config.ConfigPath())
	if err != nil {
		return nil, err
	}
	policy, err := newReadPolicy(root, []string{path})
	if err != nil {
		return nil, err
	}
	return newBootstrapWithReadPolicy(ctx, config.LoadBounded, store.Open, lease, policy), nil
}

// This handshake proves protocol support and selected mode only. Listener
// ownership and current storage readiness must be verified separately.
func writeCapabilities(w http.ResponseWriter, r *http.Request, canonicalReadOnly bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, struct {
		Protocol          string `json:"protocol"`
		Version           int    `json:"version"`
		CanonicalReadOnly bool   `json:"canonical_read_only"`
	}{"track-canonical-read-only", 1, canonicalReadOnly})
}
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	s.readPolicy.capabilities(w, r)
}
