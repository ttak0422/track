package lsp

import (
	"path/filepath"
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
	srv.docs[uri] = "Golang and Go"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %+v", links)
	}
	if links[0].Range.Start.Line != 0 || links[0].Range.Start.Character != 0 || links[0].Range.End.Character != 6 {
		t.Fatalf("unexpected first range: %+v", links[0].Range)
	}
	if links[0].Target != uriFromPath(filepath.Join(vault, "100.md")) {
		t.Fatalf("unexpected target: %q", links[0].Target)
	}
}

func TestDefinition(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "mentions Golang"

	loc, err := srv.definition(uri, position{Line: 0, Character: 10})
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
	srv.docs[uri] = "# Go\n\nGo"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no self-links, got %+v", links)
	}

	loc, err := srv.definition(uri, position{Line: 2, Character: 1})
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
	srv.docs[uri] = "# Go"

	links, err := srv.documentLinks(uri)
	if err != nil {
		t.Fatalf("document links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected title link to another note, got %+v", links)
	}
}
