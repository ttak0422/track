package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

// TestDefinitionRefreshesStaleIndex covers a note created by another process (the CLI, the web server,
// a second editor, or a cloud-sync write) after this LSP started: it lands on disk but raises no LSP
// event, so the keyword index is stale and a [[link]] to it would otherwise fail to resolve. Serving
// definition must refresh the index from disk first so the jump works on the first try.
func TestDefinitionRefreshesStaleIndex(t *testing.T) {
	srv, vault := setupServer(t)

	const targetID = 1782233452000
	const title = "20260612 MNS BE定例" // date-prefixed, with a space and multibyte tail
	targetPath := srv.cfg.NotePath(targetID)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create note dir: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("# meeting\n"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	if err := note.WriteMetadata(srv.cfg.MetadataPath(targetID), note.Metadata{Title: title}); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	sourceURI := uriFromPath(filepath.Join(vault, "note", "300.md"))
	srv.docs[sourceURI] = "see [[" + title + "]] here"

	// The store has never seen the target note, so resolving the link directly fails: the index is stale.
	if loc, err := srv.definition(sourceURI, newPosition(0, 8)); err != nil || loc != nil {
		t.Fatalf("expected stale index to miss before refresh, got loc=%+v err=%v", loc, err)
	}

	// Going through handleRequest must refresh the index from disk and then resolve the link.
	params, err := json.Marshal(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: documentURI(sourceURI)},
		Position:     newPosition(0, 8),
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := srv.handleRequest(rpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "textDocument/definition",
		Params:  params,
	})
	if resp.Error != nil {
		t.Fatalf("definition request error: %+v", resp.Error)
	}
	loc, ok := resp.Result.(*location)
	if !ok || loc == nil {
		t.Fatalf("expected resolved location after refresh, got %T %+v", resp.Result, resp.Result)
	}
	if string(loc.URI) != uriFromPath(targetPath) {
		t.Fatalf("resolved to the wrong note: %q", loc.URI)
	}
}
