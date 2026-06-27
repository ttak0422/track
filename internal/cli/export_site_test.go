package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportSiteBuildsStaticSite(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "new", "--title", "Home", "--id", "100", "--body", "# Home\n\ngo to [[Child]]\n"); code != 0 {
		t.Fatalf("new Home failed")
	}
	if _, code := runIn(t, vault, "new", "--title", "Child", "--id", "200", "--body", "# Child\n\nback [[Home]]\n"); code != 0 {
		t.Fatalf("new Child failed")
	}

	out := filepath.Join(vault, "site")
	res, code := runIn(t, vault, "export-site", "--root", "100", "--id", "200", "--out", out)
	if code != 0 {
		t.Fatalf("export-site failed: %v", res)
	}
	if res["out"] != out {
		t.Fatalf("unexpected out: %v", res)
	}

	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `href="200.html"`) {
		t.Fatalf("index should link to child page:\n%s", index)
	}
	if _, err := os.Stat(filepath.Join(out, "200.html")); err != nil {
		t.Fatalf("child page not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "style.css")); err != nil {
		t.Fatalf("style.css not written: %v", err)
	}
}

func TestExportSiteRequiresRoot(t *testing.T) {
	vault := t.TempDir()
	out, code := runIn(t, vault, "export-site", "--out", filepath.Join(vault, "site"))
	if code != 1 || !strings.Contains(out["error"].(string), "root") {
		t.Fatalf("expected --root required error, got code=%d out=%v", code, out)
	}
}
