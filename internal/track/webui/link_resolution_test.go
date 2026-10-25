package webui

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/link"
)

func TestResolveIDLinksUsesSourceModeAndVault(t *testing.T) {
	srv, server, main, work := twoVaultWorkspace(t)
	srv.active.cfg.LinkFormat = link.IDFormat
	writeVaultNote(t, main, 200, "100", "numeric title\n")
	writeVaultNote(t, work, 300, "id:404", "legacy prefixed title\n")
	for _, tc := range []struct {
		key, vault, title string
		found             bool
	}{
		{"id:100", "", "Alpha in main", true},
		{"main:id:100", "", "Alpha in main", true},
		{"work:id:100", "", "Alpha in work", true},
		{"100", "", "100", true},
		{"work:id:405", "", "", false},
		{"id:404", "work", "id:404", true},
		{"id:100", "work", "", false},
	} {
		got := getVaultJSON(t, server.URL+"/api/resolve?term="+url.QueryEscape(tc.key)+"&vault="+tc.vault)
		if got["found"] != tc.found {
			t.Fatalf("%s in %s: %v", tc.key, tc.vault, got)
		}
		if tc.found && got["note"].(map[string]any)["title"] != tc.title {
			t.Fatalf("%s: %v", tc.key, got)
		}
	}
	resp, err := http.Get(server.URL + "/api/resolve?term=work%3Aid%3A404")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("ambiguous remote literal title status %d", resp.StatusCode)
	}

}

func TestRenderStableIDIncludesKeepTargetVault(t *testing.T) {
	srv, server, _, work := twoVaultWorkspace(t)
	srv.active.cfg.LinkFormat = link.IDFormat
	writeVaultNote(t, work, 100, "Alpha in work", "# Section\n\nexcerpt ^block\n\n[[Alpha in work]]\n")
	resp := postBody(t, server.URL+"/api/render", map[string]string{"body": "![[work:id:100#^block|Pinned]]\n![[work:id:404|Not a fallback]]\n[external](https://example.com/id:100)\n"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render status %d", resp.StatusCode)
	}
	var got struct {
		Markdown string            `json:"markdown"`
		Includes []resolvedInclude `json:"includes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Includes) != 2 {
		t.Fatalf("includes: %+v", got.Includes)
	}
	inc := got.Includes[0]
	if inc.NoteID != 100 || inc.Vault == nil || *inc.Vault != "work" || inc.Caption != "Pinned" || strings.Join(inc.Lines, "\n") != "excerpt" {
		t.Fatalf("include: %+v", inc)
	}
	if got.Includes[1].Error == "" {
		t.Fatal("missing explicit ID unexpectedly resolved")
	}
	if !strings.Contains(got.Markdown, "https://example.com/id:100") {
		t.Fatal("external URL changed")
	}
}

func TestCreateRejectsReservedIDTitleBeforeWriting(t *testing.T) {
	srv, server, _, _ := twoVaultWorkspace(t)
	srv.active.cfg.LinkFormat = link.IDFormat
	resp := postBody(t, server.URL+"/api/note", map[string]string{"title": "id:404"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestExternalIDBacklinksUseSourceModeAndDeduplicate(t *testing.T) {
	srv, server, _, work := twoVaultWorkspace(t)
	other, err := srv.viewByName("work")
	if err != nil {
		t.Fatal(err)
	}
	other.cfg.LinkFormat = link.IDFormat
	writeVaultNote(t, work, 200, "Referrer", "[[main:id:100|Old title]] [[main:Alpha in main]]\n")
	got := getVaultJSON(t, server.URL+"/api/note?id=100")
	backs := got["external"].([]any)
	if len(backs) != 1 || backs[0].(map[string]any)["note_id"] != float64(200) {
		t.Fatalf("backlinks: %+v", backs)
	}
}

func TestPersistentIdentityVaultRejectsSyncedReservedTitle(t *testing.T) {
	srv, server, main, _ := twoVaultWorkspace(t)
	srv.active.cfg.LinkFormat = link.IDFormat
	getVaultJSON(t, server.URL+"/api/resolve?term=id%3A100")
	writeVaultNote(t, main, 999, "id:100", "synced collision\n")
	resp, err := http.Get(server.URL + "/api/resolve?term=id%3A100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("synced collision was served from stale index: %d", resp.StatusCode)
	}
}

func TestExternalBacklinksOmitAmbiguousTypedTarget(t *testing.T) {
	srv, server, main, work := twoVaultWorkspace(t)
	other, err := srv.viewByName("work")
	if err != nil {
		t.Fatal(err)
	}
	other.cfg.LinkFormat = link.IDFormat
	writeVaultNote(t, work, 200, "Referrer", "[[main:id:100|Old title]]\n")
	writeVaultNote(t, main, 300, "id:100", "legacy collision\n")
	got := getVaultJSON(t, server.URL+"/api/note?id=100")
	backs := got["external"].([]any)
	if len(backs) != 0 {
		t.Fatalf("ambiguous typed backlink appeared: %+v", backs)
	}
}
