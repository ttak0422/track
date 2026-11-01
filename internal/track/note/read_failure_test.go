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
