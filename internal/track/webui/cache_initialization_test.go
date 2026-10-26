package webui

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestColdVaultPostRefusesExistingTitle(t *testing.T) {
	for _, vault := range []string{"main", "work"} {
		t.Run(vault, func(t *testing.T) {
			server, main, work := twoVaultServer(t)
			root := main
			if vault == "work" {
				root = work
			}
			// POST is deliberately the first request, with no preceding read to
			// populate either the active or the lazily opened secondary cache.
			response, err := http.Post(server.URL+"/api/note?vault="+vault, "application/json", bytes.NewBufferString(`{"title":"Alpha in `+vault+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusConflict {
				t.Fatalf("duplicate create status = %d", response.StatusCode)
			}
			files, err := os.ReadDir(filepath.Join(root, "note"))
			if err != nil || len(files) != 1 {
				t.Fatalf("duplicate create wrote files: %v, %v", files, err)
			}
		})
	}
}

func TestColdVaultPostStopsOnInitializationFailure(t *testing.T) {
	server, _, work := twoVaultServer(t)
	noteDir := filepath.Join(work, "note")
	// A regular file in place of the directory makes the rebuild fail without
	// relying on platform-specific permissions or swallowing a malformed note.
	if err := os.Rename(noteDir, noteDir+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noteDir, []byte("unreadable note tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+"/api/note?vault=work", "application/json", bytes.NewBufferString(`{"title":"Unsafe"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("bootstrap failure status = %d", response.StatusCode)
	}
	files, err := os.ReadDir(filepath.Join(work, ".track", "notes"))
	if err != nil || len(files) != 1 {
		t.Fatalf("failed bootstrap wrote metadata: %v, %v", files, err)
	}
}
