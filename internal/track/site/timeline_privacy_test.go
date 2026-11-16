package site

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/index"
)

func TestTimelineExportDoesNotPublishPrivateSnapshots(t *testing.T) {
	cfg, st := vaultStore(t)
	writeVaultNote(t, cfg, 100, "Public note", "Only this note is public.\n")
	if _, err := index.New(cfg, st).Full(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.VaultDir, ".track/timelines")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private.json"), []byte(`{"title":"Private timeline marker","events":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := Build(cfg, st, Options{Root: 100, Calendar: true}, fakeFrontend(t), out); err != nil {
		t.Fatal(err)
	}
	key := pageLockKey(t, out)
	if err := filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(filepath.ToSlash(path), ".track/timelines/") {
			t.Fatalf("timeline storage was published: %s", path)
		}
		if !d.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), "Private timeline marker") {
				t.Fatalf("timeline content leaked at %s", path)
			}
			rel, _ := filepath.Rel(out, path)
			if strings.HasPrefix(filepath.ToSlash(rel), "data/") && strings.HasSuffix(path, ".bin") {
				plain, err := Unlock(key, raw)
				if err != nil {
					return err
				}
				if strings.Contains(string(plain), "Private timeline marker") {
					t.Fatalf("timeline leaked in locked data at %s", path)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var published map[string]any = readJSON[map[string]any](t, filepath.Join(out, "data", "site.json"))
	if published["calendar"] != true {
		t.Fatal("test did not enable static calendar")
	}
}
