package search

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ttak0422/track/internal/track/store"
)

// The published site has no server to ask, so it runs this package's scan path in the browser
// (ADR 0067): web/src/staticSearch.ts is a line-for-line port of bodyMatchesAnyGroup and
// bodyLineMatchGroups. A port drifts silently — each side stays green while they stop agreeing — so
// both read this one fixture and must answer every case the same way. The TypeScript half is in
// web/src/staticSearch.test.ts; this half is what makes the fixture's answers Go's answers.
func TestBodyScanFixtureMatchesPublishedPort(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/staticSearch.cases.json")
	if err != nil {
		t.Fatalf("read the shared search fixture: %v", err)
	}
	var fixture struct {
		Cases []struct {
			Name    string `json:"name"`
			Query   string `json:"query"`
			Body    string `json:"body"`
			Match   bool   `json:"match"`
			Line    int    `json:"line"`
			Snippet string `json:"snippet"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse the shared search fixture: %v", err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the shared search fixture lists no cases")
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			groups := store.BodyGroups(c.Query)
			if got := bodyMatchesAnyGroup(c.Body, groups); got != c.Match {
				t.Errorf("bodyMatchesAnyGroup(%q, %q) = %v, want %v", c.Body, c.Query, got, c.Match)
			}
			// The line lookup runs even when the body did not match: the fixture pins what it answers
			// either way, since the port has to agree on the fallback line too.
			line, snippet := bodyLineMatchGroups(c.Body, groups)
			if line != c.Line || snippet != c.Snippet {
				t.Errorf("bodyLineMatchGroups(%q, %q) = (%d, %q), want (%d, %q)",
					c.Body, c.Query, line, snippet, c.Line, c.Snippet)
			}
		})
	}
}
