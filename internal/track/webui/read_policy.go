package webui

// Immutable canonical-read-only policy shared by the explicit web launch mode
// and its internal test constructors. HTTP authorization is enforced separately.
import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/vaultfile"
)

type readPolicy struct {
	root               string
	configurationFiles []string
	vaults             []string
}

func newReadPolicy(root string, files []string) (*readPolicy, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || len(files) == 0 {
		return nil, errors.New("internal read policy requires an explicit absolute cache root and configuration paths")
	}
	for _, file := range files {
		if !filepath.IsAbs(file) || filepath.Clean(file) != file {
			return nil, errors.New("configuration paths must be clean and absolute")
		}
	}
	return &readPolicy{root: root, configurationFiles: append([]string(nil), files...)}, nil
}

// Each constructor/replacement captures all known roots without mutating an
// existing policy. A foreign config must never narrow the launch protection set.
func (p *readPolicy) withConfig(cfg *config.Config) *readPolicy {
	if p == nil {
		return nil
	}
	out := &readPolicy{root: p.root, configurationFiles: append([]string(nil), p.configurationFiles...), vaults: append([]string(nil), p.vaults...)}
	add := func(path string) {
		if path == "" {
			return
		}
		for _, old := range out.vaults {
			if old == path {
				return
			}
		}
		out.vaults = append(out.vaults, path)
		out.configurationFiles = append(out.configurationFiles, config.VaultConfigPath(path))
	}
	add(cfg.VaultDir)
	for _, path := range cfg.Vaults {
		add(path)
	}
	return out
}

// Always accept the original config, never one whose DBPath has been redirected.
func (p *readPolicy) cacheConfig(cfg *config.Config) (*config.Config, error) {
	if p == nil {
		return cfg, nil
	}
	protected := *cfg
	protected.Vaults = make(map[string]string, len(cfg.Vaults)+len(p.vaults))
	for name, path := range cfg.Vaults {
		protected.Vaults[name] = path
	}
	for i, path := range p.vaults {
		protected.Vaults[fmt.Sprintf("\x00protected-%d", i)] = path
	}
	path, err := config.ReadOnlyCachePath(p.root, &protected, p.configurationFiles)
	if err != nil {
		return nil, err
	}
	copy := *cfg
	copy.DBPath = path
	return &copy, nil
}
func (p *readPolicy) open(cfg *config.Config, open func(string) (*store.Store, error)) (*store.Store, error) {
	cache, err := p.cacheConfig(cfg)
	if err != nil {
		return nil, err
	}
	st, err := open(cache.DBPath)
	if err != nil {
		return nil, err
	}
	if err = p.verifyStore(cfg, st); err != nil {
		if st != nil {
			st.Close()
		}
		return nil, err
	}
	return st, nil
}
func (p *readPolicy) finalize(ctx context.Context, cfg *config.Config, ix *index.Indexer, snap *index.Snapshot) error {
	if p == nil {
		return ix.ApplyActivity(ctx, snap)
	}
	if _, err := p.cacheConfig(cfg); err != nil {
		return err
	}
	return ix.FinalizeRead(ctx, snap)
}
func (p *readPolicy) publish(cfg *config.Config, ix *index.Indexer, snap *index.Snapshot) error {
	if p != nil {
		if _, err := p.cacheConfig(cfg); err != nil {
			return err
		}
	}
	return ix.Publish(snap)
}
func (p *readPolicy) verifyStore(cfg *config.Config, st *store.Store) error {
	if p == nil {
		return nil
	}
	cache, err := p.cacheConfig(cfg)
	if err != nil {
		return err
	}
	if st == nil || st.OpenedPath() != cache.DBPath {
		return errors.New("store is not the policy-approved private cache")
	}
	return nil
}

// Called with the view's reindex lock held. Version reuse suppresses repeated
// identical publication without treating cached rows as fresh filesystem reads.
func (s *Server) refreshRead(ctx context.Context, v *vaultView) (bool, error) {
	p := s.readPolicy.withConfig(v.cfg)
	if err := p.verifyStore(v.cfg, v.store); err != nil {
		return false, err
	}
	cache, err := p.cacheConfig(v.cfg)
	if err != nil {
		return false, err
	}
	ix := index.New(cache, v.store)
	initialized, err := ix.EnsureInitializedReadContext(ctx)
	if err != nil {
		return false, err
	}
	ix.WithPrevious(v.readVersions)
	snap, err := ix.Stage(ctx, nil)
	if err != nil {
		return false, err
	}
	state, err := v.store.ReadState()
	if err != nil {
		return false, err
	}
	if len(snap.Notes) == 0 && sameReadHealth(state, snap.State) {
		revision, err := v.store.Revision()
		if err != nil {
			return false, err
		}
		if revision != snap.Revision {
			return false, index.ErrSuperseded
		}
		return initialized, nil
	}
	if err = ix.FinalizeRead(ctx, snap); err != nil {
		return false, err
	}
	if err = p.verifyStore(v.cfg, v.store); err != nil {
		return false, err
	}
	if err = p.publish(v.cfg, ix, snap); err != nil {
		return false, err
	}
	if v.readVersions == nil {
		v.readVersions = map[int64]*vaultfile.NoteRead{}
	}
	for _, n := range snap.Notes {
		if _, known := v.readVersions[n.ID]; known || len(v.readVersions) < 100000 {
			v.readVersions[n.ID] = vaultfile.VersionOnly(n.Snapshot)
		}
	}
	return true, nil
}
