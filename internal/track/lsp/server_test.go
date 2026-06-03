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

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	protocol "typefox.dev/lsp"
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
		BabelLanguages: map[string]babel.Executor{
			"lua":  {},
			"viml": {},
		},
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

func TestInitializeCompletionTriggerCharacters(t *testing.T) {
	srv, _ := setupServer(t)

	resp := srv.handleRequest(rpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})
	if resp.Error != nil {
		t.Fatalf("initialize response error: %+v", resp.Error)
	}
	result, ok := resp.Result.(protocol.InitializeResult)
	if !ok {
		t.Fatalf("expected initialize result, got %T", resp.Result)
	}
	triggers := result.Capabilities.CompletionProvider.TriggerCharacters
	for _, want := range []string{"[", "#", ":", " "} {
		if !stringSliceContains(triggers, want) {
			t.Fatalf("expected completion trigger %q in %+v", want, triggers)
		}
	}
}

func TestDiagnosticsWarnOnH1OutsideFirstLine(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "# Title\n\n# Later\n\n## Section\n\n# Another\n"

	diags, err := srv.diagnostics(uri)
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if len(diags) != 2 {
		t.Fatalf("expected warnings for h1 headings outside the first line, got %+v", diags)
	}
	if diags[0].Severity != protocol.SeverityWarning || diags[0].Code != diagnosticCodeH1TitleLine {
		t.Fatalf("unexpected diagnostic metadata: %+v", diags[0])
	}
	if diags[0].Range.Start.Line != 2 || diags[0].Range.End.Character != 7 {
		t.Fatalf("expected later h1 line range, got %+v", diags[0].Range)
	}
	if diags[1].Range.Start.Line != 6 {
		t.Fatalf("expected later h1 on line 6, got %+v", diags[1].Range)
	}
}

func TestDiagnosticsWarnWhenFirstH1IsNotOnFirstLine(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "\n# Title\n"

	diags, err := srv.diagnostics(uri)
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("expected the off-first-line h1 to be warned, got %+v", diags)
	}
	if diags[0].Range.Start.Line != 1 {
		t.Fatalf("expected h1 on line 1 to be warned, got %+v", diags[0].Range)
	}
}

func TestDiagnosticsIgnoreH1InsideFences(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "# Title\n\n```markdown\n# Example\n```\n\n## Section\n"

	diags, err := srv.diagnostics(uri)
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("fenced h1 should not produce diagnostics, got %+v", diags)
	}
}

func TestNotificationPublishesDiagnosticsAndClearsThem(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	openParams, err := jsonMarshalRaw(didOpenParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        documentURI(uri),
			LanguageID: protocol.LanguageKind("markdown"),
			Version:    1,
			Text:       "# Title\n\n# Later\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	notifications := srv.handleNotification(rpcMessage{
		JSONRPC: "2.0",
		Method:  "textDocument/didOpen",
		Params:  openParams,
	})
	published := publishedDiagnostics(t, notifications)
	if string(published.URI) != uri || len(published.Diagnostics) != 1 {
		t.Fatalf("expected one published diagnostic for %q, got %+v", uri, published)
	}

	changeParams, err := jsonMarshalRaw(didChangeParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			Version:                2,
			TextDocumentIdentifier: textDocumentIdentifier{URI: documentURI(uri)},
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: "# Title\n\n## Later\n"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	notifications = srv.handleNotification(rpcMessage{
		JSONRPC: "2.0",
		Method:  "textDocument/didChange",
		Params:  changeParams,
	})
	published = publishedDiagnostics(t, notifications)
	if len(published.Diagnostics) != 0 {
		t.Fatalf("expected diagnostics to be cleared after fixing h1s, got %+v", published.Diagnostics)
	}
	body, err := json.Marshal(notifications[0])
	if err != nil {
		t.Fatalf("marshal clear diagnostics notification: %v", err)
	}
	if !strings.Contains(string(body), `"diagnostics":[]`) {
		t.Fatalf("empty diagnostics must marshal as [] for LSP clients, got %s", body)
	}
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

func TestDefinitionJumpsToHeading(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[targetURI] = "# Go\n\n## foo\n\n### bar\n\n## bar\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go##bar]] and [[Go###bar]]"

	// [[Go##bar]] resolves to the first h2 "bar" (line 6), not the h3 with the same text.
	loc, err := srv.definition(uri, newPosition(0, 8))
	if err != nil || loc == nil {
		t.Fatalf("h2 anchor definition: loc=%+v err=%v", loc, err)
	}
	if string(loc.URI) != targetURI || loc.Range.Start.Line != 6 {
		t.Fatalf("expected jump to line 6, got %+v", loc)
	}
	// [[Go###bar]] resolves to the h3 "bar" (line 4): the level disambiguates.
	loc, err = srv.definition(uri, newPosition(0, 24))
	if err != nil || loc == nil {
		t.Fatalf("h3 anchor definition: loc=%+v err=%v", loc, err)
	}
	if loc.Range.Start.Line != 4 {
		t.Fatalf("expected jump to line 4, got %+v", loc)
	}
}

func TestDefinitionHeadingFallsBackToTop(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[targetURI] = "# Go\n\n## foo\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go#missing]]"

	loc, err := srv.definition(uri, newPosition(0, 8))
	if err != nil || loc == nil {
		t.Fatalf("definition: loc=%+v err=%v", loc, err)
	}
	if string(loc.URI) != targetURI || loc.Range.Start.Line != 0 {
		t.Fatalf("missing heading should fall back to the note top, got %+v", loc)
	}
}

func TestDefinitionFollowsSameNoteHeading(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[uri] = "# Go\n\n## foo\n\nsee [[Go##foo]]"

	// A plain self-link has nowhere to go, but a heading anchor navigates within the note.
	loc, err := srv.definition(uri, newPosition(4, 8))
	if err != nil || loc == nil {
		t.Fatalf("same-note heading definition: loc=%+v err=%v", loc, err)
	}
	if string(loc.URI) != uri || loc.Range.Start.Line != 2 {
		t.Fatalf("expected jump to line 2 within the same note, got %+v", loc)
	}
}

func TestCompletionOffersHeadings(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[targetURI] = "# Go\n\n## foo\n\n## food\n\n### other\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go##fo"

	items, err := srv.completion(uri, position{Line: 0, Character: 12})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	// Only the two h2 headings prefixed "fo" qualify; the h3 is excluded by level.
	if len(items) != 2 {
		t.Fatalf("expected 2 heading candidates, got %+v", items)
	}
	edit := completionEdit(&items[0])
	if items[0].Label != "foo" || edit == nil || edit.NewText != "Go##foo]]" {
		t.Fatalf("heading completion should insert the full anchor, got %+v / %+v", items[0], edit)
	}
}

func TestCompletionHeadingLevelMatchesHashCount(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	// note "Go" holds an h1 "foobar" and an h2 "hoge".
	srv.docs[targetURI] = "# Go\n\n# foobar\n\n## hoge\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))

	// Typing a single "#" offers the h1 headings (the title "Go" plus "foobar"), not the h2.
	srv.docs[uri] = "see [[Go#"
	h1, err := srv.completion(uri, position{Line: 0, Character: 9})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(h1, "foobar") || completionLabelsContain(h1, "hoge") {
		t.Fatalf("single # should offer h1 headings only, got %+v", h1)
	}
	// The title heading ("Go", note 100's first h1) is omitted as self-evident noise.
	if completionLabelsContain(h1, "Go") {
		t.Fatalf("h1 completion should exclude the note's own title, got %+v", h1)
	}

	// Typing "##" offers the h2 heading "hoge".
	srv.docs[uri] = "see [[Go##"
	h2, err := srv.completion(uri, position{Line: 0, Character: 10})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(h2, "hoge") || completionLabelsContain(h2, "foobar") {
		t.Fatalf("double ## should offer h2 headings only, got %+v", h2)
	}
	if edit := completionEdit(&h2[0]); edit == nil || edit.NewText != "Go##hoge]]" {
		t.Fatalf("## completion should insert the full anchor, got %+v", edit)
	}
}

func TestCompletionOffersNoteAndHeadingsTogether(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[targetURI] = "# Go\n\n## foo\n\n## bar\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	// Typing just the note name (no "#") should already surface the headings as full anchors.
	srv.docs[uri] = "see [[Go"

	items, err := srv.completion(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	// The bare note and both h2 anchors are offered; the title h1 anchor (Go#Go) is omitted as noise.
	if !completionLabelsContain(items, "Go") {
		t.Fatalf("expected the bare note candidate, got %+v", items)
	}
	if !completionLabelsContain(items, "Go##foo") || !completionLabelsContain(items, "Go##bar") {
		t.Fatalf("expected heading anchors alongside the note, got %+v", items)
	}
	if completionLabelsContain(items, "Go#Go") {
		t.Fatalf("title heading anchor should be excluded, got %+v", items)
	}
	// The anchor inserts the whole [[note##heading]] target and closes the link.
	var anchor *completionItem
	for i := range items {
		if items[i].Label == "Go##foo" {
			anchor = &items[i]
			break
		}
	}
	if edit := completionEdit(anchor); edit == nil || edit.NewText != "Go##foo]]" {
		t.Fatalf("anchor should insert the full closed anchor, got %+v", edit)
	}
}

func TestCompletionDedupesDuplicateHeadings(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	// Two "## foo" sections: the link resolves to the first, so completion must not offer it twice.
	srv.docs[targetURI] = "# Go\n\n## foo\n\n## foo\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))

	// Pre-"#" stage: the bare note plus a single Go##foo anchor.
	srv.docs[uri] = "see [[Go"
	pre, err := srv.completion(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if n := countLabel(pre, "Go##foo"); n != 1 {
		t.Fatalf("expected exactly one Go##foo anchor, got %d in %+v", n, pre)
	}

	// Post-"#" stage: a single "foo" heading candidate.
	srv.docs[uri] = "see [[Go##"
	post, err := srv.completion(uri, position{Line: 0, Character: 10})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if n := countLabel(post, "foo"); n != 1 {
		t.Fatalf("expected exactly one foo heading candidate, got %d in %+v", n, post)
	}
}

func TestCompletionAnchorsOnlyForTitleKeyword(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	srv.docs[targetURI] = "# Go\n\n## foo\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	// "Golang" is an alias of note 100; typing it must not expand heading anchors (only "#" does).
	srv.docs[uri] = "see [[Gol"

	items, err := srv.completion(uri, position{Line: 0, Character: 9})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "Golang") {
		t.Fatalf("expected the alias candidate, got %+v", items)
	}
	if completionLabelsContain(items, "Golang##foo") {
		t.Fatalf("alias should not expand heading anchors before '#', got %+v", items)
	}
}

func TestCompletionExcludesTitleHeadingOnly(t *testing.T) {
	srv, vault := setupServer(t)
	targetURI := uriFromPath(filepath.Join(vault, "100.md"))
	// The first h1 "Go" is the title; a later h1 "Go" repeats it, and "intro" is a distinct h1.
	srv.docs[targetURI] = "# Go\n\n# intro\n\n# Go\n"
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go#"

	items, err := srv.completion(uri, position{Line: 0, Character: 9})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	// Only "intro" survives: every "Go" h1 is the title text and a link to it just points at the note.
	if len(items) != 1 || items[0].Label != "intro" {
		t.Fatalf("expected only the non-title h1 heading, got %+v", items)
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

func TestBabelCompletionLanguage(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```l"

	items, err := srv.completion(uri, newPosition(0, 4))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "lua") {
		t.Fatalf("expected lua language completion, got %+v", items)
	}
	edit := completionEdit(&items[0])
	if edit == nil || edit.Range.Start.Character != 3 || edit.Range.End.Character != 4 {
		t.Fatalf("unexpected language text edit: %+v", edit)
	}
}

func TestBabelCompletionHeaderKeys(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :res"

	items, err := srv.completion(uri, newPosition(0, 11))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, ":results") {
		t.Fatalf("expected :results header completion, got %+v", items)
	}
	item := completionItemByLabel(items, ":results")
	edit := completionEdit(item)
	if edit == nil || edit.NewText != ":results " || edit.Range.Start.Character != 7 || edit.Range.End.Character != 11 {
		t.Fatalf("unexpected :results text edit: %+v", edit)
	}
}

func TestBabelCompletionHeaderKeysAtColon(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :"

	items, err := srv.completion(uri, newPosition(0, 8))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	item := completionItemByLabel(items, ":eval")
	if item == nil {
		t.Fatalf("expected :eval header completion at colon, got %+v", items)
	}
	edit := completionEdit(item)
	if edit == nil || edit.NewText != ":eval " || edit.Range.Start.Character != 7 || edit.Range.End.Character != 8 {
		t.Fatalf("unexpected :eval text edit: %+v", edit)
	}
	doc := completionDocumentation(item)
	if !strings.Contains(doc, "Controls whether the block may execute") {
		t.Fatalf("expected :eval documentation, got %q", doc)
	}
}

func TestBabelCompletionHeaderValueAfterKeySpace(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :eval "

	items, err := srv.completion(uri, newPosition(0, 13))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "yes") || !completionLabelsContain(items, "no") {
		t.Fatalf("expected eval value completions, got %+v", items)
	}
	if completionLabelsContain(items, ":eval") || completionLabelsContain(items, ":results") {
		t.Fatalf("property completions should not appear before an eval value is chosen, got %+v", items)
	}
	item := completionItemByLabel(items, "no")
	doc := completionDocumentation(item)
	if !strings.Contains(doc, "Never execute this block") {
		t.Fatalf("expected eval value documentation, got %q", doc)
	}
}

func TestBabelCompletionHeaderValues(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :results o"

	items, err := srv.completion(uri, newPosition(0, 17))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "output") {
		t.Fatalf("expected output result completion, got %+v", items)
	}
	if completionLabelsContain(items, "verbatim") {
		t.Fatalf("prefix should filter result completions, got %+v", items)
	}
}

func TestBabelCompletionResultsOffersMoreValuesAfterValue(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :results output "

	items, err := srv.completion(uri, newPosition(0, 23))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, "replace") || !completionLabelsContain(items, "verbatim") {
		t.Fatalf("expected more result value completions, got %+v", items)
	}
	if completionLabelsContain(items, "output") {
		t.Fatalf("used result value should not appear again, got %+v", items)
	}
	if !completionLabelsContain(items, ":eval") {
		t.Fatalf("expected header key completions alongside more result values, got %+v", items)
	}
}

func TestBabelCompletionResultsFiltersUsedValues(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :results output replace "

	items, err := srv.completion(uri, newPosition(0, 31))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if completionLabelsContain(items, "output") || completionLabelsContain(items, "replace") {
		t.Fatalf("used result values should not appear again, got %+v", items)
	}
	if !completionLabelsContain(items, "verbatim") || !completionLabelsContain(items, ":eval") {
		t.Fatalf("expected unused result values and header keys, got %+v", items)
	}
}

func TestBabelCompletionOffersOnlyHeaderKeysAfterValue(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :eval no "

	items, err := srv.completion(uri, newPosition(0, 16))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !completionLabelsContain(items, ":results") {
		t.Fatalf("expected header keys after eval value, got %+v", items)
	}
	if completionLabelsContain(items, ":eval") {
		t.Fatalf("used header key should not appear again, got %+v", items)
	}
	if completionLabelsContain(items, "yes") || completionLabelsContain(items, "no") {
		t.Fatalf("value completions should not appear after an eval value is chosen, got %+v", items)
	}
}

func TestBabelCompletionOmitsUsedHeaderKeys(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "```lua :name test :eval query "

	items, err := srv.completion(uri, newPosition(0, 30))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if completionLabelsContain(items, ":name") || completionLabelsContain(items, ":eval") {
		t.Fatalf("used header keys should not appear again, got %+v", items)
	}
	if !completionLabelsContain(items, ":results") {
		t.Fatalf("expected unused header key completion, got %+v", items)
	}
}

func TestCompletionResponseMarshalsForLSP(t *testing.T) {
	srv, vault := setupServer(t)
	uri := uriFromPath(filepath.Join(vault, "200.md"))
	srv.docs[uri] = "see [[Go"
	params, err := json.Marshal(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: documentURI(uri)},
		Position:     newPosition(0, 8),
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
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal completion response: %v", err)
	}
	if !strings.Contains(string(body), `"textEdit":{"range"`) || !strings.Contains(string(body), `"newText":"Go]]"`) {
		t.Fatalf("completion response should contain a plain LSP TextEdit, got %s", body)
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

func TestShutdownResponseIncludesNullResult(t *testing.T) {
	srv, _ := setupServer(t)
	resp := srv.handleRequest(rpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "shutdown",
	})
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"result":null`) {
		t.Fatalf("shutdown response should include a null result, got %s", body)
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

func publishedDiagnostics(t *testing.T, notifications []rpcMessage) publishDiagnosticsParams {
	t.Helper()
	if len(notifications) != 1 {
		t.Fatalf("expected one notification, got %+v", notifications)
	}
	if notifications[0].Method != "textDocument/publishDiagnostics" {
		t.Fatalf("expected publishDiagnostics notification, got %+v", notifications[0])
	}
	var params publishDiagnosticsParams
	if err := json.Unmarshal(notifications[0].Params, &params); err != nil {
		t.Fatalf("unmarshal publish diagnostics params: %v", err)
	}
	return params
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

func completionLabelsContain(items []completionItem, label string) bool {
	return countLabel(items, label) > 0
}

func countLabel(items []completionItem, label string) int {
	n := 0
	for _, item := range items {
		if item.Label == label {
			n++
		}
	}
	return n
}

func completionItemByLabel(items []completionItem, label string) *completionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

func completionDocumentation(item *completionItem) string {
	if item == nil || item.Documentation == nil {
		return ""
	}
	switch doc := item.Documentation.Value.(type) {
	case protocol.MarkupContent:
		return doc.Value
	case string:
		return doc
	default:
		return ""
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestInVaultAllowsRealpathUnderSymlinkedVault(t *testing.T) {
	srv, vault := setupServer(t)
	parent := filepath.Dir(vault)
	link := filepath.Join(parent, "vault-link")
	if err := os.Symlink(vault, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	srv.cfg.VaultDir = link

	if !srv.inVault(uriFromPath(filepath.Join(vault, "100.md"))) {
		t.Fatalf("realpath under symlinked vault should be in vault")
	}
}
