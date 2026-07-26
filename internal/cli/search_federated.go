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

	results, err := federatedScoped(healthy, query, limit, scope)
	if err != nil {
		return nil, err
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	return map[string]any{"results": results, "unavailable": unavailable}, nil
}

// federatedScoped mirrors searchResults across vaults, but keeps its two phases apart: title hits from
// every vault merge into one page, then body hits from every vault merge into another. Merging each
// vault's already-composed title-then-body list instead would interleave bm25-ranked body hits with
// title hits, which are ranked on a different scale. Hits are deduplicated by (vault, id).
func federatedScoped(vaults []openVault, query string, limit int, scope store.SearchScope) ([]store.SearchResult, error) {
	switch scope {
	case store.SearchTitle:
		return federatedTitleResults(vaults, query, limit)
	case store.SearchAll:
		results, err := federatedTitleResults(vaults, query, limit)
		if err != nil {
			return nil, err
		}
		seen := make(map[vaultKey]bool, len(results))
		for _, r := range results {
			seen[vaultKey{r.Vault, r.NoteID}] = true
		}
		body, err := federatedBodyResults(vaults, query, limit-len(results), seen)
		if err != nil {
			return nil, err
		}
		return append(results, body...), nil
	case store.SearchBody:
		return federatedBodyResults(vaults, query, limit, nil)
	default:
		return nil, fmt.Errorf("unknown search scope %q", scope)
	}
}

// federatedTitleResults runs the single-vault title query in every vault, labels and resolves each
// hit against the vault it came from, and merges the pages into the global top-k.
func federatedTitleResults(vaults []openVault, query string, limit int) ([]store.SearchResult, error) {
	pages := make([][]store.SearchResult, 0, len(vaults))
	for _, v := range vaults {
		page, err := v.store.SearchScoped(query, limit, store.SearchTitle)
		if err != nil {
			return nil, err
		}
		for i := range page {
			page[i].Vault = v.name
			page[i].Path = v.cfg.PathForKind(page[i].FileKind, page[i].NoteID)
		}
		pages = append(pages, page)
	}
	return store.MergeSearchResults(pages, limit), nil
}

// federatedBodyResults is the cross-vault counterpart of bodySearchResults, and runs exactly that per
// vault — so the FTS path, the short-term scan fallback, and the line/snippet lookup all stay in one
// place. Already-returned title hits are skipped per vault, since ids only mean anything inside one.
func federatedBodyResults(vaults []openVault, query string, limit int, seen map[vaultKey]bool) ([]store.SearchResult, error) {
	if limit <= 0 {
		return []store.SearchResult{}, nil
	}
	if len(store.BodyGroups(query)) == 0 {
		return []store.SearchResult{}, nil
	}
	pages := make([][]store.SearchResult, 0, len(vaults))
	for _, v := range vaults {
		skip := map[int64]bool{}
		for key := range seen {
			if key.vault == v.name {
				skip[key.id] = true
			}
		}
		page, err := bodySearchResults(v.cfg, v.store, query, limit, skip)
		if err != nil {
			return nil, err
		}
		for i := range page {
			page[i].Vault = v.name
		}
		pages = append(pages, page)
	}
	return store.MergeSearchResults(pages, limit), nil
}
