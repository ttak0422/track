package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// capture redirects stdout while fn runs and returns what it printed.
func capture(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), code
}

// run sets up an isolated vault for one Run invocation.
func runIn(t *testing.T, vault string, args ...string) (map[string]any, int) {
	t.Helper()
	t.Setenv("TRACK_VAULT", vault)
	out, code := capture(t, func() int { return Run(args) })
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %q (err %v)", out, err)
	}
	return decoded, code
}

func TestVersion(t *testing.T) {
	out, code := capture(t, func() int { return Run([]string{"version"}) })
	if code != 0 || out != "track "+Version+"\n" {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, code := capture(t, func() int { return Run([]string{"bogus"}) })
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}

func TestNewResolveKeywordsFlow(t *testing.T) {
	vault := t.TempDir()

	created, code := runIn(t, vault, "new", "--title", "リンク", "--id", "1000")
	if code != 0 {
		t.Fatalf("new failed: %v", created)
	}
	if created["id"].(float64) != 1000 {
		t.Fatalf("unexpected id: %v", created["id"])
	}
	noteContent, err := os.ReadFile(vault + "/1000.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(noteContent), "<!--track") {
		t.Fatalf("note file should not contain metadata: %q", noteContent)
	}
	metaContent, err := os.ReadFile(vault + "/.track/notes/1000.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaContent), "version: 1") || !strings.Contains(string(metaContent), "title: リンク") {
		t.Fatalf("unexpected metadata content: %q", metaContent)
	}

	kws, code := runIn(t, vault, "keywords")
	if code != 0 {
		t.Fatalf("keywords failed: %v", kws)
	}
	list := kws["keywords"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["term"] != "リンク" {
		t.Fatalf("unexpected keywords: %v", list)
	}

	res, code := runIn(t, vault, "resolve", "--term", "リンク")
	if code != 0 || res["found"] != true {
		t.Fatalf("resolve failed: %v", res)
	}
	if res["note_id"].(float64) != 1000 {
		t.Fatalf("resolve note_id: %v", res["note_id"])
	}

	missing, _ := runIn(t, vault, "resolve", "--term", "なし")
	if missing["found"] != false {
		t.Fatalf("expected found=false, got %v", missing)
	}
}

func TestNewRequiresTitle(t *testing.T) {
	out, code := runIn(t, t.TempDir(), "new")
	if code != 1 || !strings.Contains(out["error"].(string), "title") {
		t.Fatalf("expected title error, got code=%d out=%v", code, out)
	}
}

func TestRequiresTrackVault(t *testing.T) {
	t.Setenv("TRACK_VAULT", "")
	out, code := capture(t, func() int { return Run([]string{"keywords"}) })
	if code != 1 {
		t.Fatalf("expected exit 1 without TRACK_VAULT, got %d", code)
	}
	if !strings.Contains(out, "TRACK_VAULT is required") {
		t.Fatalf("expected TRACK_VAULT error, got %q", out)
	}
}

func TestBacklinksAndReindex(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Go", "--id", "100")
	runIn(t, vault, "new", "--title", "Other", "--id", "200")

	// Make note 200 reference Go, then full reindex to build the link graph.
	if err := os.WriteFile(vault+"/200.md",
		[]byte("[[Go]] を参照\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, code := runIn(t, vault, "reindex", "--full")
	if code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}
	if rep["links"].(float64) != 1 {
		t.Fatalf("expected 1 link, got %v", rep["links"])
	}

	back, code := runIn(t, vault, "backlinks", "--id", "100")
	if code != 0 {
		t.Fatalf("backlinks failed: %v", back)
	}
	list := back["backlinks"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["note_id"].(float64) != 200 {
		t.Fatalf("expected note 200 backlink, got %v", list)
	}
}

func TestReindexReconcilesMetadataTitleFromBody(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Old", "--id", "100")

	if err := os.WriteFile(vault+"/100.md", []byte("# New\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, code := runIn(t, vault, "reindex", "--full")
	if code != 0 {
		t.Fatalf("reindex failed: %v", rep)
	}

	kws, code := runIn(t, vault, "keywords")
	if code != 0 {
		t.Fatalf("keywords failed: %v", kws)
	}
	list := kws["keywords"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["term"] != "New" {
		t.Fatalf("expected reconciled keyword New, got %v", list)
	}
	metaContent, err := os.ReadFile(vault + "/.track/notes/100.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaContent), "title: New") {
		t.Fatalf("expected metadata title to be rewritten, got %q", metaContent)
	}
}

func TestJournalIdempotent(t *testing.T) {
	vault := t.TempDir()
	first, code := runIn(t, vault, "journal", "--offset", "0")
	if code != 0 || first["created"] != true {
		t.Fatalf("first journal: %v", first)
	}
	path := first["path"].(string)
	if filepath.Dir(path) != filepath.Join(vault, "journal") {
		t.Fatalf("journal path should be under journal dir, got %q", path)
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if len(name) != 8 || first["id"].(float64) == 0 {
		t.Fatalf("journal name/id should be yyyyMMdd, got name=%q id=%v", name, first["id"])
	}
	second, _ := runIn(t, vault, "journal", "--offset", "0")
	if second["created"] != false {
		t.Fatalf("second journal should not recreate, got %v", second)
	}
	if first["id"] != second["id"] {
		t.Fatalf("journal id changed between calls: %v vs %v", first["id"], second["id"])
	}
	if first["path"] != second["path"] {
		t.Fatalf("journal path changed between calls: %v vs %v", first["path"], second["path"])
	}
}

func TestSearch(t *testing.T) {
	vault := t.TempDir()
	runIn(t, vault, "new", "--title", "Golang notes", "--id", "300")
	res, code := runIn(t, vault, "search", "--query", "Golang")
	if code != 0 {
		t.Fatalf("search failed: %v", res)
	}
	if len(res["results"].([]any)) != 1 {
		t.Fatalf("expected 1 result, got %v", res["results"])
	}
}

func TestBabelExecRunsAndStores(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	if _, code := runIn(t, vault, "new", "--title", "Demo", "--id", "500"); code != 0 {
		t.Fatal("new failed")
	}
	body := "# Demo\n\n```sh :name hi :results output\necho hello\n```\n"
	if err := os.WriteFile(vault+"/500.md", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "babel", "exec", "--id", "500", "--name", "hi")
	if code != 0 {
		t.Fatalf("babel exec failed: %v", out)
	}
	if out["status"] != "success" || out["stdout"] != "hello\n" || out["stored"] != true {
		t.Fatalf("unexpected result: %v", out)
	}

	metaContent, err := os.ReadFile(vault + "/.track/notes/500.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaContent), "version: 2") || !strings.Contains(string(metaContent), "hi:") {
		t.Fatalf("sidecar should store the block result at v2: %q", metaContent)
	}

	restored, code := runIn(t, vault, "babel", "restore", "--id", "500")
	if code != 0 {
		t.Fatalf("babel restore failed: %v", restored)
	}
	restoredBlocks := restored["blocks"].([]any)
	if len(restoredBlocks) != 1 {
		t.Fatalf("expected one restored block, got %v", restoredBlocks)
	}
	restoredBlock := restoredBlocks[0].(map[string]any)
	if restoredBlock["id"] != "hi" || restoredBlock["stdout"] != "hello\n" || restoredBlock["restored"] != true {
		t.Fatalf("unexpected restored block: %v", restoredBlock)
	}
	if int(restoredBlock["end_line"].(float64)) != 4 {
		t.Fatalf("expected restored end_line 4, got %v", restoredBlock["end_line"])
	}

	if err := os.WriteFile(vault+"/500.md", []byte("# Demo\n\n```sh :name hi :results output\necho changed\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, code := runIn(t, vault, "babel", "restore", "--id", "500")
	if code != 0 {
		t.Fatalf("babel restore after edit failed: %v", stale)
	}
	if got := len(stale["blocks"].([]any)); got != 0 {
		t.Fatalf("stale result should not be restored, got %v", stale["blocks"])
	}
}

func TestBabelExecByLine(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	runIn(t, vault, "new", "--title", "Demo", "--id", "502")
	// Two blocks; the cursor row (0-based) lands inside the second one.
	body := "# Demo\n\n```sh\necho first\n```\n\n```sh\necho second\n```\n"
	if err := os.WriteFile(vault+"/502.md", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "babel", "exec", "--id", "502", "--line", "7")
	if code != 0 {
		t.Fatalf("babel exec by line failed: %v", out)
	}
	if out["stdout"] != "second\n" {
		t.Fatalf("expected the second block to run, got %v", out)
	}
	if int(out["end_line"].(float64)) != 8 {
		t.Fatalf("expected end_line 8, got %v", out["end_line"])
	}
}

func TestBabelExecRefusesEvalNo(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	vault := t.TempDir()
	t.Setenv("TRACK_BABEL_SH", "sh {{file}}")

	runIn(t, vault, "new", "--title", "D", "--id", "501")
	if err := os.WriteFile(vault+"/501.md", []byte("# D\n\n```sh :name x :eval no\necho hi\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := runIn(t, vault, "babel", "exec", "--id", "501", "--name", "x")
	if code != 1 {
		t.Fatalf("expected refusal exit 1, got %d (%v)", code, out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "eval no") {
		t.Fatalf("expected :eval no error, got %v", out)
	}
}
