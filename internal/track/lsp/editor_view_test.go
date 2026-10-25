package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/editorview"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	protocol "typefox.dev/lsp"
)

func aliasFixture(t *testing.T) (*Server, string, string, string, string) {
	t.Helper()
	srv, vault := setupServer(t)
	target, source := renameFixture(t, srv, vault)
	srv.docs = map[string]string{}
	t.Setenv(editorview.Env, filepath.Join(t.TempDir(), "view"))
	a, err := editorview.Ensure(srv.cfg, "note", 100, "Go 100% #日本語")
	if err != nil {
		t.Fatal(err)
	}
	b, err := editorview.Ensure(srv.cfg, "note", 200, "Source")
	if err != nil {
		t.Fatal(err)
	}
	return srv, target, source, uriFromPath(a.EditorPath), uriFromPath(b.EditorPath)
}
func TestEditorViewIncomingIdentityAndOwningURI(t *testing.T) {
	srv, target, source, a, b := aliasFixture(t)
	if id, ok := srv.noteIDFromURI(a); !ok || id != 100 || !srv.inVault(a) {
		t.Fatalf("alias rejected %s", a)
	}
	srv.docs[a] = "# unsaved\n\n## Fresh\n"
	srv.docs[b] = "see [[Go##Fresh]]"
	got, err := srv.documentText(target)
	if err != nil || !strings.Contains(got, "Fresh") {
		t.Fatalf("unsaved identity %q %v", got, err)
	}
	loc, err := srv.definition(b, position{Line: 0, Character: 8})
	if err != nil || loc == nil || string(loc.URI) != a || loc.Range.Start.Line != 2 {
		t.Fatalf("definition %+v %v", loc, err)
	}
	refs, err := srv.references(a, position{})
	if err != nil || len(refs) != 1 || string(refs[0].URI) != b {
		t.Fatalf("references %+v %v", refs, err)
	}
	delete(srv.docs, a)
	srv.docs[target] = "# canonical owner"
	if path := srv.notePath("note", 100); uriFromPath(path) != target {
		t.Fatalf("canonical owner replaced by alias %s", path)
	}
	delete(srv.docs, target)
	srv.docs[source] = "see [[Go]]"
	delete(srv.docs, b)
	if refs, err = srv.references(a, position{}); err != nil || len(refs) != 1 || string(refs[0].URI) != source {
		t.Fatalf("canonical source owner %+v %v", refs, err)
	}
}
func TestEditorViewDuplicateOwnerRefusesRename(t *testing.T) {
	srv, target, _, a, b := aliasFixture(t)
	srv.docs[target] = "canonical unsaved"
	srv.docs[a] = "alias unsaved"
	srv.docs[b] = "see [[Go]]"
	if _, err := srv.documentText(a); err == nil {
		t.Fatal("duplicate buffers silently selected")
	}
	if _, err := srv.rename(b, position{Line: 0, Character: 8}, "Renamed"); err == nil {
		t.Fatal("ambiguous rename allowed")
	}
	meta, _, _ := note.ReadMetadata(srv.cfg.MetadataPath(100))
	if meta.Title != "Go" {
		t.Fatal("metadata mutated")
	}
	delete(srv.docs, target)
	if got, err := srv.documentText(a); err != nil || got != "alias unsaved" {
		t.Fatalf("ownership not recovered %q %v", got, err)
	}
}
func TestEditorViewRenameEditsOpenAliasWithoutDiskWrite(t *testing.T) {
	srv, _, source, a, b := aliasFixture(t)
	srv.docs[a] = "target"
	srv.docs[b] = "prefix unsaved [[Go]]"
	edit, err := srv.rename(b, position{Line: 0, Character: 18}, "Renamed")
	if err != nil || edit == nil {
		t.Fatalf("rename %+v %v", edit, err)
	}
	if len(edit.Changes[documentURI(b)]) != 1 {
		t.Fatalf("wrong workspace URI %+v", edit)
	}
	path, _ := pathFromURI(source)
	raw, _ := os.ReadFile(path)
	if string(raw) != "see [[Go]] here" {
		t.Fatalf("open canonical file overwritten %q", raw)
	}
	if path := srv.notePath("note", 100); uriFromPath(path) != a {
		t.Fatal("title rename changed pinned alias")
	}
}
func TestEditorViewFindsNewUnsavedBacklink(t *testing.T) {
	srv, _, _, a, _ := aliasFixture(t)
	path := srv.cfg.NotePath(300)
	os.WriteFile(path, []byte("no link on disk"), 0600)
	note.WriteMetadata(srv.cfg.MetadataPath(300), note.Metadata{Title: "New source"})
	index.New(srv.cfg, srv.store).One(path)
	e, err := editorview.Ensure(srv.cfg, "note", 300, "New source")
	if err != nil {
		t.Fatal(err)
	}
	uri := uriFromPath(e.EditorPath)
	srv.docs[uri] = "unsaved [[Go]]"
	refs, err := srv.references(a, position{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range refs {
		if string(ref.URI) == uri {
			found = true
		}
	}
	if !found {
		t.Fatalf("new unsaved edge missing %+v", refs)
	}
}
func TestEditorViewDidSaveCanonicalIndexAndPercentURI(t *testing.T) {
	srv, target, _, a, _ := aliasFixture(t)
	path, _ := pathFromURI(a)
	roundTrip := uriFromPath(path)
	if roundTrip != a || !strings.Contains(path, "100% #") {
		t.Fatalf("URI changed %s %s", a, path)
	}
	for i := 0; i < 2; i++ {
		os.WriteFile(path, []byte("saved body"), 0600)
		raw, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": a}, "text": "saved body"})
		notes := srv.handleNotification(rpcMessage{Method: "textDocument/didSave", Params: raw})
		for _, n := range notes {
			if n.Method == "window/showMessage" {
				t.Fatalf("save warning %s", n.Params)
			}
		}
	}
	refs, err := srv.store.AllNotes()
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := pathFromURI(target)
	found := false
	for _, ref := range refs {
		if ref.NoteID == 100 {
			found = true
			if ref.Path != "" && ref.Path != canonical {
				t.Fatalf("alias indexed %+v", ref)
			}
		}
	}
	if !found {
		t.Fatal("saved note not indexed")
	}
}
func TestEditorViewStaleVersionAndRecoveryWarning(t *testing.T) {
	srv, _, _, a, _ := aliasFixture(t)
	opened, _ := json.Marshal(protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: documentURI(a), Version: 3, Text: "newer"}})
	srv.handleNotification(rpcMessage{Method: "textDocument/didOpen", Params: opened})
	change, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": a, "version": 2}, "contentChanges": []map[string]any{{"text": "stale"}}})
	srv.handleNotification(rpcMessage{Method: "textDocument/didChange", Params: change})
	if srv.docs[a] != "newer" {
		t.Fatal("stale change accepted")
	}
	path, _ := pathFromURI(a)
	os.Remove(path)
	os.WriteFile(path, []byte("recovery"), 0600)
	save, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": a}})
	notifications := srv.handleNotification(rpcMessage{Method: "textDocument/didSave", Params: save})
	if len(notifications) != 1 || notifications[0].Method != "window/showMessage" || !strings.Contains(string(notifications[0].Params), "recovery conflict") {
		t.Fatalf("missing recovery warning %+v", notifications)
	}
	delete(srv.docs, a)
	srv.notePath("note", 100)
	if len(srv.pendingWarnings) == 0 {
		t.Fatal("fallback hid alias failure")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "recovery" {
		t.Fatal("recovery overwritten")
	}
}
func TestEditorViewRejectsUnownedAliasAndRemoteURI(t *testing.T) {
	srv, _, _, a, _ := aliasFixture(t)
	path, _ := pathFromURI(a)
	unknown := filepath.Join(filepath.Dir(path), "Fake--100.md")
	os.Symlink(srv.cfg.NotePath(100), unknown)
	if srv.inVault(uriFromPath(unknown)) {
		t.Fatal("unowned alias admitted")
	}
	if _, err := pathFromURI("file://untrusted.example/tmp/100.md"); err == nil {
		t.Fatal("remote file authority admitted")
	}
}

func TestReviewCrossVaultReadableAnchor(t *testing.T) {
	srv, _ := setupCrossVault(t)
	t.Setenv(editorview.Env, filepath.Join(t.TempDir(), "view"))
	uri := uriFromPath(srv.cfg.NotePath(100))
	srv.docs[uri] = "see [[work:Remote note##Section]]\n"
	loc, err := srv.definition(uri, position{Line: 0, Character: 8})
	if err != nil || loc == nil {
		t.Fatalf("definition=%+v err=%v", loc, err)
	}
	if loc.Range.Start.Line != 2 {
		t.Fatalf("expected line 2; got %+v", loc)
	}
}
func TestReviewRenameSelfQualifiedStableLink(t *testing.T) {
	srv, vault := setupServer(t)
	target, source := renameFixture(t, srv, vault)
	srv.cfg.LinkFormat = "id-v1"
	srv.cfg.VaultName = "home"
	srv.cfg.Vaults = map[string]string{"home": vault}
	srv.docs[source] = "see [[home:id:100|Old display]]"
	srv.docs[target] = "target"
	edit, err := srv.rename(source, position{Line: 0, Character: 10}, "New")
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := note.ReadMetadata(srv.cfg.MetadataPath(100))
	if err != nil || meta.Title != "New" {
		t.Fatalf("meta=%+v err=%v", meta, err)
	}
	if edit != nil {
		t.Fatalf("stable link must be untouched; edits=%+v", edit)
	}
	raw, _ := os.ReadFile(srv.cfg.NotePath(200))
	t.Logf("disk=%q", raw)
}
func TestReviewCanonicalRetargetDoesNotRenameOtherNote(t *testing.T) {
	srv, vault := setupServer(t)
	target, source := renameFixture(t, srv, vault)
	srv.docs = map[string]string{target: "unsaved target"}
	path, _ := pathFromURI(target)
	other, _ := pathFromURI(source)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	edit, err := srv.rename(target, position{}, "Wrong rename")
	meta, _, _ := note.ReadMetadata(srv.cfg.MetadataPath(200))
	if meta.Title == "Wrong rename" {
		t.Fatalf("canonical path silently switched identity: edit=%+v err=%v meta=%+v", edit, err, meta)
	}
}

func TestEditorViewPreservesRawClientURIEscapes(t *testing.T) {
	srv, _, _, a, b := aliasFixture(t)
	lower := regexp.MustCompile(`%[0-9A-F]{2}`).ReplaceAllStringFunc(a, strings.ToLower)
	raw, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": lower, "version": 1, "text": "body"}})
	srv.handleNotification(rpcMessage{Method: "textDocument/didOpen", Params: raw})
	srv.docs[b] = "[[Go]]"
	loc, err := srv.definition(b, position{Line: 0, Character: 3})
	if err != nil || loc == nil || string(loc.URI) != lower {
		t.Fatalf("client URI lost: got %+v err %v want %s", loc, err, lower)
	}
}
