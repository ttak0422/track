package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/site"
)

// fakeFrontend creates a minimal static-mode frontend build for export-site to copy.
func fakeFrontend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><head><title>track</title></head><body><div id=\"root\"></div></body>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// readBundle opens one file of the published data bundle. It is locked (ADR 0069): the published file is
// "<name>.bin", and reading it takes the site's key, derived from the site's own public identity (its
// base URL and its root note's title) and baked into the pages the export writes.
func readBundle(t *testing.T, out, baseURL, rootTitle, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, "data", filepath.FromSlash(name)+".bin"))
	if err != nil {
		t.Fatalf("bundle file missing: %v", err)
	}
	plain, err := site.Unlock(site.LockKey(baseURL, rootTitle), raw)
	if err != nil {
		t.Fatalf("unlock %s: %v", name, err)
	}
	return plain
}

func TestExportSiteBuildsStaticSite(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Home", "--id", "100", "--body", "# Home\n\ngo to [[Child]]\n"); code != 0 {
		t.Fatalf("new Home failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Child", "--id", "200", "--body", "# Child\n\nback [[Home]]\n"); code != 0 {
		t.Fatalf("new Child failed")
	}

	out := filepath.Join(vault, "site")
	res, code := runIn(t, vault, "export-site", "--root", "100", "--id", "200", "--frontend", fakeFrontend(t), "--out", out)
	if code != 0 {
		t.Fatalf("export-site failed: %v", res)
	}
	if res["out"] != out {
		t.Fatalf("unexpected out: %v", res)
	}

	// Frontend copied and data bundle generated.
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Fatalf("frontend not copied: %v", err)
	}
	raw := readBundle(t, out, "", "Home", "note/"+site.PublishID(200))
	var note struct {
		Note struct {
			Body string `json:"body"`
		} `json:"note"`
		Backlinks []struct {
			NoteID string `json:"note_id"`
		} `json:"backlinks"`
	}
	if err := json.Unmarshal(raw, &note); err != nil {
		t.Fatal(err)
	}
	if len(note.Backlinks) != 1 || note.Backlinks[0].NoteID != site.PublishID(100) {
		t.Fatalf("child should have backlink from 100, got %+v", note.Backlinks)
	}
	if !strings.Contains(note.Note.Body, "[[Home]]") {
		t.Fatalf("child body should keep wiki link: %q", note.Note.Body)
	}

	// A crawlable per-note HTML page is written with the note's own OGP head.
	childPage, err := os.ReadFile(filepath.Join(out, "notes", site.PublishID(200), "index.html"))
	if err != nil {
		t.Fatalf("per-note page not written: %v", err)
	}
	if !strings.Contains(string(childPage), `<meta property="og:title" content="Child">`) {
		t.Fatalf("child page should carry its own og:title: %s", childPage)
	}
	metaRaw := readBundle(t, out, "", "Home", "site")
	if strings.Contains(string(metaRaw), `"share"`) {
		t.Fatalf("share should be opt-in by default: %s", metaRaw)
	}
}

func TestExportSiteShareOption(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Home", "--id", "100", "--body", "# Home\n"); code != 0 {
		t.Fatalf("new Home failed")
	}

	frontend := fakeFrontend(t)
	withoutBase := filepath.Join(vault, "without-base")
	res, code := runIn(t, vault, "export-site", "--root", "100", "--share", "--frontend", frontend, "--out", withoutBase)
	if code != 1 || !strings.Contains(res["error"].(string), "--share requires --base-url") {
		t.Fatalf("expected --share to require --base-url, got code=%d out=%v", code, res)
	}

	out := filepath.Join(vault, "site")
	res, code = runIn(t, vault, "export-site", "--root", "100", "--share", "--base-url", "https://example.com/blog", "--frontend", frontend, "--out", out)
	if code != 0 {
		t.Fatalf("export-site with sharing failed: %v", res)
	}
	raw := readBundle(t, out, "https://example.com/blog", "Home", "site")
	var meta struct {
		BaseURL string `json:"base_url"`
		Share   bool   `json:"share"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.BaseURL != "https://example.com/blog" || !meta.Share {
		t.Fatalf("share option should be published with its base URL, got %+v", meta)
	}
}

func TestExportSiteRequiresRoot(t *testing.T) {
	vault := t.TempDir()
	out, code := runIn(t, vault, "export-site", "--frontend", fakeFrontend(t), "--out", filepath.Join(vault, "site"))
	if code != 1 || !strings.Contains(out["error"].(string), "root") {
		t.Fatalf("expected --root required error, got code=%d out=%v", code, out)
	}
}

// A vault that names its landing note needs no --root: a site's front door does not change per
// deployment, so it belongs with the content (ADR 0049's rule, now applied to vault mode too).
func TestExportSiteRootDefaultsToVaultHome(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Home", "--id", "100", "--body", "# Home\n"); code != 0 {
		t.Fatalf("new Home failed")
	}
	writeVaultConfig(t, vault, "web:\n  home: Home\n")

	out := filepath.Join(vault, "site")
	res, code := runIn(t, vault, "export-site", "--frontend", fakeFrontend(t), "--out", out)
	if code != 0 {
		t.Fatalf("export-site failed: %v", res)
	}
	raw := readBundle(t, out, "", "Home", "site")
	var meta struct {
		Root string `json:"root"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Root != site.PublishID(100) {
		t.Fatalf("web.home should have become the site root, got %q", meta.Root)
	}
}

// --all is how a vault publishes what a directory published: every note in it, with no id list to
// keep in step with the vault.
func TestExportSiteAllPublishesEveryNote(t *testing.T) {
	vault := t.TempDir()
	for _, n := range []struct{ id, title string }{{"100", "Home"}, {"200", "Second"}, {"300", "Third"}} {
		if _, code := runIn(t, vault, "new", "--title", n.title, "--id", n.id, "--body", "# "+n.title+"\n"); code != 0 {
			t.Fatalf("new %s failed", n.title)
		}
	}

	out := filepath.Join(vault, "site")
	res, code := runIn(t, vault, "export-site", "--all", "--root", "100", "--frontend", fakeFrontend(t), "--out", out)
	if code != 0 {
		t.Fatalf("export-site failed: %v", res)
	}
	if got := len(res["notes"].([]any)); got != 3 {
		t.Fatalf("--all should publish the three notes and no journal hub, got %d: %v", got, res)
	}

	// Saying both "all of them" and "these two" means one of the two was a mistake.
	res, code = runIn(t, vault, "export-site", "--all", "--id", "200", "--root", "100", "--frontend", fakeFrontend(t), "--out", filepath.Join(vault, "site2"))
	if code != 1 || !strings.Contains(res["error"].(string), "--all publishes every note") {
		t.Fatalf("expected --all with --id to be refused, got code=%d out=%v", code, res)
	}
}

// writeVaultConfig writes <vault>/.track/config.yml, the vault-scope config (ADR 0050).
func writeVaultConfig(t *testing.T, vault, body string) {
	t.Helper()
	dir := filepath.Join(vault, ".track")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
