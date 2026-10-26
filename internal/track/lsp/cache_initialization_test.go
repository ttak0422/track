package lsp

import (
	"os"
	"strings"
	"testing"
)

func TestColdCreateNoteRejectsExistingDiskTitle(t *testing.T) {
	srv, _ := setupServer(t)
	if err := os.MkdirAll(srv.cfg.NoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srv.cfg.NotePath(100), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.DeleteNote(100); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.createNote("Go"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("cold create accepted duplicate: %v", err)
	}
	files, err := os.ReadDir(srv.cfg.NoteDir())
	if err != nil || len(files) != 1 {
		t.Fatalf("duplicate create wrote files: %v, %v", files, err)
	}
}
