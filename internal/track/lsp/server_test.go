package lsp

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func setupServer(t *testing.T) (*Server, string) {
	t.Helper()
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:          vault,
		DBPath:            filepath.Join(vault, ".track", "index.db"),
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertNote(&note.Note{
		ID:   100,
		Path: cfg.NotePath(100),
		Meta: note.Metadata{
			Title:   "Go",
			Aliases: []string{"Golang"},
		},
	}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	return NewServer(cfg, s), vault
}

func TestDocumentLinks(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "[[Golang]] and [[Go]]"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %+v", links)
	}
	if links[0].Range.Start.Line != 0 || links[0].Range.Start.Character != 2 || links[0].Range.End.Character != 8 {
		t.Fatalf("unexpected first range: %+v", links[0].Range)
	}
	if links[0].Target != uriFromPath(filepath.Join(vault, "100.md")) {
		t.Fatalf("unexpected target: %q", links[0].Target)
	}
}

func TestDefinition(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "mentions [[Golang]]"

	for _, col := range []int{9, 10, 13, 17, 18} {
		loc, err := srv.definition(uri, position{Line: 0, Character: col})
		if err != nil {
			t.Fatalf("definition at col %d: %v", col, err)
		}
		if loc == nil {
			t.Fatalf("expected definition at col %d", col)
		}
		if loc.URI != uriFromPath(filepath.Join(vault, "100.md")) {
			t.Fatalf("unexpected definition uri at col %d: %q", col, loc.URI)
		}
	}
	loc, err := srv.definition(uri, position{Line: 0, Character: 19})
	if err != nil {
		t.Fatalf("definition after link: %v", err)
	}
	if loc != nil {
		t.Fatalf("did not expect definition after the link, got %+v", loc)
	}
}

func TestSelfLinksAreNotLinked(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[uri] = "# Go\n\n[[Go]]"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no self-links, got %+v", links)
	}

	loc, err := srv.definition(uri, position{Line: 2, Character: 3})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if loc != nil {
		t.Fatalf("self-link should not resolve, got %+v", loc)
	}
}

func TestTitleLineCanLinkToAnotherNote(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "# [[Go]]"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected title link to another note, got %+v", links)
	}
}

func TestDisplayAliasResolves(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go|ゴー]]"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected display-alias link to resolve, got %+v", links)
	}
	if links[0].Target != uriFromPath(filepath.Join(vault, "100.md")) {
		t.Fatalf("unexpected target: %q", links[0].Target)
	}
}

func TestCompletionInsideBrackets(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go"

	items, err := srv.completion(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	// note 100 contributes both its title (Go) and alias (Golang).
	if len(items) != 2 {
		t.Fatalf("expected 2 candidates, got %+v", items)
	}
	var goItem *completionItem
	for i := range items {
		if items[i].Label == "Go" {
			goItem = &items[i]
			break
		}
	}
	if goItem == nil || goItem.TextEdit == nil || goItem.TextEdit.NewText != "Go]]" {
		t.Fatalf("existing note completion should close the link, got %+v", goItem)
	}
}

func TestCompletionExcludesSelfAndOutsideBrackets(t *testing.T) {
	srv, vault := setupServer(t)

	selfURI := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[selfURI] = "[[Go"
	items, err := srv.completion(selfURI, position{Line: 0, Character: 4})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("a note should not complete its own terms, got %+v", items)
	}

	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "plain text"
	outside, err := srv.completion(uri, position{Line: 0, Character: 5})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if len(outside) != 0 {
		t.Fatalf("expected no completion outside [[, got %+v", outside)
	}
}

func TestCompletionOffersCreateNoteWhenNoKeywordMatches(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Rust"

	items, err := srv.completion(uri, position{Line: 0, Character: 10})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	var create *completionItem
	for i := range items {
		if items[i].Command != nil && items[i].Command.Command == createNoteCommand {
			create = &items[i]
			break
		}
	}
	if create == nil {
		t.Fatalf("expected create-note completion item, got %+v", items)
	}
	if create.Label != "Rust" || create.FilterText != "Rust" || create.InsertText != "Rust" || create.TextEdit == nil || create.TextEdit.NewText != "Rust]]" {
		t.Fatalf("unexpected create item: %+v", create)
	}

	srv.docs[uri] = "see [[Go"
	matched, err := srv.completion(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	for _, item := range matched {
		if item.Command != nil && item.Command.Command == createNoteCommand {
			t.Fatalf("did not expect create-note item when a keyword matches, got %+v", matched)
		}
	}
}

func TestCompletionDoesNotDuplicateExistingClose(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go]]"

	items, err := srv.completion(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	var goItem *completionItem
	for i := range items {
		if items[i].Label == "Go" {
			goItem = &items[i]
			break
		}
	}
	if goItem == nil || goItem.TextEdit == nil {
		t.Fatalf("expected Go completion item, got %+v", items)
	}
	if goItem.TextEdit.NewText != "Go" {
		t.Fatalf("completion should not duplicate existing close, got %+v", goItem.TextEdit)
	}
}

func TestCompletionResponseIsIncomplete(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [["
	params, err := json.Marshal(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     position{Line: 0, Character: 6},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp := srv.handleRequest(rpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "textDocument/completion",
		Params:  params,
	})
	if resp.Error != nil {
		t.Fatalf("completion response error: %+v", resp.Error)
	}
	list, ok := resp.Result.(completionList)
	if !ok {
		t.Fatalf("expected completionList, got %T", resp.Result)
	}
	if !list.IsIncomplete {
		t.Fatalf("completion list should be incomplete so cmp re-queries after additional typing")
	}
}

func TestCodeActionCreatesUnresolvedNote(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Rust]]"

	actions, err := srv.codeActions(uri, rangeValue{
		Start: position{Line: 0, Character: 6},
		End:   position{Line: 0, Character: 6},
	})
	if err != nil {
		t.Fatalf("code actions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one create-note action, got %+v", actions)
	}
	if actions[0].Command == nil || actions[0].Command.Command != createNoteCommand {
		t.Fatalf("unexpected action: %+v", actions[0])
	}

	arg, err := jsonMarshalRaw(actions[0].Command.Arguments[0])
	if err != nil {
		t.Fatalf("marshal action arg: %v", err)
	}
	result, err := srv.executeCommand(executeCommandParams{
		Command:   createNoteCommand,
		Arguments: []json.RawMessage{arg},
	})
	if err != nil {
		t.Fatalf("execute create note: %v", err)
	}
	if result["title"] != "Rust" {
		t.Fatalf("unexpected create result: %+v", result)
	}

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 1 || links[0].Target != result["uri"] {
		t.Fatalf("expected newly created note to resolve, links=%+v result=%+v", links, result)
	}

	again, err := srv.codeActions(uri, rangeValue{
		Start: position{Line: 0, Character: 6},
		End:   position{Line: 0, Character: 6},
	})
	if err != nil {
		t.Fatalf("code actions after create: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("resolved link should not offer create action, got %+v", again)
	}
}

func TestServeEndsCleanlyOnPartialMessage(t *testing.T) {
	srv, _ := setupServer(t)
	// The header promises more bytes than the stream delivers, like Neovim closing stdin mid shutdown.
	err := srv.Serve(strings.NewReader("Content-Length: 100\r\n\r\n{}"), io.Discard)
	if err != nil {
		t.Fatalf("a partial message should end the session cleanly, got %v", err)
	}
}

func TestServeEndsCleanlyOnEOF(t *testing.T) {
	srv, _ := setupServer(t)
	if err := srv.Serve(strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("EOF should end cleanly, got %v", err)
	}
}

func TestIsDisconnect(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, os.ErrClosed, syscall.EPIPE} {
		if !isDisconnect(err) {
			t.Fatalf("%v should count as a disconnect", err)
		}
	}
	if isDisconnect(errors.New("boom")) {
		t.Fatalf("a generic error should not count as a disconnect")
	}
}

func jsonMarshalRaw(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}
