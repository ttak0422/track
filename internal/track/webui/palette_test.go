package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPaletteEmptyPath(t *testing.T) {
	css, err := LoadPalette("")
	if err != nil || css != "" {
		t.Fatalf("empty path -> %q, %v; want empty", css, err)
	}
}

func TestLoadPaletteBuildsScopedCSS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colors.yml")
	contents := "light:\n  accent: \"#2f6f5e\"\n  text: \"#20231f\"\ndark:\n  accent: \"#62b39b\"\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	css, err := LoadPalette(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, want := range []string{
		":root{--accent:#2f6f5e;--text:#20231f;}",
		`:root[data-theme="dark"]{--accent:#62b39b;}`,
		"@media (prefers-color-scheme: dark)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("css missing %q\ngot:\n%s", want, css)
		}
	}
}

func TestLoadPaletteIgnoresUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colors.yml")
	// "wallpaper" is not a themeable variable and must be dropped, not injected.
	if err := os.WriteFile(path, []byte("light:\n  accent: \"#abc\"\n  wallpaper: \"#fff\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	css, err := LoadPalette(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Contains(css, "wallpaper") {
		t.Fatalf("unknown key leaked into css: %s", css)
	}
	if !strings.Contains(css, "--accent:#abc;") {
		t.Fatalf("known key dropped: %s", css)
	}
}

func TestLoadPaletteRejectsInjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colors.yml")
	// A value trying to break out of the declaration must be rejected.
	if err := os.WriteFile(path, []byte("light:\n  accent: \"red;} body{display:none\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPalette(path); err == nil {
		t.Fatal("expected invalid color value to be rejected")
	}
}

func TestLoadPaletteMissingFileErrors(t *testing.T) {
	if _, err := LoadPalette(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("expected error for missing palette file")
	}
}
