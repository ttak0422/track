package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// setupCrossVault registers a second "work" vault holding one indexed note titled "Remote note"
// and returns the running server plus the expected target path.
func setupCrossVault(t *testing.T) (*Server, string) {
	t.Helper()
	srv, _ := setupServer(t)
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB_PATH", "")
	t.Setenv("TRACK_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))

	work := t.TempDir()
	srv.cfg.Vaults = map[string]string{
		"work": work,
		"off":  filepath.Join(t.TempDir(), "unmounted"),
	}

	workCfg, err := config.LoadAt(work)
	if err != nil {
		t.Fatalf("load work cfg: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(workCfg.NotePath(200)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workCfg.NotePath(200), []byte("# Top\n\n## Section\n\ndetails\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := note.WriteMetadata(workCfg.MetadataPath(200), note.Metadata{Title: "Remote note"}); err != nil {
		t.Fatal(err)
	}
	ws, err := store.Open(workCfg.DBPath)
	if err != nil {
		t.Fatalf("open work store: %v", err)
	}
	defer ws.Close()
	if err := ws.UpsertNote(&note.Note{ID: 200, Path: workCfg.NotePath(200), Meta: note.Metadata{Title: "Remote note"}}); err != nil {
		t.Fatal(err)
	}
	return srv, workCfg.NotePath(200)
}

func TestCrossVaultDefinitionAndDiagnostics(t *testing.T) {
	srv, targetPath := setupCrossVault(t)
	uri := uriFromPath(srv.cfg.NotePath(100))
	srv.docs[uri] = "see [[work:Remote note##Section]]\nand [[work:Nope]]\nand [[off:Anything]]\nand [[stranger:Thing]]\n"

	loc, err := srv.definition(uri, position{Line: 0, Character: 8})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if loc == nil || string(loc.URI) != uriFromPath(targetPath) {
		t.Fatalf("definition should jump into the work vault, got %+v", loc)
	}
	if loc.Range.Start.Line != 2 {
		t.Fatalf("anchor should land on the Section heading (line 2), got %+v", loc.Range)
	}

	diags, err := srv.diagnostics(uri)
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	byLine := map[int]diagnostic{}
	for _, d := range diags {
		byLine[int(d.Range.Start.Line)] = d
	}
	if _, ok := byLine[0]; ok {
		t.Fatalf("resolved cross-vault ref must not be diagnosed, got %+v", diags)
	}
	if d := byLine[1]; d.Code != diagnosticCodeUnresolvedLink || !strings.Contains(d.Message, `in vault "work"`) {
		t.Fatalf("missing title in a reachable vault should warn with the vault named, got %+v", d)
	}
	if d := byLine[2]; d.Code != diagnosticCodeVaultUnavailable {
		t.Fatalf("unavailable vault must be reported explicitly, got %+v", d)
	}
	if d := byLine[3]; d.Code != diagnosticCodeUnresolvedLink || strings.Contains(d.Message, "in vault") {
		t.Fatalf("unregistered prefix stays a plain unresolved local title, got %+v", d)
	}
}

func TestCrossVaultCompletionOptIn(t *testing.T) {
	srv, _ := setupCrossVault(t)
	uri := uriFromPath(srv.cfg.NotePath(100))

	// Without a vault prefix the other vault's titles must not appear.
	srv.docs[uri] = "[[Rem"
	items, err := srv.completion(uri, position{Line: 0, Character: 5})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	for _, item := range items {
		if strings.Contains(item.Label, "Remote note") && strings.Contains(item.Label, "work:") {
			t.Fatalf("cross-vault titles must be opt-in, got %+v", items)
		}
	}

	// Typing the registered prefix opts into the work vault's dictionary.
	srv.docs[uri] = "[[work:"
	items, err = srv.completion(uri, position{Line: 0, Character: 7})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	found := false
	for _, item := range items {
		if item.Label == "work:Remote note" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected work:Remote note completion, got %+v", items)
	}
}
