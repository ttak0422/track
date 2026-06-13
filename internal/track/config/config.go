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
	"gopkg.in/yaml.v3"
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

type fileConfig struct {
	VaultDir          string   `yaml:"vault_dir"`
	DBPath            string   `yaml:"db_path"`
	CacheDir          string   `yaml:"cache_dir"`
	Extensions        []string `yaml:"extensions"`
	DateFormat        string   `yaml:"date_format"`
	JournalDateFormat string   `yaml:"journal_date_format"`
}

const (
	KindNote     = "note"
	KindJournal  = "journal"
	KindTemplate = "template"
)

// Load resolves configuration from the fixed user config file, with environment overrides for tests
// and one-off debugging. The default file is ~/.config/track/config.yml on XDG-style systems, or the
// platform user config equivalent.
//
// TRACK_CONFIG overrides the config file path. TRACK_VAULT, TRACK_DB, and TRACK_CACHE_DIR override
// the matching resolved values. A vault must still be configured explicitly, either in the config
// file or through TRACK_VAULT.
func Load() (*Config, error) {
	fc, err := loadFileConfig()
	if err != nil {
		return nil, err
	}

	vault := fc.VaultDir
	if env := os.Getenv("TRACK_VAULT"); env != "" {
		vault = env
	}
	if vault == "" {
		return nil, fmt.Errorf("vault_dir is required in %s or TRACK_VAULT", ConfigPath())
	}
	vault = expandHome(vault)
	abs, err := filepath.Abs(vault)
	if err != nil {
		return nil, err
	}
	vault = abs

	db := fc.DBPath
	if env := os.Getenv("TRACK_DB"); env != "" {
		db = env
	}
	if db == "" {
		cacheDir := fc.CacheDir
		if env := os.Getenv("TRACK_CACHE_DIR"); env != "" {
			cacheDir = env
		}
		if cacheDir == "" {
			userCache, err := os.UserCacheDir()
			if err != nil {
				return nil, fmt.Errorf("resolve cache dir: %w", err)
			}
			cacheDir = filepath.Join(userCache, "track")
		}
		cacheDir = expandHome(cacheDir)
		db = filepath.Join(cacheDir, vaultCacheKey(vault), "index.db")
	} else {
		db = expandHome(db)
	}

	extensions := fc.Extensions
	if len(extensions) == 0 {
		extensions = []string{".md"}
	}
	dateFormat := fc.DateFormat
	if dateFormat == "" {
		dateFormat = "2006-01-02"
	}
	journalDateFormat := fc.JournalDateFormat
	if journalDateFormat == "" {
		journalDateFormat = "20060102"
	}

	return &Config{
		VaultDir:          vault,
		DBPath:            db,
		Extensions:        extensions,
		DateFormat:        dateFormat,
		JournalDateFormat: journalDateFormat,
		BabelLanguages:    loadBabelLanguages(),
	}, nil
}

// ConfigPath returns the fixed user config path, or TRACK_CONFIG when set for tests and one-off runs.
func ConfigPath() string {
	if path := os.Getenv("TRACK_CONFIG"); path != "" {
		return expandHome(path)
	}
	userConfig, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(expandHome("~"), ".config", "track", "config.yml")
	}
	return filepath.Join(userConfig, "track", "config.yml")
}

func loadFileConfig() (fileConfig, error) {
	path := ConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileConfig{}, nil
		}
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg fileConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
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
	return filepath.Join(c.NoteDir(), strconv.FormatInt(id, 10)+c.PrimaryExt())
}

// NoteDir returns the directory used for regular notes.
func (c *Config) NoteDir() string {
	return filepath.Join(c.VaultDir, KindNote)
}

// JournalDir returns the directory used for daily journal notes.
func (c *Config) JournalDir() string {
	return filepath.Join(c.VaultDir, KindJournal)
}

// JournalPath returns the path for a daily journal note named yyyyMMdd.
func (c *Config) JournalPath(name string) string {
	return filepath.Join(c.JournalDir(), name+c.PrimaryExt())
}

// TemplateDir returns the directory used for template markdown files.
func (c *Config) TemplateDir() string {
	return filepath.Join(c.VaultDir, KindTemplate)
}

// TemplatePath returns the path for a template file with the given id.
func (c *Config) TemplatePath(id int64) string {
	return filepath.Join(c.TemplateDir(), strconv.FormatInt(id, 10)+".template"+c.PrimaryExt())
}

// PathForKind returns the derived path for a tracked file kind and id.
func (c *Config) PathForKind(kind string, id int64) string {
	switch kind {
	case KindJournal:
		return c.JournalPath(strconv.FormatInt(id, 10))
	case KindTemplate:
		return c.TemplatePath(id)
	default:
		return c.NotePath(id)
	}
}

// KindFromPath classifies a vault file by its managed directory.
func (c *Config) KindFromPath(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	vault, err := filepath.Abs(c.VaultDir)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(vault, abs)
	if err != nil {
		return "", false
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if len(parts) != 2 {
		return "", false
	}
	name := parts[1]
	switch parts[0] {
	case KindNote:
		stem := strings.TrimSuffix(name, c.PrimaryExt())
		if filepath.Ext(name) == c.PrimaryExt() && isNumericID(stem) {
			return KindNote, true
		}
	case KindJournal:
		stem := strings.TrimSuffix(name, c.PrimaryExt())
		if filepath.Ext(name) == c.PrimaryExt() && isNumericID(stem) {
			return KindJournal, true
		}
	case KindTemplate:
		stem := strings.TrimSuffix(name, ".template"+c.PrimaryExt())
		if strings.HasSuffix(name, ".template"+c.PrimaryExt()) && isNumericID(stem) {
			return KindTemplate, true
		}
	}
	return "", false
}

func isNumericID(name string) bool {
	if name == "" {
		return false
	}
	_, err := strconv.ParseInt(name, 10, 64)
	return err == nil
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

// RenamesPath returns the vault-local title rename history path.
func (c *Config) RenamesPath() string {
	return filepath.Join(c.TrackDir(), "renames.yaml")
}
