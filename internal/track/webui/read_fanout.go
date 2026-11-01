package webui

import (
	"context"
	"github.com/ttak0422/track/internal/track/store"
	"time"
)

// A cache query can itself be stuck in the OS. Its owned goroutine retains
// admission until it actually exits; a healthy peer does not wait for it.
func boundedPages(ctx context.Context, gate *readBoundary, views []*vaultView, read func(*vaultView) ([]store.SearchResult, error)) ([][]store.SearchResult, []vaultInfo) {
	type result struct {
		view *vaultView
		page []store.SearchResult
		err  error
	}
	done := make(chan result, len(views))
	pending := map[*vaultView]bool{}
	gaps := []vaultInfo{}
	for _, v := range views {
		if !gate.admit(v.name) {
			gaps = append(gaps, vaultInfo{Name: v.name, Error: "vault query capacity is unavailable"})
			continue
		}
		pending[v] = true
		go func(v *vaultView) { defer gate.release(v.name); page, err := read(v); done <- result{v, page, err} }(v)
	}
	pages := [][]store.SearchResult{}
	collect := func(out result) {
		delete(pending, out.view)
		if out.err != nil {
			gaps = append(gaps, vaultInfo{Name: out.view.name, Error: out.err.Error()})
		} else {
			pages = append(pages, out.page)
		}
	}
	for len(pending) > 0 {
		select {
		case out := <-done:
			collect(out)
		case <-ctx.Done():
			// A ready page remains useful even when deadline and delivery become ready together.
			for {
				select {
				case out := <-done:
					collect(out)
				default:
					for v := range pending {
						gaps = append(gaps, vaultInfo{Name: v.name, Error: "vault query deadline reached"})
					}
					return pages, gaps
				}
			}
		}
	}
	return pages, gaps
}
func (s *Server) searchAcrossBounded(views []*vaultView, query string, limit int) ([]store.SearchResult, []vaultInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	pages, gaps := boundedPages(ctx, s.queryReads, views, func(v *vaultView) ([]store.SearchResult, error) { return s.searchOne(v, query, limit) })
	if query == "" {
		out := []store.SearchResult{}
		for _, page := range pages {
			out = append(out, page...)
		}
		sortRefs(out)
		if len(out) > limit {
			out = out[:limit]
		}
		return out, gaps, nil
	}
	out := []store.SearchResult{}
	for _, match := range []string{"title", "body", "path"} {
		grouped := [][]store.SearchResult{}
		for _, page := range pages {
			group := []store.SearchResult{}
			for _, hit := range page {
				if hit.Match == match {
					group = append(group, hit)
				}
			}
			grouped = append(grouped, group)
		}
		if left := limit - len(out); left > 0 {
			out = append(out, store.MergeSearchResults(grouped, left)...)
		}
	}
	return out, gaps, nil
}
