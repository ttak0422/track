package store

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestSearchRanksByTitleMatchMtimeAndID(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{
			ID:    100,
			Mtime: 100,
			Meta:  note.Metadata{Title: "Topic old"},
		},
		{
			ID:    200,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Topic recent lower id"},
		},
		{
			ID:    300,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Topic recent higher id"},
		},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	results, err := s.SearchScoped("Topic", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %+v", results)
	}
	gotIDs := []int64{results[0].NoteID, results[1].NoteID, results[2].NoteID}
	wantIDs := []int64{300, 200, 100}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("result order = %v, want %v", gotIDs, wantIDs)
		}
	}
}

func TestSearchRefsCarriesIconOverride(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: 100, Meta: note.Metadata{Title: "Iconed", Icon: "🔥"}},
		{ID: 200, Mtime: 200, Meta: note.Metadata{Title: "Plain"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	refs, err := s.SearchRefs()
	if err != nil {
		t.Fatalf("search refs: %v", err)
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.Title] = r.Icon
	}
	if got["Iconed"] != "🔥" {
		t.Errorf("icon override lost through index/search: %q", got["Iconed"])
	}
	if got["Plain"] != "" {
		t.Errorf("note without an override should carry no icon, got %q", got["Plain"])
	}
}

func TestNewestRefsListsOnlyRegularNotesByCreationID(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: 900, Meta: note.Metadata{Title: "Old but edited"}},
		{ID: 200, Mtime: 200, Meta: note.Metadata{Title: "Middle"}},
		{ID: 300, Mtime: 100, Meta: note.Metadata{Title: "Newest", Tags: []string{"fresh"}, Icon: "🌱"}},
		{ID: 20260802, Kind: "journal", Mtime: 1000, Meta: note.Metadata{Title: "20260802"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	refs, err := s.NewestRefs(2)
	if err != nil {
		t.Fatalf("newest refs: %v", err)
	}
	if len(refs) != 2 || refs[0].NoteID != 300 || refs[1].NoteID != 200 {
		t.Fatalf("newest refs = %+v, want note ids [300 200]", refs)
	}
	if refs[0].Icon != "🌱" || !slices.Equal(refs[0].Tags, []string{"fresh"}) {
		t.Fatalf("newest ref lost display metadata: %+v", refs[0])
	}
}

func TestSearchTitleAndOr(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: 100, Meta: note.Metadata{Title: "Alpha Beta"}},
		{ID: 200, Mtime: 200, Meta: note.Metadata{Title: "Alpha Gamma"}},
		{ID: 300, Mtime: 300, Meta: note.Metadata{Title: "Delta"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	ids := func(query string) []int64 {
		t.Helper()
		results, err := s.SearchScoped(query, 10, SearchTitle)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		got := make([]int64, len(results))
		for i, r := range results {
			got[i] = r.NoteID
		}
		slices.Sort(got)
		return got
	}

	if got := ids("Alpha Beta"); !slices.Equal(got, []int64{100}) {
		t.Fatalf("implicit AND = %v, want [100]", got)
	}
	if got := ids("Alpha AND Gamma"); !slices.Equal(got, []int64{200}) {
		t.Fatalf("explicit AND = %v, want [200]", got)
	}
	if got := ids("Beta OR Delta"); !slices.Equal(got, []int64{100, 300}) {
		t.Fatalf("OR = %v, want [100 300]", got)
	}
	if got := ids("Alpha Beta OR Delta"); !slices.Equal(got, []int64{100, 300}) {
		t.Fatalf("grouped (Alpha AND Beta) OR Delta = %v, want [100 300]", got)
	}
	if got := ids("Alpha"); !slices.Equal(got, []int64{100, 200}) {
		t.Fatalf("single term = %v, want [100 200]", got)
	}
}

func TestBodyQueryUsesFTS(t *testing.T) {
	cases := map[string]bool{
		"golang":     true,  // one long term
		"foo bar":    true,  // two long terms
		"テスト":        true,  // three CJK characters form a trigram
		"世界":         false, // two CJK characters cannot: fall back to scan
		"a":          false, // one short term
		"foo ab":     false, // any short term forces the fallback
		"":           false, // no terms
		"AND":        false, // the join keyword is dropped, leaving no terms
		"foo AND ba": false, // dropped AND leaves a short term
		"foo OR bar": true,  // OR groups of long terms still index
		"foo OR ab":  false, // a short term in any OR group forces the fallback
		"OR":         false, // the separator alone leaves no terms
	}
	for query, want := range cases {
		if got := BodyQueryUsesFTS(query); got != want {
			t.Errorf("BodyQueryUsesFTS(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestFTSMatchExprGroups(t *testing.T) {
	cases := map[string]string{
		"foo":         `("foo")`,
		"foo bar":     `("foo" AND "bar")`,
		"foo OR bar":  `("foo") OR ("bar")`,
		"a b OR c":    `("a" AND "b") OR ("c")`,
		`quo"te`:      `("quo""te")`, // an embedded quote is doubled, never parsed as syntax
		"foo AND bar": `("foo" AND "bar")`,
	}
	for query, want := range cases {
		if got := ftsMatchExprGroups(BodyGroups(query)); got != want {
			t.Errorf("ftsMatchExprGroups(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestSearchBodyFTS(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{ID: 100, Mtime: 100, Body: "alpha and beta live together here", Meta: note.Metadata{Title: "Both"}},
		{ID: 200, Mtime: 200, Body: "alpha stands alone with no partner", Meta: note.Metadata{Title: "AlphaOnly"}},
		{ID: 300, Mtime: 300, Body: "intro\n```go\nfunc searchInsideCode() {}\n```\nend", Meta: note.Metadata{Title: "Code"}},
		{ID: 400, Mtime: 400, Body: "これはテストのためのノートです", Meta: note.Metadata{Title: "CJK"}},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	ids := func(query string) []int64 {
		t.Helper()
		results, err := s.SearchBodyFTS(query, 10)
		if err != nil {
			t.Fatalf("body search %q: %v", query, err)
		}
		got := make([]int64, len(results))
		for i, r := range results {
			got[i] = r.NoteID
		}
		slices.Sort(got)
		return got
	}

	if got := ids("beta"); !slices.Equal(got, []int64{100}) {
		t.Errorf("single term = %v, want [100]", got)
	}
	if got := ids("alpha beta"); !slices.Equal(got, []int64{100}) {
		t.Errorf("multi-term AND = %v, want [100] (only the note with both terms)", got)
	}
	if got := ids("alpha"); !slices.Equal(got, []int64{100, 200}) {
		t.Errorf("shared term = %v, want [100 200]", got)
	}
	// OR matches a note satisfying either group; "partner" is only in 200, "together" only in 100.
	if got := ids("partner OR together"); !slices.Equal(got, []int64{100, 200}) {
		t.Errorf("OR of two single-term groups = %v, want [100 200]", got)
	}
	// A group is AND'd internally: "beta OR partner" = (has beta) or (has partner).
	if got := ids("beta OR partner"); !slices.Equal(got, []int64{100, 200}) {
		t.Errorf("OR mixing groups = %v, want [100 200]", got)
	}
	if got := ids("searchInsideCode"); !slices.Equal(got, []int64{300}) {
		t.Errorf("code-block text is indexed: got %v, want [300]", got)
	}
	if got := ids("テスト"); !slices.Equal(got, []int64{400}) {
		t.Errorf("CJK substring (no surrounding spaces) = %v, want [400]", got)
	}
}

func TestSearchBodyFTSRanksByRelevance(t *testing.T) {
	s := newTestStore(t)
	// Note 100 mentions the term repeatedly in a short body; note 200 mentions it once amid filler.
	// bm25 should rank the denser, shorter note first regardless of mtime (200 is newer).
	filler := "lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt"
	if err := s.UpsertNote(&note.Note{ID: 100, Mtime: 100, Body: "kubernetes kubernetes kubernetes", Meta: note.Metadata{Title: "Dense"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNote(&note.Note{ID: 200, Mtime: 200, Body: "kubernetes " + filler, Meta: note.Metadata{Title: "Sparse"}}); err != nil {
		t.Fatal(err)
	}
	results, err := s.SearchBodyFTS("kubernetes", 10)
	if err != nil {
		t.Fatalf("body search: %v", err)
	}
	if len(results) != 2 || results[0].NoteID != 100 {
		t.Fatalf("expected dense note 100 ranked first, got %+v", results)
	}
}

func TestSearchBodyFTSReflectsUpdatesAndDeletes(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertNote(&note.Note{ID: 100, Mtime: 100, Body: "originalword content", Meta: note.Metadata{Title: "N"}}); err != nil {
		t.Fatal(err)
	}
	find := func(query string) int {
		t.Helper()
		results, err := s.SearchBodyFTS(query, 10)
		if err != nil {
			t.Fatalf("body search %q: %v", query, err)
		}
		return len(results)
	}
	if find("originalword") != 1 {
		t.Fatal("expected the original body to be indexed")
	}
	// Re-upserting replaces the FTS body, not appends to it.
	if err := s.UpsertNote(&note.Note{ID: 100, Mtime: 101, Body: "replacedword content", Meta: note.Metadata{Title: "N"}}); err != nil {
		t.Fatal(err)
	}
	if find("originalword") != 0 {
		t.Error("stale term should be gone after re-upsert")
	}
	if find("replacedword") != 1 {
		t.Error("new term should be searchable after re-upsert")
	}
	// Deleting the note drops its FTS row too.
	if err := s.DeleteNote(100); err != nil {
		t.Fatal(err)
	}
	if find("replacedword") != 0 {
		t.Error("deleted note should not surface in body search")
	}
}

func TestSearchHashMatchesTagsHierarchically(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{
			ID:    100,
			Mtime: 100,
			Meta:  note.Metadata{Title: "Exact old", Tags: []string{"graph"}},
		},
		{
			ID:    200,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Exact recent", Tags: []string{"graph", "draft"}},
		},
		{
			ID:    300,
			Mtime: 500,
			Meta:  note.Metadata{Title: "Nested", Tags: []string{"graph/workspace"}},
		},
		{
			ID:    400,
			Mtime: 900,
			Meta:  note.Metadata{Title: "Unrelated prefix", Tags: []string{"graph-workspace", "cartography"}},
		},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	// #graph matches the exact tag and its descendants (graph/workspace), never "graph-workspace" or
	// "cartography". Exact matches rank before descendants; recency breaks ties.
	results, err := s.SearchScoped("#graph", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %+v", results)
	}
	gotIDs := []int64{results[0].NoteID, results[1].NoteID, results[2].NoteID}
	wantIDs := []int64{200, 100, 300}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("result order = %v, want %v", gotIDs, wantIDs)
		}
	}
	if !slices.Contains(results[0].Tags, "graph") || !slices.Contains(results[0].Tags, "draft") {
		t.Fatalf("expected tag metadata on result: %+v", results[0])
	}

	// The child tag matches only its own subtree.
	results, err = s.SearchScoped("#graph/workspace", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search child: %v", err)
	}
	if len(results) != 1 || results[0].NoteID != 300 {
		t.Fatalf("child tag results = %+v, want only note 300", results)
	}

	// "#graph/" is how a reader asks for everything under graph, which is what "#graph" already
	// selects. Kept as written it matched nothing — no tag is stored with a trailing separator — so
	// the query fell through to a full-text hunt for the literal "#graph/".
	trailing, err := s.SearchScoped("#graph/", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search trailing separator: %v", err)
	}
	var trailingIDs []int64
	for _, r := range trailing {
		trailingIDs = append(trailingIDs, r.NoteID)
	}
	if !slices.Equal(trailingIDs, wantIDs) {
		t.Fatalf("#graph/ results = %v, want the same as #graph (%v)", trailingIDs, wantIDs)
	}
}

func TestSearchHashPrefixCombinesMultipleTagsAndTitleText(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*note.Note{
		{
			ID:    100,
			Mtime: 100,
			Meta:  note.Metadata{Title: "Graph Workspace", Tags: []string{"graph", "web"}},
		},
		{
			ID:    200,
			Mtime: 200,
			Meta:  note.Metadata{Title: "Graph Draft", Tags: []string{"graph", "draft"}},
		},
		{
			ID:    300,
			Mtime: 300,
			Meta:  note.Metadata{Title: "Web Workspace", Tags: []string{"web", "workspace"}},
		},
	} {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}

	results, err := s.SearchScoped("#graph #web", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search tags: %v", err)
	}
	if len(results) != 1 || results[0].NoteID != 100 {
		t.Fatalf("multi-tag results = %+v, want only note 100", results)
	}

	results, err = s.SearchScoped("#graph Workspace", 10, SearchTitle)
	if err != nil {
		t.Fatalf("search tag plus text: %v", err)
	}
	if len(results) != 1 || results[0].NoteID != 100 {
		t.Fatalf("tag plus text results = %+v, want only note 100", results)
	}
}

// seedStore opens a fresh index and fills it with notes, standing in for one vault.
func seedStore(t *testing.T, notes ...*note.Note) *Store {
	t.Helper()
	s := newTestStore(t)
	for _, n := range notes {
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", n.ID, err)
		}
	}
	return s
}

func TestMergeSearchResultsMatchesOneVaultsOrdering(t *testing.T) {
	// Cross-vault search is per-vault queries merged in Go, so the merge has to reproduce the order
	// one query over all the notes would have produced — the rank the SQL assigned included, not a
	// recomputed one. Ids are distinct here so the combined store is a fair reference.
	all := []*note.Note{
		{ID: 100, Mtime: 100, Meta: note.Metadata{Title: "Topic"}},              // exact title
		{ID: 200, Mtime: 500, Meta: note.Metadata{Title: "About the Topic"}},    // substring only
		{ID: 300, Mtime: 400, Meta: note.Metadata{Title: "Topic of the day"}},   // title prefix
		{ID: 400, Mtime: 400, Meta: note.Metadata{Title: "Topic, second"}},      // prefix, mtime tie
		{ID: 500, Mtime: 900, Meta: note.Metadata{Title: "Untitled Topic pad"}}, // substring, newest
		{ID: 600, Mtime: 200, Meta: note.Metadata{Title: "Nothing here"}},       // no match at all
	}
	combined := seedStore(t, all...)
	// The same notes split over three vaults, one of which holds nothing that matches.
	vaults := []*Store{
		seedStore(t, all[0], all[4]),
		seedStore(t, all[1], all[2], all[3]),
		seedStore(t, all[5]),
	}

	for _, limit := range []int{10, 3, 1} {
		want, err := combined.SearchScoped("Topic", limit, SearchTitle)
		if err != nil {
			t.Fatalf("combined search: %v", err)
		}
		pages := make([][]SearchResult, 0, len(vaults))
		for i, v := range vaults {
			page, err := v.SearchScoped("Topic", limit, SearchTitle)
			if err != nil {
				t.Fatalf("vault %d search: %v", i, err)
			}
			for j := range page {
				page[j].Vault = fmt.Sprintf("v%d", i)
			}
			pages = append(pages, page)
		}
		got := MergeSearchResults(pages, limit)
		if len(got) != len(want) {
			t.Fatalf("limit %d: merged %d hits, one vault holding the same notes gives %d", limit, len(got), len(want))
		}
		for i := range want {
			if got[i].NoteID != want[i].NoteID {
				t.Fatalf("limit %d: merged order %s, want %s", limit, resultIDs(got), resultIDs(want))
			}
		}
	}
}

// resultIDs renders a result page's ids for failure messages.
func resultIDs(results []SearchResult) string {
	ids := make([]int64, len(results))
	for i, r := range results {
		ids[i] = r.NoteID
	}
	return fmt.Sprint(ids)
}

func TestMergeSearchResultsHasNoVaultCeiling(t *testing.T) {
	// The ATTACH-based cross-vault query it replaced could only reach SQLite's 10 attached databases
	// and silently answered for the first ten. A Go-side merge has no such limit.
	pages := make([][]SearchResult, 0, 14)
	for i := range 14 {
		s := seedStore(t, &note.Note{ID: 100, Mtime: int64(i), Meta: note.Metadata{Title: "Topic"}})
		page, err := s.SearchScoped("Topic", 50, SearchTitle)
		if err != nil {
			t.Fatalf("vault %d search: %v", i, err)
		}
		for j := range page {
			page[j].Vault = fmt.Sprintf("v%d", i)
		}
		pages = append(pages, page)
	}
	merged := MergeSearchResults(pages, 50)
	if len(merged) != 14 {
		t.Fatalf("every vault must contribute, got %d hits", len(merged))
	}
	// Same rank and same id in every vault: recency decides, and the newest comes first.
	if merged[0].Vault != "v13" || merged[13].Vault != "v0" {
		t.Fatalf("merged order should be by recency across vaults, got %+v", merged)
	}
}

func TestBodyFTSCarriesItsRankForMerging(t *testing.T) {
	// A body hit merges on bm25, so the score has to survive the scan out of SQLite: without it every
	// hit would rank 0 and cross-vault body search would degrade to plain recency.
	dense := seedStore(t, &note.Note{ID: 100, Mtime: 100, Body: "needle needle needle", Meta: note.Metadata{Title: "Dense"}})
	sparse := seedStore(t, &note.Note{ID: 200, Mtime: 900, Body: "one needle in a much longer body of other words", Meta: note.Metadata{Title: "Sparse"}})

	var pages [][]SearchResult
	for _, s := range []*Store{sparse, dense} {
		page, err := s.SearchBodyFTS("needle", 10)
		if err != nil {
			t.Fatalf("body fts: %v", err)
		}
		if len(page) != 1 || page[0].Rank == 0 {
			t.Fatalf("body hit must carry its bm25 rank, got %+v", page)
		}
		pages = append(pages, page)
	}
	// bm25 is negative and lower is better, so the denser note wins despite being the older one.
	merged := MergeSearchResults(pages, 10)
	if len(merged) != 2 || merged[0].NoteID != 100 {
		t.Fatalf("merged body hits should be bm25-ordered, got %+v", merged)
	}
}
