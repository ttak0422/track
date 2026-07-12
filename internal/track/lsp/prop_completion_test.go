package lsp

import (
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

func setupPropServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, vault := setupServer(t)
	srv.cfg.Properties = map[string]config.PropSpec{
		"status": {Type: "string", Values: []string{"draft", "done"}},
		"stage":  {},
		"done":   {Type: "boolean"},
	}
	return srv, vault
}

func TestPropertyCompletionOffersKeys(t *testing.T) {
	srv, vault := setupPropServer(t)
	uri := uriFromPath(filepath.Join(vault, "note", "100.md"))
	srv.docs[uri] = "- sta"

	items, err := srv.completion(uri, position{Line: 0, Character: 5})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "status") || !completionLabelsContain(items, "stage") {
		t.Fatalf("expected status and stage keys, got %+v", items)
	}
	if completionLabelsContain(items, "done") {
		t.Fatalf("keys not matching the typed prefix should be filtered, got %+v", items)
	}
	if edit := completionEdit(&items[1]); edit == nil || edit.NewText != "status:: " {
		t.Fatalf("key completion should insert 'status:: ', got %+v", edit)
	}
}

func TestPropertyCompletionOffersEnumValues(t *testing.T) {
	srv, vault := setupPropServer(t)
	uri := uriFromPath(filepath.Join(vault, "note", "100.md"))
	srv.docs[uri] = "status:: dr"

	items, err := srv.completion(uri, position{Line: 0, Character: 11})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if len(items) != 1 || items[0].Label != "draft" {
		t.Fatalf("expected the single matching enum value, got %+v", items)
	}
	// The edit replaces everything after "::" so the field normalizes to one separating space.
	if edit := completionEdit(&items[0]); edit == nil || edit.NewText != " draft" || edit.Range.Start.Character != 8 {
		t.Fatalf("value completion edit = %+v", completionEdit(&items[0]))
	}
}

func TestPropertyCompletionBooleanValues(t *testing.T) {
	srv, vault := setupPropServer(t)
	uri := uriFromPath(filepath.Join(vault, "note", "100.md"))
	srv.docs[uri] = "done::"

	items, err := srv.completion(uri, position{Line: 0, Character: 6})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "true") || !completionLabelsContain(items, "false") {
		t.Fatalf("expected true/false for a boolean property, got %+v", items)
	}
}

func TestPropertyCompletionSkipsCodeFences(t *testing.T) {
	srv, vault := setupPropServer(t)
	uri := uriFromPath(filepath.Join(vault, "note", "100.md"))
	srv.docs[uri] = "```\nsta\n```\n"

	items, err := srv.completion(uri, position{Line: 1, Character: 3})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if completionLabelsContain(items, "status") {
		t.Fatalf("no property keys inside a code fence, got %+v", items)
	}
}

func TestPropertyCompletionSilentWithoutSchema(t *testing.T) {
	srv, vault := setupServer(t) // no Properties configured
	uri := uriFromPath(filepath.Join(vault, "note", "100.md"))
	srv.docs[uri] = "sta"

	items, err := srv.completion(uri, position{Line: 0, Character: 3})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if completionLabelsContain(items, "status") {
		t.Fatalf("unexpected property completion without a schema: %+v", items)
	}
}
