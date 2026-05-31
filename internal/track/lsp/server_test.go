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
	if targetString(links[0]) != uriFromPath(filepath.Join(vault, "100.md")) {
		t.Fatalf("unexpected target: %q", targetString(links[0]))
	}
}

func TestBacklinks(t *testing.T) {
	srv, vault := setupServer(t)
	sourcePath := filepath.Join(vault, "200.md")
	sourceURI := uriFromPath(sourcePath)
	if err := srv.store.UpsertNote(&note.Note{
		ID:   200,
		Path: sourcePath,
		Meta: note.Metadata{Title: "Source"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.ReplaceLinks(200, []int64{100}); err != nil {
		t.Fatal(err)
	}
	srv.docs[sourceURI] = "first [[Go]]\nsecond [[Golang]]"

	backlinks, err := srv.backlinks(uriFromPath(filepath.Join(vault, "100.md")))
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 2 {
		t.Fatalf("expected two backlink occurrences, got %+v", backlinks)
	}
	if backlinks[0].NoteID != 200 || backlinks[0].Title != "Source" || backlinks[0].Range.Start.Line != 0 {
		t.Fatalf("unexpected first backlink: %+v", backlinks[0])
	}
	if backlinks[1].Range.Start.Line != 1 || backlinks[1].Preview != "second [[Golang]]" {
		t.Fatalf("unexpected second backlink: %+v", backlinks[1])
	}
}

func TestReferences(t *testing.T) {
	srv, vault := setupServer(t)
	sourcePath := filepath.Join(vault, "200.md")
	sourceURI := uriFromPath(sourcePath)
	if err := srv.store.UpsertNote(&note.Note{
		ID:   200,
		Path: sourcePath,
		Meta: note.Metadata{Title: "Source"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.ReplaceLinks(200, []int64{100}); err != nil {
		t.Fatal(err)
	}
	srv.docs[sourceURI] = "see [[Go]]"

	refs, err := srv.references(uriFromPath(filepath.Join(vault, "100.md")), position{Line: 0, Character: 0})
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if len(refs) != 1 || string(refs[0].URI) != sourceURI || refs[0].Range.Start.Character != 4 {
		t.Fatalf("unexpected references to current note: %+v", refs)
	}

	refsFromLink, err := srv.references(sourceURI, position{Line: 0, Character: 6})
	if err != nil {
		t.Fatalf("references from link: %v", err)
	}
	if len(refsFromLink) != 1 || string(refsFromLink[0].URI) != sourceURI {
		t.Fatalf("link references should resolve to the target note's backlinks, got %+v", refsFromLink)
	}
}

func TestDefinition(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "mentions [[Golang]]"

	for _, col := range []int{9, 10, 13, 17, 18} {
		loc, err := srv.definition(uri, newPosition(0, col))
		if err != nil {
			t.Fatalf("definition at col %d: %v", col, err)
		}
		if loc == nil {
			t.Fatalf("expected definition at col %d", col)
		}
		if string(loc.URI) != uriFromPath(filepath.Join(vault, "100.md")) {
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
	if targetString(links[0]) != uriFromPath(filepath.Join(vault, "100.md")) {
		t.Fatalf("unexpected target: %q", targetString(links[0]))
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
	edit := completionEdit(goItem)
	if goItem == nil || edit == nil || edit.NewText != "Go]]" {
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
	edit := completionEdit(create)
	if create.Label != "Rust" || create.FilterText != "Rust" || create.InsertText != "Rust" || edit == nil || edit.NewText != "Rust]]" {
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
	edit := completionEdit(goItem)
	if goItem == nil || edit == nil {
		t.Fatalf("expected Go completion item, got %+v", items)
	}
	if edit.NewText != "Go" {
		t.Fatalf("completion should not duplicate existing close, got %+v", edit)
	}
}

func TestCompletionResponseIsIncomplete(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [["
	params, err := json.Marshal(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: documentURI(uri)},
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
	if err := srv.store.UpsertNote(&note.Note{
		ID:   200,
		Path: filepath.Join(vault, "200.md"),
		Meta: note.Metadata{Title: "Source"},
	}); err != nil {
		t.Fatal(err)
	}
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
	if len(links) != 1 || targetString(links[0]) != result["uri"] {
		t.Fatalf("expected newly created note to resolve, links=%+v result=%+v", links, result)
	}
	backlinks, err := srv.backlinks(result["uri"].(string))
	if err != nil {
		t.Fatalf("backlinks after create: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].NoteID != 200 {
		t.Fatalf("created note should be backlinked from note 200, got %+v", backlinks)
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

func targetString(link documentLink) string {
	if link.Target == nil {
		return ""
	}
	return string(*link.Target)
}

func completionEdit(item *completionItem) *textEdit {
	if item == nil || item.TextEdit == nil {
		return nil
	}
	edit, ok := item.TextEdit.Value.(textEdit)
	if !ok {
		return nil
	}
	return &edit
}

// TestFeaturesIgnoreFilesOutsideVault verifies the server treats markdown that is not a track note
// (outside the vault, or under .track) as inert: no links, definition, references, completion, or
// code actions. Editors commonly attach this server to all markdown, so this gate matters.
func TestFeaturesIgnoreVaultOutsiders(t *testing.T) {
	srv, vault := setupServer(t)

	outside := uriFromPath(filepath.Join(filepath.Dir(vault), "outside-readme.md"))
	dotTrack := uriFromPath(filepath.Join(vault, ".track", "notes-scratch.md"))
	nonMarkdown := uriFromPath(filepath.Join(vault, "200.txt"))
	body := "[[Go]] and [[Golang]]"

	for _, uri := range []string{outside, dotTrack, nonMarkdown} {
		srv.docs[uri] = body

		links, err := srv.documentLinks(uri)
		if err != nil || len(links) != 0 {
			t.Fatalf("documentLinks(%q) = %+v, %v; want empty", uri, links, err)
		}
		loc, err := srv.definition(uri, position{Line: 0, Character: 3})
		if err != nil || loc != nil {
			t.Fatalf("definition(%q) = %+v, %v; want nil", uri, loc, err)
		}
		refs, err := srv.references(uri, position{Line: 0, Character: 3})
		if err != nil || len(refs) != 0 {
			t.Fatalf("references(%q) = %+v, %v; want empty", uri, refs, err)
		}
		items, err := srv.completion(uri, position{Line: 0, Character: 3})
		if err != nil || len(items) != 0 {
			t.Fatalf("completion(%q) = %+v, %v; want empty", uri, items, err)
		}
		actions, err := srv.codeActions(uri, rangeValue{Start: position{Line: 0, Character: 0}, End: position{Line: 0, Character: 3}})
		if err != nil || len(actions) != 0 {
			t.Fatalf("codeActions(%q) = %+v, %v; want empty", uri, actions, err)
		}
		backs, err := srv.backlinks(uri)
		if err != nil || len(backs) != 0 {
			t.Fatalf("backlinks(%q) = %+v, %v; want empty", uri, backs, err)
		}
	}
}

// TestInVaultClassification pins the boundary cases of the vault membership check.
func TestInVaultClassification(t *testing.T) {
	srv, vault := setupServer(t)
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(vault, "100.md"), true},
		{filepath.Join(vault, "journal", "20260531.md"), true},
		{filepath.Join(vault, ".track", "x.md"), false},
		{filepath.Join(vault, "note.txt"), false},
		{filepath.Join(filepath.Dir(vault), "elsewhere.md"), false},
	}
	for _, c := range cases {
		if got := srv.inVault(uriFromPath(c.path)); got != c.want {
			t.Fatalf("inVault(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
