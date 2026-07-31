package cli

import (
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/search"
	"github.com/ttak0422/track/internal/track/store"
)

// openVault is one reachable vault a cross-vault search reads: the registry name that labels its rows
// ("" for the unregistered active vault), its config, and the index handle held open across both
// search phases so a vault is opened and self-healed once per command.
type openVault struct {
	name  string
	cfg   *config.Config
	store *store.Store
}

// federatedSearchResults runs one search across the active and every registered vault: self-heal each
// vault's index, run the ordinary single-vault query in each, and merge the per-vault pages on the
// rank key their own SQL assigned (store.MergeSearchResults). Each page is that vault's top-k under
// the one shared total order, so the merge is the exact global top-k rather than an approximation —
// and unlike a query spanning attached databases it has no ceiling on how many vaults it covers. An
// unreachable vault is reported under "unavailable" instead of failing the search.
func federatedSearchResults(targets []vaultTarget, query string, limit int, scope store.SearchScope) (map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}

	var healthy []openVault
	defer func() {
		for _, v := range healthy {
			v.store.Close()
		}
	}()
	unavailable := []map[string]any{}
	for _, tgt := range targets {
		var opened openVault
		_, err := runInVault(tgt, func(c *config.Config) (map[string]any, error) {
			s, err := store.Open(c.DBPath)
			if err != nil {
				return nil, err
			}
			if _, err := index.New(c, s).RefreshIfStale(); err != nil {
				s.Close()
				return nil, err
			}
			opened = openVault{name: tgt.Name, cfg: c, store: s}
			return nil, nil
		})
		if err != nil {
			unavailable = append(unavailable, map[string]any{"name": tgt.Name, "path": tgt.Path, "error": err.Error()})
			continue
		}
		healthy = append(healthy, opened)
	}

	vaults := make([]search.Vault, 0, len(healthy))
	for _, v := range healthy {
		vaults = append(vaults, search.Vault{Name: v.name, Cfg: v.cfg, Store: v.store})
	}
	results, err := search.Federated(vaults, query, limit, scope)
	if err != nil {
		return nil, err
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	return map[string]any{"results": results, "unavailable": unavailable}, nil
}
