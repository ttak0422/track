package cli

import (
	"fmt"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/store"
)

// vaultKey is the (vault, id) identity a cross-vault result needs: the same numeric id can name
// different notes in different vaults.
type vaultKey struct {
	vault string
	id    int64
}

// federatedSearchResults runs one search across the active and every registered vault: self-heal
// each vault's index, attach all reachable DBs to one connection (store.OpenFederated), and merge
// ranked, vault-labeled results. An unreachable vault is reported under "unavailable" instead of
// failing the search — the caller still gets everything the reachable vaults hold.
func federatedSearchResults(targets []vaultTarget, query string, limit int, scope store.SearchScope) (map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}

	var healthy []vaultTarget
	cfgs := map[string]*config.Config{}
	unavailable := []map[string]any{}
	for _, tgt := range targets {
		var cfg *config.Config
		_, err := runInVault(tgt, func(c *config.Config) (map[string]any, error) {
			s, err := store.Open(c.DBPath)
			if err != nil {
				return nil, err
			}
			defer s.Close()
			if _, err := index.New(c, s).RefreshIfStale(); err != nil {
				return nil, err
			}
			cfg = c
			return nil, nil
		})
		if err != nil {
			unavailable = append(unavailable, map[string]any{"name": tgt.Name, "path": tgt.Path, "error": err.Error()})
			continue
		}
		healthy = append(healthy, tgt)
		cfgs[tgt.Name] = cfg
	}

	results := []store.SearchResult{}
	if len(healthy) > 0 {
		vaults := make([]store.FederatedVault, len(healthy))
		for i, tgt := range healthy {
			vaults[i] = store.FederatedVault{Name: tgt.Name, DBPath: cfgs[tgt.Name].DBPath}
		}
		fed, err := store.OpenFederated(vaults)
		if err != nil {
			return nil, err
		}
		defer fed.Close()
		results, err = federatedScoped(fed, cfgs, healthy, query, limit, scope)
		if err != nil {
			return nil, err
		}
		if results == nil {
			results = []store.SearchResult{}
		}
	}
	return map[string]any{"results": results, "unavailable": unavailable}, nil
}

// federatedScoped mirrors searchResults for the federated connection: title hits first, then body
// hits that were not already title hits, deduplicated by (vault, id).
func federatedScoped(fed *store.Federated, cfgs map[string]*config.Config, healthy []vaultTarget, query string, limit int, scope store.SearchScope) ([]store.SearchResult, error) {
	switch scope {
	case store.SearchTitle:
		results, err := fed.Search(query, limit)
		addFederatedPaths(cfgs, results)
		return results, err
	case store.SearchAll:
		results, err := fed.Search(query, limit)
		if err != nil {
			return nil, err
		}
		addFederatedPaths(cfgs, results)
		seen := make(map[vaultKey]bool, len(results))
		for _, r := range results {
			seen[vaultKey{r.Vault, r.NoteID}] = true
		}
		body, err := federatedBodyResults(fed, cfgs, healthy, query, limit-len(results), seen)
		if err != nil {
			return nil, err
		}
		return append(results, body...), nil
	case store.SearchBody:
		return federatedBodyResults(fed, cfgs, healthy, query, limit, nil)
	default:
		return nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

// federatedBodyResults is the cross-vault counterpart of bodySearchResults: the federated FTS
// union for indexable queries, or a per-vault file scan for terms too short to form a trigram.
func federatedBodyResults(fed *store.Federated, cfgs map[string]*config.Config, healthy []vaultTarget, query string, limit int, seen map[vaultKey]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	groups := store.BodyGroups(query)
	if len(groups) == 0 {
		return []store.SearchResult{}, nil
	}
	if store.BodyQueryUsesFTS(query) {
		hits, err := fed.SearchBodyFTS(query, limit+len(seen))
		if err != nil {
			return nil, err
		}
		out := make([]store.SearchResult, 0, len(hits))
		for _, hit := range hits {
			if seen[vaultKey{hit.Vault, hit.NoteID}] {
				continue
			}
			hit.Path = cfgs[hit.Vault].PathForKind(hit.FileKind, hit.NoteID)
			hit.Line, hit.Snippet = fileLineMatchGroups(hit.Path, groups)
			out = append(out, hit)
			if len(out) >= limit {
				break
			}
		}
		return out, nil
	}

	// Short terms fall back to the per-vault file scan, merged by the shared recency order.
	var out []store.SearchResult
	for _, tgt := range healthy {
		cfg := cfgs[tgt.Name]
		s, err := store.Open(cfg.DBPath)
		if err != nil {
			return nil, err
		}
		skip := map[int64]bool{}
		for key := range seen {
			if key.vault == tgt.Name {
				skip[key.id] = true
			}
		}
		hits, err := bodySearchScan(cfg, s, groups, limit, skip)
		s.Close()
		if err != nil {
			return nil, err
		}
		for i := range hits {
			hits[i].Vault = tgt.Name
		}
		out = append(out, hits...)
	}
	sortSearchResults(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func addFederatedPaths(cfgs map[string]*config.Config, results []store.SearchResult) {
	for i := range results {
		results[i].Path = cfgs[results[i].Vault].PathForKind(results[i].FileKind, results[i].NoteID)
	}
}
