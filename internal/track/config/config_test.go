package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsToHomeTrack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// With nothing configured, the vault defaults to $HOME/track (ADR 0015). The display path keeps
	// the configured form verbatim, so it is deterministic regardless of symlinks.
	want := filepath.Join(home, "track")
	if cfg.VaultDirDisplay != want {
		t.Fatalf("VaultDirDisplay = %q, want %q", cfg.VaultDirDisplay, want)
	}
}

func TestLoadUsesConfigFileVault(t *testing.T) {
	vault := t.TempDir()
	cache := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte("vault_dir: "+vault+"\ncache_dir: "+cache+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	wantVault, err := canonicalPath(vault)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != wantVault {
		t.Fatalf("VaultDir = %q, want %q", cfg.VaultDir, wantVault)
	}
	wantDB := filepath.Join(cache, vaultCacheKey(wantVault), "index.db")
	if cfg.DBPath != wantDB {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
}

func TestLoadCanonicalizesSymlinkVault(t *testing.T) {
	realVault := t.TempDir()
	linkParent := t.TempDir()
	linkVault := filepath.Join(linkParent, "vault-link")
	if err := os.Symlink(realVault, linkVault); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cache := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte("vault_dir: "+linkVault+"\ncache_dir: "+cache+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	wantVault, err := canonicalPath(realVault)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != wantVault {
		t.Fatalf("VaultDir = %q, want %q", cfg.VaultDir, wantVault)
	}
	wantDB := filepath.Join(cache, vaultCacheKey(wantVault), "index.db")
	if cfg.DBPath != wantDB {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
}

func TestLoadUsesExplicitTrackVault(t *testing.T) {
	vault := t.TempDir()
	cache := t.TempDir()
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", cache)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	wantVault, err := canonicalPath(vault)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != wantVault {
		t.Fatalf("VaultDir = %q, want %q", cfg.VaultDir, wantVault)
	}
	wantDB := filepath.Join(cache, vaultCacheKey(wantVault), "index.db")
	if cfg.DBPath != wantDB {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
}

func TestLoadHonorsExplicitTrackDB(t *testing.T) {
	vault := t.TempDir()
	db := filepath.Join(t.TempDir(), "custom.db")
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", vault)
	t.Setenv("TRACK_DB", db)
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DBPath != db {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, db)
	}
}

func TestKindPaths(t *testing.T) {
	vault := t.TempDir()
	cfg := &Config{VaultDir: vault, Extensions: []string{".md"}}

	cases := []struct {
		path string
		kind string
		want bool
	}{
		{cfg.NotePath(100), KindNote, true},
		{cfg.JournalPath("20260606"), KindJournal, true},
		{cfg.TemplatePath(200), KindTemplate, true},
		{filepath.Join(vault, "100.md"), "", false},
		{filepath.Join(cfg.NoteDir(), "abc.md"), "", false},
		{filepath.Join(cfg.TemplateDir(), "200.md"), "", false},
		{filepath.Join(cfg.TemplateDir(), "abc.template.md"), "", false},
	}
	for _, c := range cases {
		kind, ok := cfg.KindFromPath(c.path)
		if ok != c.want || kind != c.kind {
			t.Fatalf("KindFromPath(%q) = %q, %v; want %q, %v", c.path, kind, ok, c.kind, c.want)
		}
	}
}

func TestKindFromPathCanonicalizesSymlinkPath(t *testing.T) {
	realVault := t.TempDir()
	linkParent := t.TempDir()
	linkVault := filepath.Join(linkParent, "vault-link")
	if err := os.Symlink(realVault, linkVault); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cfg := &Config{VaultDir: realVault, Extensions: []string{".md"}}

	kind, ok := cfg.KindFromPath(filepath.Join(linkVault, "note", "100.md"))
	if !ok || kind != KindNote {
		t.Fatalf("KindFromPath through symlink = %q, %v; want %q, true", kind, ok, KindNote)
	}
}

func TestLoadWebTheme(t *testing.T) {
	cases := map[string]string{
		"dark":     "dark",
		"light":    "light",
		"system":   "system",
		"":         "system", // unset
		"hot-pink": "system", // unknown values are normalized away
	}
	for in, want := range cases {
		vault := t.TempDir()
		configPath := filepath.Join(t.TempDir(), "config.yml")
		contents := "vault_dir: " + vault + "\ncache_dir: " + t.TempDir() + "\n"
		if in != "" {
			contents += "web:\n  theme: " + in + "\n"
		}
		if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TRACK_CONFIG", configPath)
		t.Setenv("TRACK_VAULT", "")
		t.Setenv("TRACK_DB", "")
		t.Setenv("TRACK_CACHE_DIR", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load (theme=%q): %v", in, err)
		}
		if cfg.WebTheme != want {
			t.Fatalf("theme %q -> WebTheme %q, want %q", in, cfg.WebTheme, want)
		}
	}
}

func TestNoteIconPrecedence(t *testing.T) {
	cfg := &Config{Icons: IconMap{
		Tags:  map[string]string{"idea": "💡", "book": "📚"},
		Kinds: map[string]string{"journal": "📓", "note": "📝"},
	}}
	cases := []struct {
		name     string
		kind     string
		tags     []string
		override string
		want     string
	}{
		{"override wins over everything", "note", []string{"idea"}, "🔥", "🔥"},
		{"first matching tag wins over kind", "note", []string{"idea", "book"}, "", "💡"},
		{"unmapped tag falls through to kind", "note", []string{"misc"}, "", "📝"},
		{"kind mapping when no tags", "journal", nil, "", "📓"},
		{"no mapping yields empty", "note", []string{"misc"}, "", "📝"},
		{"nothing at all", "widget", nil, "", ""},
	}
	for _, c := range cases {
		if got := cfg.NoteIcon(c.kind, c.tags, c.override); got != c.want {
			t.Errorf("%s: NoteIcon(%q, %v, %q) = %q, want %q", c.name, c.kind, c.tags, c.override, got, c.want)
		}
	}
}

func TestLoadIconsAndHome(t *testing.T) {
	vault := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yml")
	contents := "vault_dir: " + vault + "\ncache_dir: " + t.TempDir() + "\n" +
		"web:\n  home: Home\n" +
		"icons:\n  tags:\n    idea: \"💡\"\n  kinds:\n    journal: \"📓\"\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.WebHome != "Home" {
		t.Fatalf("WebHome = %q, want %q", cfg.WebHome, "Home")
	}
	if got := cfg.NoteIcon("note", []string{"idea"}, ""); got != "💡" {
		t.Fatalf("tag icon = %q, want 💡", got)
	}
	if got := cfg.NoteIcon("journal", nil, ""); got != "📓" {
		t.Fatalf("kind icon = %q, want 📓", got)
	}
}

// loadWithEmbedder writes a config.yml containing the given embedder line (empty = key absent), points
// Load at it with env overrides cleared, and returns the result.
func loadWithEmbedder(t *testing.T, embedderLine string) (*Config, error) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yml")
	contents := "vault_dir: " + t.TempDir() + "\ncache_dir: " + t.TempDir() + "\n" + embedderLine
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")
	t.Setenv("TRACK_EMBEDDER", "")
	return Load()
}

func TestLoadEmbedder(t *testing.T) {
	// Scalar form: split on whitespace (no shell quoting).
	cfg, err := loadWithEmbedder(t, "embedder: track-embed --model mini\n")
	if err != nil {
		t.Fatalf("load scalar: %v", err)
	}
	if want := []string{"track-embed", "--model", "mini"}; !equalStrings(cfg.EmbedderCommand, want) {
		t.Fatalf("scalar EmbedderCommand = %v, want %v", cfg.EmbedderCommand, want)
	}

	// Sequence form: verbatim argv, so an argument may contain a space.
	cfg, err = loadWithEmbedder(t, "embedder: [track-embed, --model, \"mini lm\"]\n")
	if err != nil {
		t.Fatalf("load sequence: %v", err)
	}
	if want := []string{"track-embed", "--model", "mini lm"}; !equalStrings(cfg.EmbedderCommand, want) {
		t.Fatalf("sequence EmbedderCommand = %v, want %v", cfg.EmbedderCommand, want)
	}

	// TRACK_EMBEDDER overrides either form entirely (and is always whitespace-split).
	t.Setenv("TRACK_EMBEDDER", "other-embed --fast")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load with env: %v", err)
	}
	if want := []string{"other-embed", "--fast"}; !equalStrings(cfg.EmbedderCommand, want) {
		t.Fatalf("env override EmbedderCommand = %v, want %v", cfg.EmbedderCommand, want)
	}
}

func TestLoadEmbedderEmptyFormsDisable(t *testing.T) {
	for _, line := range []string{"", "embedder:\n", "embedder: \"\"\n", "embedder: []\n"} {
		cfg, err := loadWithEmbedder(t, line)
		if err != nil {
			t.Fatalf("load %q: %v", line, err)
		}
		if len(cfg.EmbedderCommand) != 0 {
			t.Fatalf("%q must leave no embedder configured, got %v", line, cfg.EmbedderCommand)
		}
	}
}

func TestLoadEmbedderRejectsBadValues(t *testing.T) {
	// A mapping is neither accepted form; it must fail loudly, naming the key.
	if _, err := loadWithEmbedder(t, "embedder:\n  cmd: track-embed\n"); err == nil || !strings.Contains(err.Error(), "embedder") {
		t.Fatalf("mapping embedder must error naming the key, got %v", err)
	}
	// An empty argv[0] would fail confusingly at exec time; reject it at load instead.
	if _, err := loadWithEmbedder(t, "embedder: [\"\", --model, mini]\n"); err == nil || !strings.Contains(err.Error(), "embedder") {
		t.Fatalf("empty argv[0] must error naming the key, got %v", err)
	}
	// A null sequence element must fail loudly, not be silently dropped from argv (yaml.v3 skips
	// nulls when decoding into []string, which would make a flag vanish or shift argv[0]).
	for _, line := range []string{"embedder: [track-embed, ~]\n", "embedder: [~, --model]\n", "embedder: [~]\n"} {
		if _, err := loadWithEmbedder(t, line); err == nil || !strings.Contains(err.Error(), "embedder") {
			t.Fatalf("%q must error naming the key, got %v", line, err)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDisplayPathForKindKeepsSymlink(t *testing.T) {
	realVault := t.TempDir()
	linkParent := t.TempDir()
	linkVault := filepath.Join(linkParent, "vault-link")
	if err := os.Symlink(realVault, linkVault); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cache := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte("vault_dir: "+linkVault+"\ncache_dir: "+cache+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Canonical path resolves the symlink; the display path keeps it.
	canonical := cfg.PathForKind(KindNote, 100)
	display := cfg.DisplayPathForKind(KindNote, 100)
	wantDisplay := filepath.Join(linkVault, "note", "100.md")
	if display != wantDisplay {
		t.Fatalf("display path = %q, want %q", display, wantDisplay)
	}
	if display == canonical {
		t.Fatalf("display path should differ from canonical through a symlink: %q", display)
	}
}

func TestEnsureVaultSkeleton(t *testing.T) {
	cfg := &Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	created, err := cfg.EnsureVaultSkeleton()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(created) != len(cfg.VaultSkeleton()) {
		t.Fatalf("expected all skeleton dirs created, got %v", created)
	}
	for _, dir := range cfg.VaultSkeleton() {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("skeleton dir missing: %s (err %v)", dir, err)
		}
	}
	// Idempotent: a second call creates nothing.
	again, err := cfg.EnsureVaultSkeleton()
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second ensure should create nothing, got %v", again)
	}
}

func TestLoadPropertySchema(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yml")
	body := "vault_dir: " + t.TempDir() + "\ncache_dir: " + t.TempDir() + `
properties:
  status:
    type: string
    values: [draft, done]
  rating:
    type: number
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	status := cfg.Properties["status"]
	if status.Type != "string" || len(status.Values) != 2 || status.Values[0] != "draft" {
		t.Fatalf("status spec = %+v", status)
	}
	if cfg.Properties["rating"].Type != "number" {
		t.Fatalf("rating spec = %+v", cfg.Properties["rating"])
	}
}

func TestLoadRejectsUnknownPropertyType(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yml")
	body := "vault_dir: " + t.TempDir() + "\nproperties:\n  status:\n    type: enum\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACK_CONFIG", configPath)
	t.Setenv("TRACK_VAULT", "")
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for properties.status type enum")
	}
}

func TestCaptureAndArchiveDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", t.TempDir())
	t.Setenv("TRACK_DB", "")
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	t.Setenv("TRACK_CAPTURE_INBOX", "")
	t.Setenv("TRACK_ARCHIVE_NOTE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CaptureInbox != "Inbox" {
		t.Fatalf("CaptureInbox = %q, want Inbox", cfg.CaptureInbox)
	}
	// {{year}} substitutes to the given year; a title without the placeholder is verbatim.
	when := time.Date(2031, 3, 2, 0, 0, 0, 0, time.UTC)
	if got := cfg.ArchiveNoteTitle(when); got != "Archive 2031" {
		t.Fatalf("ArchiveNoteTitle = %q, want Archive 2031", got)
	}
	cfg.ArchiveNote = "Attic"
	if got := cfg.ArchiveNoteTitle(when); got != "Attic" {
		t.Fatalf("ArchiveNoteTitle verbatim = %q, want Attic", got)
	}
}
