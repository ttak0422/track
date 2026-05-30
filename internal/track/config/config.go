// Package config centralizes track's runtime configuration: where notes live,
// where the index database and sidecar metadata live, and which file extensions
// count as notes. Keeping these in one place lets future file types and the
// (future) LSP server share the same resolution logic.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	VaultDir          string
	DBPath            string
	Extensions        []string
	DateFormat        string
	JournalDateFormat string
}

// Load resolves configuration from the environment. TRACK_VAULT is required so
// track never creates or reads an implicit vault by accident. TRACK_DB overrides
// the index database path (default: <vault>/.track/index.db).
func Load() (*Config, error) {
	vault := os.Getenv("TRACK_VAULT")
	if vault == "" {
		return nil, fmt.Errorf("TRACK_VAULT is required; set it to your track vault directory")
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
		VaultDir:          vault,
		DBPath:            db,
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
	}, nil
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

// JournalDir returns the directory used for daily journal notes.
func (c *Config) JournalDir() string {
	return filepath.Join(c.VaultDir, "journal")
}

// JournalPath returns the path for a daily journal note named yyyyMMdd.
func (c *Config) JournalPath(name string) string {
	return filepath.Join(c.JournalDir(), name+c.PrimaryExt())
}

// TrackDir returns the hidden directory used for track-owned data inside the
// vault. It contains both the SQLite index and per-note metadata files.
func (c *Config) TrackDir() string {
	return filepath.Join(c.VaultDir, ".track")
}

// MetadataDir returns the directory for versioned per-note metadata sidecars.
func (c *Config) MetadataDir() string {
	return filepath.Join(c.TrackDir(), "notes")
}

// MetadataPath returns the sidecar metadata path for a note id.
func (c *Config) MetadataPath(id int64) string {
	return filepath.Join(c.MetadataDir(), strconv.FormatInt(id, 10)+".yaml")
}
