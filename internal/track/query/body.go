// Store-backed body conditions. The query language's body attribute (body = "text" /
// body != "text") matches note bodies with the same grammar as full-text search, so the live
// surfaces resolve it through the same FTS5 index body search uses — never by grepping the files
// per query. This keeps the index schema untouched: notes_fts is the one full-text table, shared
// by `track search --scope body` and every body-conditioned query.
package query

import (
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/search"
	"github.com/ttak0422/track/internal/track/store"
)

// BodyResolver resolves a body full-text expression to the set of note ids whose bodies satisfy
// it. The expression is the body-search grammar — terms AND together, an uppercase OR separates
// alternatives — so the same expression matches the same notes in `track search --scope body`.
type BodyResolver func(expr string) (map[int64]bool, error)

// StoreBodyResolver returns a BodyResolver for a store-backed query surface. Expressions whose
// terms are all long enough to form trigrams go straight through the store's FTS5 index — a WHERE
// filter needs ids, not line numbers, so no matched file is read. An expression with a shorter
// term (a two-letter word, a two-character CJK word) has no trigram and falls back to the same
// per-file scan body search makes. limit caps how many matching notes one resolution may return;
// the count of rows being filtered is the natural cap, since a query can never match a note
// outside its row domain. A non-positive limit is treated as 50, the search path's own floor —
// with no rows to filter the result is empty either way.
func StoreBodyResolver(cfg *config.Config, s *store.Store, limit int) BodyResolver {
	return func(expr string) (map[int64]bool, error) {
		if limit <= 0 {
			limit = 50
		}
		if store.BodyQueryUsesFTS(expr) {
			hits, err := s.SearchBodyFTS(expr, limit)
			if err != nil {
				return nil, err
			}
			ids := make(map[int64]bool, len(hits))
			for _, h := range hits {
				ids[h.NoteID] = true
			}
			return ids, nil
		}
		hits, err := search.Scoped(cfg, s, expr, limit, store.SearchBody)
		if err != nil {
			return nil, err
		}
		ids := make(map[int64]bool, len(hits))
		for _, h := range hits {
			ids[h.NoteID] = true
		}
		return ids, nil
	}
}
