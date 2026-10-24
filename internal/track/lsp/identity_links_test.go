package lsp

import (
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func TestIdentityCompletionKeepsTitleLabelAndInsertsIdentity(t *testing.T) {
	srv, _ := setupServer(t)
	srv.cfg.LinkFormat = "id-v1"
	uri := uriFromPath(srv.cfg.NotePath(200))
	srv.docs[uri] = "[[Go"
	srv.docs[uriFromPath(srv.cfg.NotePath(100))] = "## Section\n\nparagraph ^part\n"
	items, err := srv.completion(uri, position{Line: 0, Character: 4})
	if err != nil {
		t.Fatal(err)
	}
	var bare, heading bool
	for _, item := range items {
		if item.Label == "Go" {
			bare = item.InsertText == "id:100|Go"
		}
		if item.Label == "Go##Section" {
			heading = item.InsertText == "id:100##Section|Go"
		}
	}
	if !bare || !heading {
		t.Fatalf("completion: %+v", items)
	}
	srv.docs[uri] = "[[id:100##S"
	items, err = srv.completion(uri, position{Line: 0, Character: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].InsertText != "id:100##Section|Go" {
		t.Fatalf("ID heading completion: %+v", items)
	}
	srv.docs[uri] = "[[id:999"
	items, err = srv.completion(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Detail), "create") || item.Command != nil {
			t.Fatalf("must not create title for missing ID: %+v", item)
		}
	}
}

func TestIdentityDefinitionAnchorsAndStaleAlias(t *testing.T) {
	srv, _ := setupServer(t)
	srv.cfg.LinkFormat = "id-v1"
	uri := uriFromPath(srv.cfg.NotePath(200))
	targetURI := uriFromPath(srv.cfg.NotePath(100))
	srv.docs[targetURI] = "## Section\n\nparagraph ^part\n"
	srv.docs[uri] = "[[id:100##Section|Old title]]\n![[id:100#^part|Old title]]\n[[id:999|Go]]"
	if err := srv.store.UpsertNote(&note.Note{ID: 100, Meta: note.Metadata{Title: "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	for line, want := range map[uint32]uint32{0: 0, 1: 2} {
		loc, err := srv.definition(uri, position{Line: line, Character: 5})
		if err != nil || loc == nil || string(loc.URI) != targetURI || loc.Range.Start.Line != want {
			t.Fatalf("line %d: %+v %v", line, loc, err)
		}
	}
	loc, err := srv.definition(uri, position{Line: 2, Character: 5})
	if err != nil || loc != nil {
		t.Fatalf("missing ID aliased Go resolved: %+v %v", loc, err)
	}
	diags, err := srv.diagnostics(uri)
	if err != nil {
		t.Fatal(err)
	}
	var missing bool
	for _, d := range diags {
		if d.Code == diagnosticCodeUnresolvedLink {
			if d.Range.Start.Line != 2 {
				t.Fatalf("resolved ID warned: %+v", d)
			}
			missing = true
		}
	}
	if !missing {
		t.Fatalf("missing ID not diagnosed: %+v", diags)
	}
}

func TestIdentityCrossVaultCompletionAndAnchorsUseSourceMode(t *testing.T) {
	srv, path := setupCrossVault(t)
	srv.cfg.LinkFormat = "id-v1" // target vault remains legacy.
	uri := uriFromPath(srv.cfg.NotePath(100))
	srv.docs[uri] = "[[work:Rem"
	items, err := srv.completion(uri, position{Line: 0, Character: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Label != "work:Remote note" || items[0].InsertText != "work:id:200|Remote note" {
		t.Fatalf("cross completion %+v", items)
	}
	srv.docs[uri] = "[[work:id:200##S"
	items, err = srv.completion(uri, position{Line: 0, Character: 15})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].InsertText != "work:id:200##Section|Remote note" {
		t.Fatalf("cross heading %+v", items)
	}
	srv.docs[uri] = "[[work:id:200##Section|Old alias]]"
	loc, err := srv.definition(uri, position{Line: 0, Character: 10})
	if err != nil || loc == nil || string(loc.URI) != uriFromPath(path) || loc.Range.Start.Line != 2 {
		t.Fatalf("cross definition %+v %v", loc, err)
	}
}

func TestIdentityMissingIDDoesNotOfferCreationOrWriteReservedTitle(t *testing.T) {
	srv, _ := setupServer(t)
	srv.cfg.LinkFormat = "id-v1"
	uri := uriFromPath(srv.cfg.NotePath(200))
	srv.docs[uri] = "[[id:999|Go]]"
	actions, err := srv.codeActions(uri, newRange(0, 4, 0, 4))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if strings.HasPrefix(a.Title, "Create note") {
			t.Fatalf("create missing ID action: %+v", a)
		}
	}
	if _, err := srv.createNote("id:999"); err == nil {
		t.Fatal("created reserved title")
	}
}

func TestPersistentIdentityDictionaryRejectsSyncedReservedTitle(t *testing.T) {
	srv, _ := setupServer(t)
	srv.cfg.LinkFormat = "id-v1"
	if _, err := srv.keywordDict(); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(srv.cfg.MetadataPath(999), note.Metadata{Title: "id:100"}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.keywordDict(); err == nil {
		t.Fatal("synced collision served from stale dictionary")
	}
}
