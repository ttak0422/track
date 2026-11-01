package note

import (
	"errors"
	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/vaultfile"
	"os"
	"path/filepath"
	"testing"
)

func TestPostReadStatFailureRemainsFileLocal(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	if err := os.MkdirAll(cfg.NoteDir(), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.NoteDir(), "1.md")
	if err := os.WriteFile(path, []byte("readable synthetic source"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := parseFileWithStat(path, cfg, false, func(string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
	})
	var local *vaultfile.Error
	if !errors.As(err, &local) {
		t.Fatalf("post-read disappearance became a fatal unclassified error: %v", err)
	}
}

func TestAtomicActivityUpdateRefusesChangedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "1.yaml")
	expected := Metadata{Version: 1, Title: "Keep", Extra: map[string]any{"opaque": uint64(18446744073709551615)}}
	if err := WriteMetadata(path, expected); err != nil {
		t.Fatal(err)
	}
	current, _, err := ReadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(path, Metadata{Title: "External edit"}); err != nil {
		t.Fatal(err)
	}
	updated, _ := EnsureDay(current, "2026-11-01")
	if err := WriteMetadataAtomic(path, current, updated); err == nil {
		t.Fatal("activity overwrite accepted stale metadata")
	}
	after, _, err := ReadMetadata(path)
	if err != nil || after.Title != "External edit" {
		t.Fatal(after, err)
	}
}
