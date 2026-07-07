package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestIncludesCommandExtractsSections(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "note", "100.md"))
	srv.docs[targetURI] = "# Go\n\n## 設計\nfirst\nsecond\n\n## 実装\nimpl"
	uri := uriFromPath(filepath.Join(vault, "note", "200.md"))
	srv.docs[uri] = "intro\n![[Go##設計]] :only-contents\n![[Missing]]\n![[Go###ない]]"

	arg, _ := json.Marshal(includesParams{URI: uri})
	raw, err := srv.executeCommand(executeCommandParams{
		Command:   includesCommand,
		Arguments: []json.RawMessage{arg},
	})
	if err != nil {
		t.Fatalf("execute includes: %v", err)
	}
	results, ok := raw.([]includeResult)
	if !ok || len(results) != 3 {
		t.Fatalf("expected 3 include results, got %+v", raw)
	}

	sec := results[0]
	if sec.NoteID != 100 || sec.Title != "Go" || sec.Range.Start.Line != 1 {
		t.Errorf("section include resolved wrong: %+v", sec)
	}
	if len(sec.Lines) != 2 || sec.Lines[0] != "first" || sec.Lines[1] != "second" {
		t.Errorf("section lines = %q", sec.Lines)
	}

	if results[1].Error == "" || results[1].NoteID != 0 {
		t.Errorf("unresolved include must carry an error: %+v", results[1])
	}
	if results[2].Error == "" || len(results[2].Lines) != 0 {
		t.Errorf("missing heading must error, not fall back: %+v", results[2])
	}
}
