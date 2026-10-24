package site

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
)

func TestBuildStableLinksPublishOpaqueIdentity(t *testing.T) {
	cfg, s := vaultStore(t)
	cfg.LinkFormat = link.IDFormat
	cfg.VaultName = "main"
	body := "[[id:200#Section|Old title]] [[200]] [[id:404|Missing]]\n![[id:200#^part|Excerpt]]\n\n[External](https://example.com/id:200)\n\n```track-query\nTABLE title\n```\n\n```track-query :layout list\nTABLE title\n```\n"
	body += "\n[[id:" + PublishID(200) + "|Invalid numeric ID]]\n"
	writeVaultNote(t, cfg, 100, "Home", body)
	writeVaultNoteMeta(t, cfg, 200, "# Section\n\nIncluded ^part\n", note.Metadata{Title: "Current title", Slug: "existing-page", Props: map[string]any{"up": "[[id:100]]"}})
	writeVaultNote(t, cfg, 300, "200", "numeric title\n")
	out := buildAll(t, cfg, s, 100, 200, 300)
	root := readJSON[jsonNoteResponse](t, filepath.Join(out, "data", "note", PublishID(100)+".json"))
	key := "id:" + PublishID(200)
	if !strings.Contains(root.Note.Body, "[["+key+"#Section|Old title]]") || !strings.Contains(root.Note.Body, "[[200]]") {
		t.Fatalf("body: %s", root.Note.Body)
	}
	if !strings.Contains(root.Note.Body, `"target":"`+key+`"`) || !strings.Contains(root.Note.Body, "| [["+key+"]] |") {
		t.Fatalf("generated links: %s", root.Note.Body)
	}
	if !strings.Contains(root.Note.Body, "https://example.com/id:200") {
		t.Fatal("external URL changed")
	}
	if len(root.Note.Includes) != 1 || root.Note.Includes[0].Error != "" || strings.Join(root.Note.Includes[0].Lines, "\n") != "Included" {
		t.Fatalf("includes: %+v", root.Note.Includes)
	}
	dict := readJSON[map[string]jsonRef](t, filepath.Join(out, "data", "resolve.json"))
	if dict[key].NoteID != "existing-page" || dict["200"].NoteID != PublishID(300) {
		t.Fatalf("dictionary: %+v", dict)
	}
	invalid := publishedLinkKey("id:"+PublishID(200), cfg.LinkFormat, cfg.VaultName)
	if invalid == key || dict[invalid].NoteID != "" {
		t.Fatal("invalid source ID acquired a published target")
	}
	if _, ok := dict["id:200"]; ok {
		t.Fatal("raw ID leaked into dictionary")
	}
	if _, ok := dict["id:"+PublishID(404)]; ok {
		t.Fatal("unpublished ID resolved")
	}
	if len(root.Children) != 1 || root.Children[0].NoteID != "existing-page" {
		t.Fatalf("ID hierarchy: %+v", root.Children)
	}
}

func TestPublishBodyLinksKeepsLegacyAndCodeLiteral(t *testing.T) {
	body := "[[id:200|Legacy]]\n```text\n[[id:200]]\n```\n"
	if got := publishBodyLinks(body, "", ""); got != body {
		t.Fatalf("legacy changed: %q", got)
	}
	got := publishBodyLinks(body, link.IDFormat, "")
	if !strings.Contains(got, "```text\n[[id:200]]\n```") {
		t.Fatalf("code changed: %q", got)
	}
	if !strings.Contains(got, "[[id:"+PublishID(200)+"|Legacy]]") {
		t.Fatalf("ID not published: %q", got)
	}
}
