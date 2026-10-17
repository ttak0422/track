package markdownhandoff

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadFolderScope(t *testing.T) {
	root := filepath.Join(t.TempDir(), "日本語 folder")
	for name, body := range map[string]string{
		"現在 file.md":   "# Current\n[Next](docs/next.md)\n![Image](images/pic.png)",
		"docs/next.md": "[Back](../現在%20file.md)", "images/pic.png": "image bytes",
		".git/secret.md": "hidden", "node_modules/pkg/README.md": "dependency",
		"vendor/pkg/README.md": "dependency", "secret.txt": "unsupported",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("must not transfer"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "symlink.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Read(filepath.Join(root, "現在 file.md"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range snapshot.Files {
		names = append(names, f.Path)
	}
	if snapshot.Active != "現在 file.md" || !reflect.DeepEqual(names, []string{"docs/next.md", "images/pic.png", "現在 file.md"}) {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestSnapshotRejectsUnsafeAndOversizedInput(t *testing.T) {
	for name, snapshot := range map[string]Snapshot{
		"traversal":          {Active: "x.md", Files: []File{{Path: "../x.md"}}},
		"absolute":           {Active: "/x.md", Files: []File{{Path: "/x.md"}}},
		"duplicate":          {Active: "x.md", Files: []File{{Path: "x.md"}, {Path: "x.md"}}},
		"non-markdown":       {Active: "secret.txt", Files: []File{{Path: "secret.txt"}}},
		"invalid UTF-8":      {Active: "x.md", Files: []File{{Path: "x.md", Data: []byte{0xff}}}},
		"oversized Markdown": {Active: "x.md", Files: []File{{Path: "x.md", Data: make([]byte, MaxMarkdown+1)}}},
		"oversized image":    {Active: "x.md", Files: []File{{Path: "x.md"}, {Path: "x.png", Data: make([]byte, MaxImage+1)}}},
		"oversized folder":   {Active: "x.md", Files: []File{{Path: "x.md"}, {Path: "a.png", Data: make([]byte, 9<<20)}, {Path: "b.png", Data: make([]byte, 9<<20)}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := snapshot.Validate(); err == nil {
				t.Fatal("accepted unsafe snapshot")
			}
		})
	}
	root := t.TempDir()
	p := filepath.Join(root, "large.md")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxMarkdown + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := Read(p); err == nil {
		t.Fatal("accepted oversized disk file")
	}
}
