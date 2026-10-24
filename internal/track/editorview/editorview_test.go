package editorview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
)

func fixture(t *testing.T) *config.Config {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(base, "vault")
	if err := os.MkdirAll(filepath.Join(vault, "note"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{VaultDir: vault, Extensions: []string{".md"}}
	if err := os.WriteFile(cfg.NotePath(123), []byte("master\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(Env, filepath.Join(base, "view"))
	return cfg
}
func TestSafeTitle(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"日本語 🌸", "日本語 🌸"}, {"e\u0301", "e\u0301"}, {"é", "é"}, {"a/b\\c:*?<>|\"", "a_b_c_______"}, {"CON", "_CON"}, {".hidden", "_.hidden"}, {"con.txt", "_con.txt"}, {"..", "Untitled"}, {" \t.", "_"}, {"a\u202eb", "a_b"}, {" . ", "Untitled"}} {
		if got := SafeTitle(tc.in, 200); got != tc.want {
			t.Errorf("SafeTitle(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	got := SafeTitle(strings.Repeat("界", 100), 17)
	if !utf8.ValidString(got) || len(got) > 17 {
		t.Fatalf("bad byte boundary %q", got)
	}
}
func TestEnsurePinsAndResolves(t *testing.T) {
	cfg := fixture(t)
	e, err := Ensure(cfg, "note", 123, "日本語 100% # heading")
	if err != nil {
		t.Fatal(err)
	}
	if e.Path != cfg.NotePath(123) || !strings.HasSuffix(e.EditorPath, "日本語 100% # heading--123.md") {
		t.Fatalf("bad paths %+v", e)
	}
	resolved, err := Resolve(cfg, e.EditorPath)
	if err != nil || resolved.ID != 123 || resolved.Path != e.Path {
		t.Fatalf("resolve %+v %v", resolved, err)
	}
	next, err := Ensure(cfg, "note", 123, "Renamed")
	if err != nil || next.EditorPath != e.EditorPath || next.Title != "Renamed" {
		t.Fatalf("not pinned %+v %v", next, err)
	}
	if info, err := os.Lstat(e.EditorPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("alias replaced")
	}
	raw, _ := os.ReadFile(e.Path)
	if string(raw) != "master\n" {
		t.Fatal("master changed")
	}
}
func TestEnsureCanonicalizesSymlinkedVaultAndViewPrefixes(t *testing.T) {
	cfg := fixture(t)
	base := filepath.Dir(cfg.VaultDir)
	prefix := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(base, prefix); err != nil {
		t.Fatal(err)
	}
	linked := *cfg
	linked.VaultDir = filepath.Join(prefix, "vault")
	// The view does not exist yet; only its parent prefix is a symlink, as with /tmp on macOS.
	t.Setenv(Env, filepath.Join(prefix, "view"))
	e, err := Ensure(&linked, "note", 123, "Title")
	if err != nil {
		t.Fatal(err)
	}
	wantEditorPath := filepath.Join(base, "view", vaultKey(cfg.VaultDir), "note", "Title--123.md")
	if e.Path != cfg.NotePath(123) || e.VaultDir != cfg.VaultDir || e.EditorPath != wantEditorPath {
		t.Fatalf("noncanonical identity %+v; want vault %q and editor path %q", e, cfg.VaultDir, wantEditorPath)
	}
	target, err := os.Readlink(e.EditorPath)
	if err != nil || target != cfg.NotePath(123) {
		t.Fatalf("alias target %q, %v; want %q", target, err, cfg.NotePath(123))
	}
	for _, path := range []string{linked.NotePath(123), e.EditorPath} {
		resolved, err := Resolve(&linked, path)
		if err != nil || resolved.ID != 123 || resolved.Path != e.Path || resolved.VaultDir != cfg.VaultDir {
			t.Fatalf("resolve %q: %+v, %v", path, resolved, err)
		}
	}
	t.Setenv(Env, filepath.Join(base, "view"))
	next, err := Ensure(cfg, "note", 123, "Renamed")
	if err != nil || next.EditorPath != e.EditorPath || next.Path != e.Path || next.Title != "Renamed" {
		t.Fatalf("canonical spelling did not reuse pinned alias: %+v, %v", next, err)
	}
	other := *cfg
	other.VaultDir = filepath.Join(base, "other-vault")
	if err := os.MkdirAll(filepath.Join(other.VaultDir, "note"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other.NotePath(123), []byte("other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(&other, e.EditorPath); err == nil {
		t.Fatal("alias accepted for a different vault with the same note ID")
	}
}
func TestReplacedAliasPreserved(t *testing.T) {
	cfg := fixture(t)
	e, err := Ensure(cfg, "note", 123, "Title")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(e.EditorPath); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(e.EditorPath, []byte("recovery"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(cfg, e.EditorPath); err == nil || !strings.Contains(err.Error(), "recovery conflict") {
		t.Fatalf("missing conflict %v", err)
	}
	if _, err = Ensure(cfg, "note", 123, "New"); err == nil {
		t.Fatal("Ensure replaced recovery file")
	}
	raw, _ := os.ReadFile(e.EditorPath)
	master, _ := os.ReadFile(e.Path)
	if string(raw) != "recovery" || string(master) != "master\n" {
		t.Fatal("data changed")
	}
}
func TestMissingMasterNeverRecreated(t *testing.T) {
	cfg := fixture(t)
	e, err := Ensure(cfg, "note", 123, "Title")
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(e.Path)
	if _, err = Resolve(cfg, e.EditorPath); err == nil {
		t.Fatal("missing master accepted")
	}
	if _, err = Ensure(cfg, "note", 123, "Other"); err == nil {
		t.Fatal("missing master regenerated")
	}
	if _, err = os.Lstat(e.EditorPath); err != nil {
		t.Fatal("dangling alias removed")
	}
	if _, err = os.Stat(e.Path); !os.IsNotExist(err) {
		t.Fatal("master recreated")
	}
}
func TestRejectUnownedAndForgedTargets(t *testing.T) {
	cfg := fixture(t)
	e, err := Ensure(cfg, "note", 123, "Title")
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(filepath.Dir(e.EditorPath), "Other--123.md")
	os.Symlink(e.Path, unknown)
	if _, err = Resolve(cfg, unknown); err == nil {
		t.Fatal("trusted numeric suffix")
	}
	os.Remove(e.EditorPath)
	os.Symlink(unknown, e.EditorPath)
	if _, err = Resolve(cfg, e.EditorPath); err == nil {
		t.Fatal("nested symlink accepted")
	}
}
func TestRejectUnknownOccupantAndManifestMismatch(t *testing.T) {
	cfg := fixture(t)
	dir, err := layout(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = privateDir(filepath.Join(dir, "note"), true); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(dir, "note", "Title--123.md")
	os.WriteFile(occupied, []byte("mine"), 0600)
	if _, err = Ensure(cfg, "note", 123, "Title"); err == nil {
		t.Fatal("unknown file accepted")
	}
	os.Remove(occupied)
	e, err := Ensure(cfg, "note", 123, "Title")
	if err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(cfg, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m.Vault = "/different"
	raw, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600)
	if _, err = Resolve(cfg, e.EditorPath); err == nil {
		t.Fatal("forged vault accepted")
	}
}
func TestRejectOverlappingSyncAndPublicRoots(t *testing.T) {
	cfg := fixture(t)
	for _, root := range []string{cfg.VaultDir, filepath.Join(cfg.VaultDir, "aliases"), filepath.Dir(cfg.VaultDir), filepath.Join(t.TempDir(), "OneDrive Work", "view"), "relative"} {
		t.Setenv(Env, root)
		if _, err := Ensure(cfg, "note", 123, "Title"); err == nil {
			t.Errorf("unsafe root accepted %s", root)
		}
	}
	root := filepath.Join(t.TempDir(), "public")
	os.Mkdir(root, 0755)
	t.Setenv(Env, root)
	if _, err := Ensure(cfg, "note", 123, "Title"); err == nil {
		t.Fatal("public root accepted")
	}
}
func TestFullIDSeparatesSanitizedAndCaseTitles(t *testing.T) {
	cfg := fixture(t)
	var paths []string
	for i, title := range []string{"same/title", "same\\title", "SAME_TITLE", "é", "e\u0301", strings.Repeat("🌸", 300)} {
		id := int64(123 + i)
		os.WriteFile(cfg.NotePath(id), []byte("body"), 0600)
		e, err := Ensure(cfg, "note", id, title)
		if err != nil {
			t.Fatal(err)
		}
		if len(filepath.Base(e.EditorPath)) > 240 {
			t.Fatal("filename too long")
		}
		for _, p := range paths {
			if p == e.EditorPath {
				t.Fatal("collision")
			}
		}
		paths = append(paths, e.EditorPath)
	}
}
func TestCanonicalReplacementRemainsSameIdentity(t *testing.T) {
	cfg := fixture(t)
	e, err := Ensure(cfg, "note", 123, "Title")
	if err != nil {
		t.Fatal(err)
	}
	replacement := e.Path + ".tmp"
	os.WriteFile(replacement, []byte("new body"), 0600)
	os.Rename(replacement, e.Path)
	if _, err = Resolve(cfg, e.EditorPath); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(e.EditorPath)
	if string(raw) != "new body" {
		t.Fatal("not following master replacement")
	}
}
