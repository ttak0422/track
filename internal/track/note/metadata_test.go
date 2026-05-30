package note

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/config"
)

func TestWriteReadMetadataRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".track", "notes", "1000.yaml")
	in := Metadata{
		Title:   "リンク",
		Aliases: []string{"link", "TEST"},
		Tags:    []string{"zettel"},
		Created: "2026-05-24",
	}
	if err := WriteMetadata(path, in); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	got, found, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if !found {
		t.Fatal("expected metadata to be found")
	}
	in.Version = CurrentMetadataVersion
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("metadata mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestWriteReadMetadataV2RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".track", "notes", "1000.yaml")
	in := Metadata{
		Title: "Example",
		Blocks: map[string]babel.BlockMeta{
			"hello": {
				Language:   "lua",
				HeaderArgs: map[string][]string{"results": {"output", "verbatim"}},
				BodyHash:   "sha256:abc",
				LastRun: &babel.RunResult{
					Status:   "success",
					ExitCode: 0,
					Stdout:   "1\n",
				},
			},
		},
	}
	if err := WriteMetadata(path, in); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	got, found, err := ReadMetadata(path)
	if err != nil || !found {
		t.Fatalf("read metadata: found=%v err=%v", found, err)
	}
	if got.Version != MetadataVersionV2 {
		t.Fatalf("blocks should bump to version 2, got %d", got.Version)
	}
	in.Version = MetadataVersionV2
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("v2 metadata mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestReadMetadataAbsent(t *testing.T) {
	got, found, err := ReadMetadata(filepath.Join(t.TempDir(), ".track", "notes", "missing.yaml"))
	if err != nil {
		t.Fatalf("read absent metadata: %v", err)
	}
	if found {
		t.Fatal("did not expect metadata")
	}
	if !reflect.DeepEqual(got, Metadata{}) {
		t.Fatalf("expected zero metadata, got %+v", got)
	}
}

func TestReadMetadataRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".track", "notes", "1000.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 999\ntitle: bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadMetadata(path); err == nil || !strings.Contains(err.Error(), "unsupported metadata version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestSplitLegacyFootmatter(t *testing.T) {
	raw := strings.Join([]string{
		"# Title",
		"",
		"本文の リンク について。",
		"",
		"<!--track",
		"title: リンク",
		"aliases:",
		"    - link",
		"    - TEST",
		"tags:",
		"    - zettel",
		"created: 2026-05-24",
		"-->",
		"",
	}, "\n")

	body, legacy, found := SplitLegacyFootmatter(raw)
	if !found {
		t.Fatal("expected legacy footmatter to be found")
	}
	if body != "# Title\n\n本文の リンク について。" {
		t.Fatalf("body mismatch: %q", body)
	}
	got, err := ParseLegacyMetadata(legacy)
	if err != nil {
		t.Fatalf("parse legacy metadata: %v", err)
	}
	want := Metadata{
		Version: CurrentMetadataVersion,
		Title:   "リンク",
		Aliases: []string{"link", "TEST"},
		Tags:    []string{"zettel"},
		Created: "2026-05-24",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestSplitLegacyFootmatterAbsent(t *testing.T) {
	raw := "# Just a body\n\nno metadata here\n"
	body, legacy, found := SplitLegacyFootmatter(raw)
	if found {
		t.Fatal("did not expect legacy footmatter")
	}
	if body != "# Just a body\n\nno metadata here" {
		t.Fatalf("body mismatch: %q", body)
	}
	if legacy != "" {
		t.Fatalf("expected empty legacy payload, got %q", legacy)
	}
}

func TestParseFileReconcilesMetadataTitleFromBody(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:   vault,
		DBPath:     filepath.Join(vault, ".track", "index.db"),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
	}
	path := cfg.NotePath(1000)
	raw := "# Body\n\n<!--track\ntitle: legacy\n-->\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(cfg.MetadataPath(1000), Metadata{
		Title:   "sidecar",
		Aliases: []string{"alias"},
		Tags:    []string{"tag"},
		Created: "2026-05-30",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := ParseFile(path, cfg)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if n.Body != "# Body" {
		t.Fatalf("body should strip legacy block, got %q", n.Body)
	}
	if n.Meta.Title != "Body" {
		t.Fatalf("expected body title to win, got %+v", n.Meta)
	}
	got, found, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected reconciled metadata to be written")
	}
	want := Metadata{
		Version: CurrentMetadataVersion,
		Title:   "Body",
		Aliases: []string{"alias"},
		Tags:    []string{"tag"},
		Created: "2026-05-30",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reconciled metadata mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestParseFileCreatesMetadataFromBodyTitle(t *testing.T) {
	vault := t.TempDir()
	cfg := &config.Config{
		VaultDir:   vault,
		DBPath:     filepath.Join(vault, ".track", "index.db"),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
	}
	path := cfg.NotePath(1000)
	if err := os.WriteFile(path, []byte("# Created From Body\n\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := ParseFile(path, cfg)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if n.Meta.Title != "Created From Body" {
		t.Fatalf("expected title from body, got %+v", n.Meta)
	}
	got, found, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Title != "Created From Body" {
		t.Fatalf("expected metadata to be created from body title, found=%v got=%+v", found, got)
	}
}

func TestFirstH1Title(t *testing.T) {
	body := strings.Join([]string{
		"```",
		"# Example",
		"```",
		"",
		"## Not H1",
		"# Actual Title #",
	}, "\n")

	if got := FirstH1Title(body); got != "Actual Title" {
		t.Fatalf("FirstH1Title = %q, want %q", got, "Actual Title")
	}
}
