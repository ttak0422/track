package config

import (
	"os"
	"path/filepath"
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
