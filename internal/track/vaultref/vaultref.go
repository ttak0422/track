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

// IsVault reports whether name qualifies a reference to *another* vault — the gate for
// link.SplitVaultRef. The active vault is in its own registry (ADR 0051), but its own name is not a
// qualifier: [[personal:Foo]] written inside personal is the ordinary local link to Foo, not a
// cross-vault edge that backlinks and the graph would never see.
func (r *Resolver) IsVault(name string) bool {
	if name == r.SelfName() {
		return false
	}
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

// Keywords returns the target vault's auto-link dictionary, for cross-vault link completion.
func (r *Resolver) Keywords(vault string) ([]store.Keyword, error) {
	_, s, err := r.vaultHandles(vault)
	if err != nil {
		return nil, err
	}
	return s.Keywords()
}

// SelfName returns the registry name of the active vault, or "" when it is not registered — in
// which case no other vault can name it, so nothing can reference it.
func (r *Resolver) SelfName() string {
	return r.cfg.VaultName
}

// Inbound lists the cross-vault backlinks to the active vault's note titled title: every *other*
// registered vault's index is scanned for ext_links rows naming this vault. Vaults that cannot be consulted are listed under unavailable. When the active
// vault is not registered under any name, no other vault can reference it, so the scan is empty.
//
// The active vault is skipped: a note there naming its own vault writes an ordinary local link
// (IsVault), so its references are already the plain backlinks — scanning it would only surface
// stale self ext_links rows left by older indexes, and cost a second store.Open on our own DB. The
// web UI answers the same way (webui.externalBacklinks).
func (r *Resolver) Inbound(title string) (refs []ExternalRef, unavailable []Unavailable) {
	self := r.SelfName()
	if self == "" {
		return nil, nil
	}
	for _, name := range sortedNames(r.cfg.Vaults) {
		if name == self {
			continue
		}
		cfg, s, err := r.vaultHandles(name)
		if err != nil {
			unavailable = append(unavailable, Unavailable{Vault: name, Error: err.Error()})
			continue
		}
		backs, err := s.ExtBacklinks([]string{self}, title)
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
