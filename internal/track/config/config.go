// Package config centralizes track's runtime configuration: where notes live, where the index database and sidecar metadata live, and which file extensions count as notes.
// Keeping these in one place lets future file types and the (future) LSP server share the same resolution logic.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
)

type Config struct {
	VaultDir          string
	DBPath            string
	Extensions        []string
	DateFormat        string
	JournalDateFormat string
	// BabelLanguages maps a source-block language to the command that runs it. lua and viml ship as
	// samples; TRACK_BABEL_<LANG> overrides or adds languages (value is "command arg arg...").
	BabelLanguages map[string]babel.Executor
}

// Load resolves configuration from the environment.
// TRACK_VAULT is required so track never creates or reads an implicit vault by accident.
// TRACK_DB overrides the index database path. Otherwise the rebuildable SQLite cache lives under
// TRACK_CACHE_DIR, or the platform user cache directory when TRACK_CACHE_DIR is unset.
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
		cacheDir := os.Getenv("TRACK_CACHE_DIR")
		if cacheDir == "" {
			userCache, err := os.UserCacheDir()
			if err != nil {
				return nil, fmt.Errorf("resolve cache dir: %w", err)
			}
			cacheDir = filepath.Join(userCache, "track")
		}
		db = filepath.Join(cacheDir, vaultCacheKey(vault), "index.db")
	}

	return &Config{
		VaultDir:          vault,
		DBPath:            db,
		Extensions:        []string{".md"},
		DateFormat:        "2006-01-02",
		JournalDateFormat: "20060102",
		BabelLanguages:    loadBabelLanguages(),
	}, nil
}

func vaultCacheKey(vault string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(vault)))
	return hex.EncodeToString(sum[:8])
}

// loadBabelLanguages returns the sample executors (lua, viml), overlaid with TRACK_BABEL_<LANG> env
// overrides. Each override value is split on whitespace into command and arguments; "{{file}}" in an
// argument is replaced with the block's temp script path at run time.
func loadBabelLanguages() map[string]babel.Executor {
	langs := map[string]babel.Executor{
		"lua":  {Command: "lua", Args: []string{"{{file}}"}},
		"viml": {Command: "nvim", Args: []string{"--headless", "-S", "{{file}}", "-c", "qa!"}},
	}
	const prefix = "TRACK_BABEL_"
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, prefix) {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name := strings.ToLower(kv[len(prefix):eq])
		fields := strings.Fields(kv[eq+1:])
		if name == "" || len(fields) == 0 {
			continue
		}
		langs[name] = babel.Executor{Command: fields[0], Args: fields[1:]}
	}
	return langs
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
	return c.VaultDir
}

// JournalPath returns the path for a daily journal note named yyyyMMdd.
func (c *Config) JournalPath(name string) string {
	return filepath.Join(c.VaultDir, name+c.PrimaryExt())
}

// TrackDir returns the hidden directory used for authoritative track-owned data inside the vault.
// Rebuildable caches such as the SQLite index live outside the vault.
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
