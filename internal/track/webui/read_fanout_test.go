package webui

import (
	"context"
	"github.com/ttak0422/track/internal/track/store"
	"sync/atomic"
	"testing"
)

func TestBoundedFanoutRetainsHealthyPeerAndChargesHungQueries(t *testing.T) {
	gate := newReadBoundary()
	blocked := &vaultView{name: "blocked"}
	healthy := &vaultView{name: "healthy"}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	exited := make(chan struct{}, 2)
	var healthyCalls atomic.Int32
	read := func(v *vaultView) ([]store.SearchResult, error) {
		if v == blocked {
			entered <- struct{}{}
			<-release
			exited <- struct{}{}
		}
		if v == healthy {
			healthyCalls.Add(1)
		}
		return []store.SearchResult{{Title: v.name}}, nil
	}
	for i := range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan [][]store.SearchResult, 1)
		go func() { pages, _ := boundedPages(ctx, gate, []*vaultView{blocked, healthy}, read); result <- pages }()
		<-entered
		// A canceled overall operation still retains any already-confirmed peer page.
		// Wait on the peer's admission release rather than relying on a timing sleep.
		waitBoot(t, func() bool {
			gate.mu.Lock()
			defer gate.mu.Unlock()
			return gate.active[healthy.name] == 0 && healthyCalls.Load() >= int32(i+1)
		})
		cancel()
		pages := <-result
		if len(pages) != 1 || pages[0][0].Title != "healthy" {
			t.Fatalf("healthy result lost: %+v", pages)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pages, gaps := boundedPages(ctx, gate, []*vaultView{blocked, healthy}, read)
	if len(pages) != 1 || len(gaps) != 1 {
		t.Fatal(pages, gaps)
	}
	close(release)
	<-exited
	<-exited
}
