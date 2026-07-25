// Package vaultref resolves cross-vault [[vault:title]] references against the registered vaults'
// index databases (multi-vault phase 3). Resolution is read-oriented: it opens other vaults'
// stores to look up titles and inbound edges, but never scans or repairs them — each vault's own
// processes keep its index fresh. An unreachable vault is reported as unavailable, never silently
// dropped, so a missing backlink is distinguishable from a missing vault.
package vaultref

import (
	"fmt"
	"os"
	"sort"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/store"
)

// Resolved is a cross-vault reference resolved to its target note: the (vault, id) identity plus
// the target's path inside its own vault.
type Resolved struct {
	Vault    string `json:"vault"`
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
	Path     string `json:"path"`
}

// ExternalRef is one inbound cross-vault backlink: a note in Vault whose body references the
// queried note as [[<queried vault>:<title>]].
type ExternalRef struct {
	Vault    string `json:"vault"`
	NoteID   int64  `json:"note_id"`
	FileKind string `json:"file_kind"`
	Title    string `json:"title"`
	Path     string `json:"path"`
}

// Unavailable reports a registered vault that could not be consulted, so callers can show the gap
// explicitly instead of silently dropping whatever that vault holds.
type Unavailable struct {
	Vault string `json:"vault"`
	Error string `json:"error"`
}

// Resolver looks up cross-vault references for one active vault, lazily opening the other
// registered vaults' configs and stores and caching them for the resolver's lifetime. Not safe for
// concurrent use; Close releases the opened stores.
type Resolver struct {
	cfg    *config.Config
	cfgs   map[string]*config.Config
	stores map[string]*store.Store
}

func New(cfg *config.Config) *Resolver {
	return &Resolver{cfg: cfg, cfgs: map[string]*config.Config{}, stores: map[string]*store.Store{}}
}

func (r *Resolver) Close() {
	for _, s := range r.stores {
		s.Close()
	}
	r.stores = map[string]*store.Store{}
}

// IsVault reports whether name is a registered vault name — the gate for link.SplitVaultRef.
func (r *Resolver) IsVault(name string) bool {
	_, ok := r.cfg.Vaults[name]
	return ok
}

// vaultHandles returns the cached config and store for a registered vault, opening them on first
// use. The vault directory must exist and its index DB must already be built: resolution reads,
// it never creates or rebuilds another vault's cache.
func (r *Resolver) vaultHandles(name string) (*config.Config, *store.Store, error) {
	if s, ok := r.stores[name]; ok {
		return r.cfgs[name], s, nil
	}
	path, ok := r.cfg.Vaults[name]
	if !ok {
		return nil, nil, fmt.Errorf("vault %q is not registered", name)
	}
	cfg, err := config.LoadAt(path)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(cfg.VaultDir); err != nil {
		return nil, nil, fmt.Errorf("vault directory unavailable: %v", err)
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return nil, nil, fmt.Errorf("index not built yet (run a command in that vault, or track refresh-all)")
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	r.cfgs[name] = cfg
	r.stores[name] = s
	return cfg, s, nil
}

// Resolve looks (vault, title) up in the target vault's index. found=false means the vault was
// reachable but holds no such title; an error means the vault itself could not be consulted.
func (r *Resolver) Resolve(vault, title string) (Resolved, bool, error) {
	cfg, s, err := r.vaultHandles(vault)
	if err != nil {
		return Resolved{}, false, err
	}
	ref, found, err := s.ResolveTerm(title)
	if err != nil || !found {
		return Resolved{}, false, err
	}
	return Resolved{
		Vault:    vault,
		NoteID:   ref.NoteID,
		FileKind: ref.FileKind,
		Title:    ref.Title,
		Path:     cfg.PathForKind(ref.FileKind, ref.NoteID),
	}, true, nil
}

// SelfNames returns every registry name whose path is the active vault — a vault may be registered
// under several names, and inbound references may use any of them.
func (r *Resolver) SelfNames() []string {
	var names []string
	for _, name := range sortedNames(r.cfg.Vaults) {
		if canonical, err := config.CanonicalPath(r.cfg.Vaults[name]); err == nil && canonical == r.cfg.VaultDir {
			names = append(names, name)
		}
	}
	return names
}

// Inbound lists the cross-vault backlinks to the active vault's note titled title: every
// registered vault's index is scanned for ext_links rows naming this vault (under any of its
// registered names). Vaults that cannot be consulted are listed under unavailable. When the active
// vault is not registered under any name, no other vault can reference it, so the scan is empty.
func (r *Resolver) Inbound(title string) (refs []ExternalRef, unavailable []Unavailable) {
	selfNames := r.SelfNames()
	if len(selfNames) == 0 {
		return nil, nil
	}
	for _, name := range sortedNames(r.cfg.Vaults) {
		cfg, s, err := r.vaultHandles(name)
		if err != nil {
			unavailable = append(unavailable, Unavailable{Vault: name, Error: err.Error()})
			continue
		}
		backs, err := s.ExtBacklinks(selfNames, title)
		if err != nil {
			unavailable = append(unavailable, Unavailable{Vault: name, Error: err.Error()})
			continue
		}
		for _, b := range backs {
			refs = append(refs, ExternalRef{
				Vault:    name,
				NoteID:   b.NoteID,
				FileKind: b.FileKind,
				Title:    b.Title,
				Path:     cfg.PathForKind(b.FileKind, b.NoteID),
			})
		}
	}
	return refs, unavailable
}

// sortedNames returns the registry names in deterministic order, keeping CLI output and
// diagnostics stable across runs.
func sortedNames(vaults map[string]string) []string {
	names := make([]string, 0, len(vaults))
	for name := range vaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
