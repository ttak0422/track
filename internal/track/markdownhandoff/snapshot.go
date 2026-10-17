// Package markdownhandoff carries bounded, read-only snapshots from a local CLI to the web reader.
// Only the CLI opens files. The HTTP server accepts bytes, never a path to read on the user's disk.
package markdownhandoff

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Protocol     = "track-markdown-v1"
	MaxMarkdown  = 2 << 20
	MaxImage     = 10 << 20
	MaxBytes     = 16 << 20
	MaxWireBytes = 24 << 20 // JSON base64 plus bounded filenames
	MaxFiles     = 5000
)

type File struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

type Snapshot struct {
	Active string `json:"active"`
	Files  []File `json:"files"`
}

func markdown(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

func limit(name string) int {
	if markdown(name) {
		return MaxMarkdown
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return MaxImage
	}
	return 0
}

func (s Snapshot) Validate() error {
	if len(s.Files) == 0 || len(s.Files) > MaxFiles {
		return fmt.Errorf("snapshot must contain 1–5,000 files")
	}
	seen := map[string]bool{}
	total := 0
	for _, f := range s.Files {
		if !fs.ValidPath(f.Path) || len(f.Path) > 4096 || strings.Contains(f.Path, "\\") || strings.ContainsFunc(f.Path, unicode.IsControl) || seen[f.Path] {
			return fmt.Errorf("unsupported or duplicate snapshot path")
		}
		seen[f.Path] = true
		max := limit(f.Path)
		if max == 0 || len(f.Data) > max {
			return fmt.Errorf("unsupported or oversized file: %s", f.Path)
		}
		if markdown(f.Path) && !utf8.Valid(f.Data) {
			return fmt.Errorf("not valid UTF-8 Markdown: %s", f.Path)
		}
		total += len(f.Data)
		if total > MaxBytes {
			return fmt.Errorf("Markdown folder snapshot exceeds 16 MiB; choose a smaller folder")
		}
	}
	if !seen[s.Active] || !markdown(s.Active) {
		return fmt.Errorf("active Markdown file is missing")
	}
	return nil
}

// Read snapshots the file's containing folder. This is the explicitly bounded scope of relative
// links, like choosing that folder in the browser. Hidden/dependency directories and symlinks are
// excluded. OpenRoot confines reads even if a path is replaced with a symlink during the walk.
func Read(filename string) (Snapshot, error) {
	var snapshot Snapshot
	abs, err := filepath.Abs(filename)
	if err != nil {
		return snapshot, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return snapshot, err
	}
	if !markdown(abs) {
		return snapshot, fmt.Errorf("choose a saved .md or .markdown file")
	}
	root, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return snapshot, err
	}
	defer root.Close()
	snapshot.Active = filepath.Base(abs)
	total, visited := 0, 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && name != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor") {
			return fs.SkipDir
		}
		visited++
		if visited > MaxFiles {
			return fmt.Errorf("Markdown folder exceeds 5,000 entries; choose a smaller folder")
		}
		if entry.IsDir() || entry.Type()&os.ModeType != 0 || limit(name) == 0 {
			return nil
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", name)
		}
		max := limit(name)
		if info.Size() > int64(max) || info.Size() > int64(MaxBytes-total) {
			return fmt.Errorf("file or folder exceeds the snapshot size limit: %s", name)
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
		if err != nil {
			return err
		}
		total += len(data)
		if total > MaxBytes {
			return fmt.Errorf("Markdown folder snapshot exceeds 16 MiB")
		}
		snapshot.Files = append(snapshot.Files, File{Path: name, Data: data})
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, snapshot.Validate()
}
