// Package asset manages attachment storage under the vault's single assets directory (<vault>/assets).
// It is the storage primitive that media features build on — the editor's paste-image command today,
// and a future "web fetch" that downloads a remote resource into the vault — so it lives in the engine,
// independent of the CLI command layer, for other integrations to reuse.
//
// A note references a stored asset with the relative path "assets/<file>", regardless of the note's
// kind.
package asset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ttak0422/track/internal/track/config"
)

// Stored describes a file written into the vault's assets directory.
type Stored struct {
	Path string `json:"path"` // absolute path written
	Ref  string `json:"ref"`  // reference to embed from a note, e.g. "assets/foo.png"
	Name string `json:"name"` // final filename within the assets directory
}

// Store writes data into the vault's assets directory under a filesystem-safe name derived from
// preferredName, creating the directory as needed and never overwriting an existing file (a numeric
// suffix is appended on collision). It returns the absolute path and the "assets/<file>" reference to
// embed from a note.
func Store(cfg *config.Config, preferredName string, data []byte) (Stored, error) {
	dir := cfg.AssetsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Stored{}, fmt.Errorf("create assets dir: %w", err)
	}
	name := uniqueName(dir, sanitizeName(preferredName))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Stored{}, fmt.Errorf("write asset: %w", err)
	}
	return Stored{Path: path, Ref: config.AssetsDirName + "/" + name, Name: name}, nil
}

// Import copies a local file into the vault's assets directory, keeping its base name. It is a thin
// convenience over Store for the common "attach a file already on disk" case.
func Import(cfg *config.Config, srcPath string) (Stored, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return Stored{}, fmt.Errorf("read source: %w", err)
	}
	return Store(cfg, filepath.Base(srcPath), data)
}

// sanitizeName reduces an arbitrary name to a single safe filename: it drops any directory part and
// replaces characters that would break a Markdown link reference (whitespace, brackets, parentheses,
// URL-significant punctuation, path separators) with "-". Non-ASCII letters (e.g. Japanese) are kept.
func sanitizeName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r):
			// drop
		case unicode.IsSpace(r), r == '/', r == '\\', r == '(', r == ')',
			r == '[', r == ']', r == '<', r == '>', r == '#', r == '?', r == '%', r == '"', r == '\'':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}

// uniqueName returns name if it is free in dir, otherwise inserts the smallest "-N" before the
// extension until the name is unused.
func uniqueName(dir, name string) string {
	if !exists(filepath.Join(dir, name)) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !exists(filepath.Join(dir, candidate)) {
			return candidate
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
