package rename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/index"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

func identityConfig(t *testing.T) *config.Config {
	t.Helper()
	vault := t.TempDir()
	return &config.Config{
		VaultDir: vault, DBPath: filepath.Join(vault, ".track", "index.db"),
		Extensions: []string{".md"}, DateFormat: "2006-01-02", JournalDateFormat: "20060102",
		JournalOff: true, LinkFormat: "id-v1",
	}
}

func identityStore(t *testing.T, cfg *config.Config) *store.Store {
	t.Helper()
	s, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func writeIdentityNote(t *testing.T, cfg *config.Config, id int64, title, body string) {
	t.Helper()
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.NotePath(id), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(cfg.MetadataPath(id), note.Metadata{Title: title}); err != nil {
		t.Fatal(err)
	}
}

func indexedIdentityStore(t *testing.T, cfg *config.Config) *store.Store {
	t.Helper()
	s := identityStore(t, cfg)
	if _, err := index.New(cfg, s).Full(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRenamePreservesStableReferencesAndRewritesLegacyTitles(t *testing.T) {
	cfg := identityConfig(t)
	writeIdentityNote(t, cfg, 100, "Old", "# Heading\n")
	body := "[[id:100|Old]] [[id:100#Heading|alias]] [[id:100#^block|Old]] [[Old]] [[Old#Heading|label]] [Old](https://example.com/Old)\n"
	writeIdentityNote(t, cfg, 200, "Source", body)
	s := indexedIdentityStore(t, cfg)
	res, err := Do(cfg, s, 100, "New")
	if err != nil || res.BacklinksUpdated != 2 {
		t.Fatalf("rename = %+v, %v; want 2 legacy rewrites", res, err)
	}
	raw, err := os.ReadFile(cfg.NotePath(200))
	want := strings.ReplaceAll(strings.ReplaceAll(body, "[[Old]]", "[[New]]"), "[[Old#Heading|label]]", "[[New#Heading|label]]")
	if err != nil || string(raw) != want {
		t.Fatalf("source body = %q, %v; want %q", raw, err, want)
	}
	meta, found, err := note.ReadMetadata(cfg.MetadataPath(100))
	if err != nil || !found || meta.Title != "New" {
		t.Fatalf("renamed metadata = %+v, %v, %v", meta, found, err)
	}
	backlinks, err := s.Backlinks(100)
	if err != nil || len(backlinks) != 1 || backlinks[0].NoteID != 200 {
		t.Fatalf("backlinks after rename = %+v, %v", backlinks, err)
	}
}

func TestRenameStableOnlyReferencesLeavesBodyUntouched(t *testing.T) {
	cfg := identityConfig(t)
	writeIdentityNote(t, cfg, 100, "Old", "")
	body := "[[id:100|Old]]\n"
	writeIdentityNote(t, cfg, 200, "Source", body)
	oldTime := time.Unix(1234567890, 0)
	if err := os.Chtimes(cfg.NotePath(200), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	s := indexedIdentityStore(t, cfg)
	res, err := Do(cfg, s, 100, "New")
	if err != nil || res.BacklinksUpdated != 0 {
		t.Fatalf("rename = %+v, %v; want no body rewrites", res, err)
	}
	info, err := os.Stat(cfg.NotePath(200))
	if err != nil || !info.ModTime().Equal(oldTime) {
		t.Fatalf("source mtime changed on metadata-only rename: %v, %v", info, err)
	}
	raw, err := os.ReadFile(cfg.NotePath(200))
	if err != nil || string(raw) != body {
		t.Fatalf("stable source changed: %q, %v", raw, err)
	}
}

func TestRenameNumericTitleIsNotID(t *testing.T) {
	cfg := identityConfig(t)
	writeIdentityNote(t, cfg, 100, "Actual ID target", "")
	writeIdentityNote(t, cfg, 300, "100", "")
	writeIdentityNote(t, cfg, 200, "Source", "[[100]] [[id:100|Actual ID target]]\n")
	s := indexedIdentityStore(t, cfg)
	res, err := Do(cfg, s, 300, "Numeric title renamed")
	if err != nil || res.BacklinksUpdated != 1 {
		t.Fatalf("rename = %+v, %v", res, err)
	}
	raw, err := os.ReadFile(cfg.NotePath(200))
	if err != nil || string(raw) != "[[Numeric title renamed]] [[id:100|Actual ID target]]\n" {
		t.Fatalf("numeric title reference = %q, %v", raw, err)
	}
}

func TestRenameRejectsReservedAndDuplicateTitles(t *testing.T) {
	cfg := identityConfig(t)
	writeIdentityNote(t, cfg, 100, "Old", "")
	writeIdentityNote(t, cfg, 200, "Taken", "")
	s := indexedIdentityStore(t, cfg)
	for _, title := range []string{"id:200", "other:id:200", "Taken"} {
		if _, err := Do(cfg, s, 100, title); err == nil {
			t.Fatalf("rename accepted %q", title)
		}
	}
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(100))
	if err != nil || meta.Title != "Old" {
		t.Fatalf("failed rename mutated title: %+v, %v", meta, err)
	}
}

func TestMoveStableLinksQualifiesOutgoingAndIncomingTargets(t *testing.T) {
	src, dst := identityConfig(t), identityConfig(t)
	src.VaultName, dst.VaultName = "home", "work"
	registry := map[string]string{"home": src.VaultDir, "work": dst.VaultDir}
	src.Vaults, dst.Vaults = registry, registry
	writeIdentityNote(t, src, 100, "Post", "[[id:100|self]] [[home:id:100|qualified self]] [[id:101#Details|secret]] [[id:500|source collision]] [[work:id:500|destination]]\n")
	writeIdentityNote(t, src, 101, "Secret", "# Details\n")
	writeIdentityNote(t, src, 102, "Pointer", "[[id:100|old display]] [[home:id:100|qualified]] [[Post|legacy display]]\n")
	writeIdentityNote(t, src, 500, "Unrelated source", "")
	writeIdentityNote(t, dst, 500, "Destination", "")
	s := identityStore(t, src)
	res, err := Move(src, dst, s, src.NotePath(100), "work", QualifyBrokenLinks)
	if err != nil || res.BacklinksUpdated != 3 {
		t.Fatalf("move = %+v, %v; want three incoming references", res, err)
	}
	raw, err := os.ReadFile(dst.NotePath(100))
	want := "[[id:100|self]] [[id:100|qualified self]] [[home:id:101#Details|secret]] [[home:id:500|source collision]] [[work:id:500|destination]]\n"
	if err != nil || string(raw) != want {
		t.Fatalf("moved body = %q, %v; want %q", raw, err, want)
	}
	raw, err = os.ReadFile(src.NotePath(102))
	want = "[[work:id:100|old display]] [[work:id:100|qualified]] [[work:Post|legacy display]]\n"
	if err != nil || string(raw) != want {
		t.Fatalf("incoming body = %q, %v; want %q", raw, err, want)
	}
	if _, err := os.Stat(src.NotePath(100)); !os.IsNotExist(err) {
		t.Fatalf("source should be moved to trash, stat = %v", err)
	}
}

func TestMoveMixedFormatsFailsBeforeCreatingDestination(t *testing.T) {
	src, dst := identityConfig(t), identityConfig(t)
	dst.LinkFormat = "title"
	writeIdentityNote(t, src, 100, "Post", "[[id:100|self]]\n")
	if _, err := Move(src, dst, nil, src.NotePath(100), "work", RefuseBrokenLinks); err == nil || !strings.Contains(err.Error(), "link_format") {
		t.Fatalf("mixed-mode move error = %v", err)
	}
	if _, err := os.Stat(dst.NotePath(100)); !os.IsNotExist(err) {
		t.Fatalf("destination should not be written, stat = %v", err)
	}
	if _, err := os.Stat(src.DBPath); !os.IsNotExist(err) {
		t.Fatalf("rejected move should not reindex source, stat = %v", err)
	}
}

func TestMoveStableMissingTargetRespectsBrokenLinkPolicy(t *testing.T) {
	for _, policy := range []LinkPolicy{RefuseBrokenLinks, UnlinkBrokenLinks} {
		t.Run(string(policy), func(t *testing.T) {
			src, dst := identityConfig(t), identityConfig(t)
			writeIdentityNote(t, src, 100, "Post", "see [[id:101|private label]]\n")
			writeIdentityNote(t, src, 101, "Secret", "")
			s := identityStore(t, src)
			_, err := Move(src, dst, s, src.NotePath(100), "work", policy)
			if policy == RefuseBrokenLinks {
				if err == nil || !strings.Contains(err.Error(), "id:101") {
					t.Fatalf("refusal = %v; want missing stable target", err)
				}
				if _, err := os.Stat(dst.NotePath(100)); !os.IsNotExist(err) {
					t.Fatalf("refused move wrote destination: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(dst.NotePath(100))
			if err != nil || string(raw) != "see private label\n" {
				t.Fatalf("unlinked body = %q, %v", raw, err)
			}
		})
	}
}
