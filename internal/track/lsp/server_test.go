package lsp

import (
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

	loc, err := srv.definition(uri, position{Line: 0, Character: 13})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if loc == nil {
		t.Fatal("expected definition")
	}
	if loc.URI != uriFromPath(filepath.Join(vault, "100.md")) {
		t.Fatalf("unexpected definition uri: %q", loc.URI)
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
