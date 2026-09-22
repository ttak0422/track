package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBabelDuplicateDiagnosticsUseUnsavedDocument(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "note", "200.md"))
	srv.docs[uri] = "```sh :name 同名\necho a\n```\n```sh :name 同名\necho b\n```\n````markdown\n```sh :name 同名\n```\n````"
	diags, err := srv.diagnostics(uri)
	if err != nil || len(diags) != 2 {
		t.Fatalf("diagnostics = %+v, %v", diags, err)
	}
	for i, d := range diags {
		if d.Code != "duplicate-babel-name" || int(d.Range.Start.Line) != i*3 || !strings.Contains(d.Message, "同名") {
			t.Fatalf("unexpected diagnostic: %+v", d)
		}
	}
	srv.docs[uri] = strings.Replace(srv.docs[uri], ":name 同名", ":name unique", 1)
	diags, err = srv.diagnostics(uri)
	if err != nil || len(diags) != 0 {
		t.Fatalf("fixed diagnostics = %+v, %v", diags, err)
	}
}
