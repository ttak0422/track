// Package config centralizes track's runtime configuration: where notes live, where the index database and sidecar metadata live, and which file extensions count as notes.
// Keeping these in one place lets future file types and the (future) LSP server share the same resolution logic.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	// WebTheme is the default theme the web UI boots with ("system", "light", or "dark"); empty means
	// "system". A user's in-browser choice is still stored client-side and overrides this default.
	WebTheme string
	// WebColorsPath is the resolved path to an optional palette file overriding the web UI's themeable
	// CSS colors. Empty means use the built-in palette. The file is read by the web layer, not here.
	WebColorsPath string
	// VaultDirDisplay is the configured vault path made absolute but with symlinks left intact, for
	// user-facing output (e.g. a copy-path action). VaultDir resolves symlinks for a stable cache key;
	// this keeps the friendlier path the user configured.
	VaultDirDisplay string
	// DefaultTemplate and JournalTemplate name the template applied when a note or journal is created
	// without an explicit --template (and without an inline body). Empty means fall back to a template
	// literally named "default" / "journal" when one exists, otherwise no template. A name or a vault
	// path is accepted, same as the --template flag.
	DefaultTemplate string
	JournalTemplate string
	// GenKeep is how many generation snapshots `gen increment` retains (count-based pruning).
	GenKeep int
	// EmbedderCommand is the optional command that turns a note's text into an embedding vector, split
	// into command and arguments. The engine feeds a note's text on stdin and reads a JSON array of
	// floats from stdout (see the similar package). Empty means no embedder is configured, so semantic
	// related-notes is unavailable and every other command is unaffected.
	EmbedderCommand []string
	// Properties is the optional per-key note-property schema (config `properties:`): a declared
	// value type and/or enum candidates. Keys not listed here are unconstrained.
	Properties map[string]PropSpec
	// CaptureInbox is the default target for `track capture` when --target is omitted: a note title,
	// optionally with a "#heading" anchor (e.g. "Inbox#Tasks"). The note is created on first capture
	// when missing; a named heading must already exist.
	CaptureInbox string
	// ArchiveNote is the title of the note `track archive` moves subtrees into, with "{{year}}"
	// substituted for the current year so archives partition per year (e.g. "Archive 2026").
	ArchiveNote string
}

// PropSpec constrains one property key: Type is a value type ("string", "number", "boolean",
// "date", "link"; empty means unconstrained) applied to each item of a list value, and Values is an
// optional enum of accepted value texts. Doctor reports violations; the LSP completes Values.
type PropSpec struct {
	Type   string   `yaml:"type"`
	Values []string `yaml:"values"`
}

type fileConfig struct {
	VaultDir          string              `yaml:"vault_dir"`
	DBPath            string              `yaml:"db_path"`
	CacheDir          string              `yaml:"cache_dir"`
	Extensions        []string            `yaml:"extensions"`
	DateFormat        string              `yaml:"date_format"`
	JournalDateFormat string              `yaml:"journal_date_format"`
	DefaultTemplate   string              `yaml:"default_template"`
	JournalTemplate   string              `yaml:"journal_template"`
	GenKeep           int                 `yaml:"gen_keep"`
	Embedder          argvList            `yaml:"embedder"`
	Properties        map[string]PropSpec `yaml:"properties"`
	CaptureInbox      string              `yaml:"capture_inbox"`
	ArchiveNote       string              `yaml:"archive_note"`
	Web               webFileConfig       `yaml:"web"`
}

// argvList is a command in config.yml that accepts two YAML shapes: a scalar string, split on
// whitespace (no shell quoting, so no argument can contain a space in this form), or a sequence used
// verbatim as argv, where arguments may contain spaces. Any other node kind is a config error.
type argvList []string

func (a *argvList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!null" { // `embedder:` with no value, or an explicit null
			*a = nil
			return nil
		}
		*a = strings.Fields(value.Value)
		return nil
	case yaml.SequenceNode:
		// Decode element by element: yaml.v3 silently drops null items when decoding into []string,
		// which would make a flag vanish (or shift argv[0]) instead of failing loudly at load.
		argv := make([]string, len(value.Content))
		for i, item := range value.Content {
			var s *string
			if err := item.Decode(&s); err != nil {
				return fmt.Errorf("embedder: %w", err)
			}
			if s == nil {
				return fmt.Errorf("embedder: list element %d is null, want a string", i+1)
			}
			argv[i] = *s
		}
		*a = argv
		return nil
	default:
		return fmt.Errorf("embedder: must be a string (\"cmd --arg\") or a list of strings ([cmd, --arg]), got a %s", nodeKindName(value.Kind))
	}
}

// nodeKindName names a YAML node kind for error messages.
func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		return "scalar"
	default:
		return "unsupported node"
	}
}

// webFileConfig holds web-only settings read from config.yml. The colorscheme is kept out of this file:
// colors_path points to a separate palette file (see webui.LoadPalette) so the palette can be edited
// and shared independently of the main config.
type webFileConfig struct {
	Theme      string `yaml:"theme"`
	ColorsPath string `yaml:"colors_path"`
}

const (
	KindNote     = "note"
	KindJournal  = "journal"
	KindTemplate = "template"
)

// AssetsDirName is the single top-level vault directory that holds media/attachments for every note
// kind (<vault>/assets). It is a sibling of note/ and journal/, so note scanning (which walks only
// those trees) never treats its files as notes. A note references an attachment with the relative
// path "assets/<file>".
const AssetsDirName = "assets"

// DataDirName is the top-level vault directory for Canonical Data Model JSONL (prices.jsonl,
// events.jsonl, ...). It is where track-fetch-* tools write their output; the files themselves are the
// source of truth (track keeps no separate data store). A View Spec references them by path.
const DataDirName = "data"

// Load resolves configuration from the fixed user config file, with environment overrides for tests
// and one-off debugging. The default file is ~/.config/track/config.yml on XDG-style systems, or the
// platform user config equivalent.
//
// TRACK_CONFIG overrides the config file path. TRACK_VAULT, TRACK_DB, and TRACK_CACHE_DIR override
// the matching resolved values. When neither the config file nor TRACK_VAULT sets a vault, it
// defaults to $HOME/track (ADR 0015).
func Load() (*Config, error) {
	fc, err := loadFileConfig()
	if err != nil {
		return nil, err
	}

	rawVault := fc.VaultDir
	if env := os.Getenv("TRACK_VAULT"); env != "" {
		rawVault = env
	}
	if rawVault == "" {
		// With no config_file vault_dir and no TRACK_VAULT, default to $HOME/track (ADR 0015).
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("vault_dir is unset and the home directory is unavailable: %w", err)
		}
		rawVault = filepath.Join(home, "track")
	}
	// displayVault keeps the configured path absolute but symlink-intact, so user-facing paths read as
	// the vault the user knows (e.g. ~/track) rather than its resolved target (~/OneDrive/track).
	displayVault, err := filepath.Abs(expandHome(rawVault))
	if err != nil {
		return nil, err
	}
	vault, err := canonicalPath(rawVault)
	if err != nil {
		return nil, err
	}

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

	defaultTemplate := fc.DefaultTemplate
	if env := os.Getenv("TRACK_DEFAULT_TEMPLATE"); env != "" {
		defaultTemplate = env
	}
	journalTemplate := fc.JournalTemplate
	if env := os.Getenv("TRACK_JOURNAL_TEMPLATE"); env != "" {
		journalTemplate = env
	}

	genKeep := fc.GenKeep
	if env := os.Getenv("TRACK_GEN_KEEP"); env != "" {
		if n, err := strconv.Atoi(env); err == nil {
			genKeep = n
		}
	}
	if genKeep < 1 {
		genKeep = 10
	}

	// TRACK_EMBEDDER replaces the config value entirely; an env var cannot carry an array, so it is
	// always whitespace-split — arguments containing spaces need the config sequence form.
	embedder := []string(fc.Embedder)
	if env := os.Getenv("TRACK_EMBEDDER"); env != "" {
		embedder = strings.Fields(env)
	}
	if len(embedder) > 0 && strings.TrimSpace(embedder[0]) == "" {
		return nil, fmt.Errorf("embedder: the first element must be the command, got an empty string")
	}

	if err := validateProperties(fc.Properties); err != nil {
		return nil, err
	}

	captureInbox := fc.CaptureInbox
	if env := os.Getenv("TRACK_CAPTURE_INBOX"); env != "" {
		captureInbox = env
	}
	if strings.TrimSpace(captureInbox) == "" {
		captureInbox = "Inbox"
	}
	archiveNote := fc.ArchiveNote
	if env := os.Getenv("TRACK_ARCHIVE_NOTE"); env != "" {
		archiveNote = env
	}
	if strings.TrimSpace(archiveNote) == "" {
		archiveNote = "Archive {{year}}"
	}

	return &Config{
		VaultDir:          vault,
		DBPath:            db,
		Extensions:        extensions,
		DateFormat:        dateFormat,
		JournalDateFormat: journalDateFormat,
		BabelLanguages:    loadBabelLanguages(),
		WebTheme:          normalizeWebTheme(fc.Web.Theme),
		WebColorsPath:     resolveColorsPath(fc.Web.ColorsPath),
		VaultDirDisplay:   displayVault,
		DefaultTemplate:   defaultTemplate,
		JournalTemplate:   journalTemplate,
		GenKeep:           genKeep,
		EmbedderCommand:   embedder,
		Properties:        fc.Properties,
		CaptureInbox:      captureInbox,
		ArchiveNote:       archiveNote,
	}, nil
}

// validateProperties rejects a schema entry whose declared type is not a property value type, so a
// config typo fails loudly at load instead of silently never matching any value.
func validateProperties(props map[string]PropSpec) error {
	for key, spec := range props {
		switch spec.Type {
		case "", "string", "number", "boolean", "date", "link":
		default:
			return fmt.Errorf("properties.%s: unknown type %q (want string, number, boolean, date, or link)", key, spec.Type)
		}
	}
	return nil
}

// archiveYear matches the "{{year}}" placeholder (with optional inner whitespace) in ArchiveNote.
var archiveYear = regexp.MustCompile(`\{\{\s*year\s*\}\}`)

// ArchiveNoteTitle resolves ArchiveNote for a given time, substituting "{{year}}" with now's year so
// `track archive` targets a per-year note (e.g. "Archive 2026"). A configured title without the
// placeholder is used verbatim, giving a single archive note.
func (c *Config) ArchiveNoteTitle(now time.Time) string {
	return archiveYear.ReplaceAllString(c.ArchiveNote, strconv.Itoa(now.Year()))
}

// resolveColorsPath expands and absolutizes an optional palette path; empty stays empty.
func resolveColorsPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if abs, err := filepath.Abs(expandHome(path)); err == nil {
		return abs
	}
	return expandHome(path)
}

// normalizeWebTheme keeps only the recognized theme values; anything else (including empty) becomes
// "system", so a stray config value can never inject an unexpected attribute into the served page.
func normalizeWebTheme(theme string) string {
	switch theme {
	case "light", "dark", "system":
		return theme
	default:
		return "system"
	}
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

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(expandHome(path))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}

	existing := abs
	var missing []string
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return abs, nil
		}
		missing = append(missing, filepath.Base(existing))
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return abs, nil
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	return resolved, nil
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

// TemplateDir returns the directory used for user template markdown files.
func (c *Config) TemplateDir() string {
	return filepath.Join(c.VaultDir, KindTemplate)
}

// DataDir returns the directory that holds Canonical Data Model JSONL files (see DataDirName).
func (c *Config) DataDir() string {
	return filepath.Join(c.VaultDir, DataDirName)
}

// AssetsDir returns the vault's single assets directory (<vault>/assets) that holds media/attachments
// for every note kind. The directory is not created.
func (c *Config) AssetsDir() string {
	return filepath.Join(c.VaultDir, AssetsDirName)
}

// VaultSkeleton lists the directories that make up an initialized vault: the note and journal trees,
// the shared assets directory, the template directory, the canonical-data directory, and the sidecar
// metadata directory.
func (c *Config) VaultSkeleton() []string {
	return []string{
		c.NoteDir(),
		c.JournalDir(),
		c.AssetsDir(),
		c.TemplateDir(),
		c.DataDir(),
		c.MetadataDir(),
	}
}

// EnsureVaultSkeleton creates any missing directories of the vault layout and returns the ones it
// created. It is idempotent: directories that already exist are left untouched.
func (c *Config) EnsureVaultSkeleton() ([]string, error) {
	var created []string
	for _, dir := range c.VaultSkeleton() {
		if _, err := os.Stat(dir); err == nil {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return created, fmt.Errorf("create %s: %w", dir, err)
		}
		created = append(created, dir)
	}
	return created, nil
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

// DisplayPathForKind is PathForKind rebased onto the symlink-intact display vault, for user-facing
// output. It falls back to the canonical path when no separate display path is configured.
func (c *Config) DisplayPathForKind(kind string, id int64) string {
	canonical := c.PathForKind(kind, id)
	if c.VaultDirDisplay == "" || c.VaultDirDisplay == c.VaultDir {
		return canonical
	}
	rel, err := filepath.Rel(c.VaultDir, canonical)
	if err != nil {
		return canonical
	}
	return filepath.Join(c.VaultDirDisplay, rel)
}

// KindFromPath classifies a vault file by its managed directory.
func (c *Config) KindFromPath(path string) (string, bool) {
	abs, err := canonicalPath(path)
	if err != nil {
		return "", false
	}
	vault, err := canonicalPath(c.VaultDir)
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

// GenDir returns the generation snapshot store (<vault>/.track/gen). It lives under .track so note
// scanning never indexes it, and generations never snapshot it; it stays inside the vault so cloud
// sync carries undo history to every device.
func (c *Config) GenDir() string {
	return filepath.Join(c.TrackDir(), "gen")
}

// TrashDir returns where `track rm` moves soft-deleted note files and their sidecars.
func (c *Config) TrashDir() string {
	return filepath.Join(c.TrackDir(), "trash")
}
