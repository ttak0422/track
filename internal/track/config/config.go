// Package config centralizes track's runtime configuration: where notes live,
// where the index database is, which file extensions count as notes, and the
// footmatter delimiters. Keeping these in one place lets future file types and
// the (future) LSP server share the same resolution logic.
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// FootmatterMarkers delimit the metadata block at the end of a note. The block
// is an HTML comment so it stays invisible in rendered markdown.
type FootmatterMarkers struct {
	Open  string
	Close string
}

type Config struct {
	VaultDir   string
	DBPath     string
	Extensions []string
	DateFormat string
	Footmatter FootmatterMarkers
}

// Load resolves configuration from the environment, falling back to XDG
// defaults. TRACK_VAULT overrides the vault directory; TRACK_DB overrides the
// index database path (default: <vault>/.track/index.db).
func Load() (*Config, error) {
	vault := os.Getenv("TRACK_VAULT")
	if vault == "" {
		vault = defaultVaultDir()
	}
	abs, err := filepath.Abs(vault)
	if err != nil {
		return nil, err
	}
	vault = abs

	db := os.Getenv("TRACK_DB")
	if db == "" {
		db = filepath.Join(vault, ".track", "index.db")
	}

	return &Config{
		VaultDir:   vault,
		DBPath:     db,
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
		Footmatter: FootmatterMarkers{Open: "<!--track", Close: "-->"},
	}, nil
}

func defaultVaultDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "track")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "track")
}

// PrimaryExt is the extension used for newly created notes.
func (c *Config) PrimaryExt() string {
	if len(c.Extensions) == 0 {
		return ".md"
	}
	return c.Extensions[0]
}

// NotePath returns the absolute path for a note with the given id.
func (c *Config) NotePath(id int64) string {
	return filepath.Join(c.VaultDir, strconv.FormatInt(id, 10)+c.PrimaryExt())
}
