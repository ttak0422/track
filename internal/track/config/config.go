// Package config centralizes track's runtime configuration: where notes live, where the index database and sidecar metadata live, and which file extensions count as notes.
// Keeping these in one place lets future file types and the (future) LSP server share the same resolution logic.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// JournalOff and GenOff turn off this vault's daily journals and generation snapshots. They are
	// named for the disabled state so a zero-value Config keeps the default behaviour, while the
	// config file reads positively (`journal: false`). A vault that is checked into a repository or
	// published wants both off: the journal tree records when its author worked, and .track/gen/
	// carries a full copy of every past state — neither belongs in a public history, and both are
	// written without anyone asking for them.
	JournalOff bool
	GenOff     bool
	// WebHome names the note (by title or numeric id) that the web workspace opens as its landing view
	// instead of the search hero. Empty keeps the search home. A dashboard note (one with ```dashboard
	// widget blocks) is the intended target. Resolved to a note id by the web layer, not here.
	WebHome string
	// WebIcon is the site icon image, as a path relative to the vault root (e.g. "assets/logo.png").
	// The static-site exporter publishes it as the site's brand mark and favicon; empty keeps the
	// built-in track mark.
	WebIcon string
	// Icons maps a tag or note kind to an emoji/icon shown beside note titles in lists, search, and the
	// static-site navigation. A per-note sidecar override (Metadata.Icon) wins over both maps; see
	// NoteIcon.
	Icons IconMap
	// Properties is the optional per-key note-property schema (config `properties:`): a declared
	// value type and/or enum candidates. Keys not listed here are unconstrained.
	Properties map[string]PropSpec
	// Queries names saved query expressions (config `queries:`), runnable as `track query --saved
	// <name>` and referenced from a ```track-query fence as "saved: <name>". A bad expression fails
	// when run, not at load, so a typo never blocks unrelated commands.
	Queries map[string]string
	// CaptureInbox is the default target for `track capture` when --target is omitted: a note title,
	// optionally with a "#heading" anchor (e.g. "Inbox#Tasks"). The note is created on first capture
	// when missing; a named heading must already exist.
	CaptureInbox string
	// ArchiveNote is the title of the note `track archive` moves subtrees into, with "{{year}}"
	// substituted for the current year so archives partition per year (e.g. "Archive 2026").
	ArchiveNote string
	// Vaults is the machine config's named vault registry (name -> absolute path, symlinks intact).
	// Engine layers use the names as the cross-vault reference gate: a [[name:title]] prefix is a
	// vault qualifier only when name is registered here. Empty when no registry is configured.
	Vaults map[string]string
	// VaultName is this vault's own entry in that registry, or "" when it is not registered (the
	// default vault, or a direnv-style TRACK_VAULT path). It is the label every surface reports the
	// vault under and the name other vaults reference it by, so it is resolved once here rather than
	// rediscovered by each caller walking Vaults and comparing canonical paths. The registry gives a
	// vault exactly one name (resolveVaults refuses a second), so there is one answer or none.
	VaultName string
}

// IconMap holds the tag→icon and kind→icon lookups resolved from config. Both are optional; an unset map
// simply never matches.
type IconMap struct {
	Tags  map[string]string
	Kinds map[string]string
}

// NoteIcon resolves the icon shown beside a note title. A non-empty per-note override (the sidecar's
// Metadata.Icon) always wins; otherwise the first tag with a mapping (tags are checked in the order they
// are stored) is used, then the note kind's mapping, then "" for no icon. Keeping this on Config means
// every surface resolves an icon the same way: the live workspace's search and the static export.
func (c *Config) NoteIcon(kind string, tags []string, override string) string {
	if override != "" {
		return override
	}
	for _, t := range tags {
		if ic, ok := c.Icons.Tags[t]; ok && ic != "" {
			return ic
		}
	}
	if ic, ok := c.Icons.Kinds[kind]; ok && ic != "" {
		return ic
	}
	return ""
}

// PropSpec constrains one property key: Type is a value type ("string", "number", "boolean",
// "date", "link"; empty means unconstrained) applied to each item of a list value, and Values is an
// optional enum of accepted value texts. Doctor reports violations; the LSP completes Values.
type PropSpec struct {
	Type   string   `yaml:"type"`
	Values []string `yaml:"values"`
}

// machineFileConfig is the user config file (~/.config/track/config.yml or TRACK_CONFIG): it owns the
// machine and the user — where the vault and cache live, and how the local web UI looks. Note semantics belong to the vault
// config instead (see vaultFileConfig); a vault-scope key here is a hard error so the split stays real.
type machineFileConfig struct {
	VaultDir     string           `yaml:"vault_dir"`
	DefaultVault string           `yaml:"default_vault"`
	CacheDir     string           `yaml:"cache_dir"`
	Web          machineWebConfig `yaml:"web"`
	// Vaults is the named vault registry (name -> path) behind the global --vault flag and the
	// `track vault` subcommands. It lives in the machine config only: which vaults exist on this
	// machine is machine state, and a synced vault must never introduce new vault paths.
	Vaults map[string]string `yaml:"vaults"`
}

// vaultFileConfig is <vault>/.track/config.yml: the note semantics a vault carries with it — id/date
// formats, task states, property schema, saved queries, icons, templates, capture targets. Path and
// command values are deliberately excluded: a cloned or synced vault must never decide which commands
// run on this machine or where its index lives, so those keys are a hard error here.
type vaultFileConfig struct {
	Extensions        []string            `yaml:"extensions"`
	DateFormat        string              `yaml:"date_format"`
	JournalDateFormat string              `yaml:"journal_date_format"`
	DefaultTemplate   string              `yaml:"default_template"`
	JournalTemplate   string              `yaml:"journal_template"`
	GenKeep           int                 `yaml:"gen_keep"`
	Journal           *bool               `yaml:"journal"`
	Gen               *bool               `yaml:"gen"`
	Properties        map[string]PropSpec `yaml:"properties"`
	Queries           map[string]string   `yaml:"queries"`
	CaptureInbox      string              `yaml:"capture_inbox"`
	ArchiveNote       string              `yaml:"archive_note"`
	Web               vaultWebConfig      `yaml:"web"`
	Icons             iconsFileConfig     `yaml:"icons"`
}

// machineWebConfig is the machine config's `web:` block: how the web UI looks on this machine. The
// colorscheme is kept out of the config file itself: colors_path points to a separate palette file
// (see webui.LoadPalette) so the palette can be edited and shared independently of the main config.
type machineWebConfig struct {
	Theme      string `yaml:"theme"`
	ColorsPath string `yaml:"colors_path"`
}

// vaultWebConfig is the vault config's `web:` block: what the workspace shows for this vault.
type vaultWebConfig struct {
	// Home names the landing note (title or numeric id) the workspace opens instead of the search hero.
	Home string `yaml:"home"`
	// Icon is the site icon image, as a path relative to the vault root (see Config.WebIcon).
	Icon string `yaml:"icon"`
}

// iconsFileConfig is the config.yml `icons:` block: two optional maps from tag/kind to an emoji.
type iconsFileConfig struct {
	Tags  map[string]string `yaml:"tags"`
	Kinds map[string]string `yaml:"kinds"`
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

// Load resolves configuration from two files with disjoint key ownership: the machine config (the
// fixed user config file, ~/.config/track/config.yml or the platform equivalent) owns machine and
// user values — vault_dir, cache_dir, web.theme, web.colors_path — and the vault
// config (<vault>/.track/config.yml) owns the note semantics that travel with the vault. Both files
// are decoded strictly: a key in the wrong file is a hard error, never a silent fallback.
//
// Every configuration key can be overridden from the environment by one rule: TRACK_ plus the key,
// upper-snake — TRACK_CACHE_DIR sets cache_dir, TRACK_GEN_KEEP sets gen_keep, and TRACK_VAULTS_<NAME>
// sets one entry of vaults:. Each variable sets exactly the thing it names, so a TRACK_VAULTS_ entry
// adds (or replaces) one vault and leaves the rest of the registry alone.
//
// Two variables sit outside that rule because neither names a key: TRACK_CONFIG is the machine config
// file itself, and TRACK_VAULT selects the active vault by path — which vault_dir cannot express once
// a registry exists, since it is refused there. When neither the machine config nor TRACK_VAULT sets a
// vault, it defaults to $HOME/track (ADR 0015).
func Load() (*Config, error) {
	return load("")
}

// LoadAt resolves configuration for a specific vault directory, ignoring the TRACK_VAULT override.
// Long-lived processes (the LSP server) use it to address another registered vault — reading its
// vault config and deriving its cache DB — without mutating their own environment the way the CLI's
// one-shot setenv selection does.
func LoadAt(vaultPath string) (*Config, error) {
	if strings.TrimSpace(vaultPath) == "" {
		return nil, fmt.Errorf("LoadAt: vault path is empty")
	}
	return load(vaultPath)
}

func load(fixedVault string) (*Config, error) {
	mc, err := loadMachineConfig()
	if err != nil {
		return nil, err
	}
	// Validate the registry on every load so a malformed entry fails loudly now, not on the first
	// --vault.
	registry, namesByPath, err := resolveVaults(vaultEntries(mc))
	if err != nil {
		return nil, err
	}

	configured, err := configuredVault(mc, registry)
	if err != nil {
		return nil, err
	}
	rawVault := fixedVault
	if rawVault == "" {
		rawVault = configured
		if env := os.Getenv("TRACK_VAULT"); env != "" {
			rawVault = env
		}
	}
	if rawVault == "" {
		// With nothing configured and no TRACK_VAULT, default to $HOME/track (ADR 0015).
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("no vault is configured and the home directory is unavailable: %w", err)
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

	vc, err := loadVaultConfig(vault)
	if err != nil {
		return nil, err
	}
	if err := validateVaultConfig(vc); err != nil {
		return nil, err
	}

	// The index is a derived cache, never a configured file: it lives under the cache dir keyed by the
	// vault path, so every vault the registry names keeps its own index without anyone naming it.
	cacheDir := mc.CacheDir
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
	db := filepath.Join(expandHome(cacheDir), vaultCacheKey(vault), "index.db")

	extensions := vc.Extensions
	if len(extensions) == 0 {
		extensions = []string{".md"}
	}
	dateFormat := vc.DateFormat
	if dateFormat == "" {
		dateFormat = "2006-01-02"
	}
	journalDateFormat := vc.JournalDateFormat
	if journalDateFormat == "" {
		journalDateFormat = "20060102"
	}

	defaultTemplate := vc.DefaultTemplate
	if env := os.Getenv("TRACK_DEFAULT_TEMPLATE"); env != "" {
		defaultTemplate = env
	}
	journalTemplate := vc.JournalTemplate
	if env := os.Getenv("TRACK_JOURNAL_TEMPLATE"); env != "" {
		journalTemplate = env
	}

	genKeep := vc.GenKeep
	if env := os.Getenv("TRACK_GEN_KEEP"); env != "" {
		if n, err := strconv.Atoi(env); err == nil {
			genKeep = n
		}
	}
	if genKeep < 1 {
		genKeep = 10
	}

	if err := validateProperties(vc.Properties); err != nil {
		return nil, err
	}

	captureInbox := vc.CaptureInbox
	if env := os.Getenv("TRACK_CAPTURE_INBOX"); env != "" {
		captureInbox = env
	}
	if strings.TrimSpace(captureInbox) == "" {
		captureInbox = "Inbox"
	}
	archiveNote := vc.ArchiveNote
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
		WebTheme:          normalizeWebTheme(mc.Web.Theme),
		WebColorsPath:     resolveColorsPath(mc.Web.ColorsPath),
		VaultDirDisplay:   displayVault,
		DefaultTemplate:   defaultTemplate,
		JournalTemplate:   journalTemplate,
		GenKeep:           genKeep,
		JournalOff:        vc.Journal != nil && !*vc.Journal,
		GenOff:            vc.Gen != nil && !*vc.Gen,
		WebHome:           strings.TrimSpace(vc.Web.Home),
		WebIcon:           strings.TrimSpace(vc.Web.Icon),
		Icons:             IconMap{Tags: vc.Icons.Tags, Kinds: vc.Icons.Kinds},
		Properties:        vc.Properties,
		Queries:           vc.Queries,
		CaptureInbox:      captureInbox,
		ArchiveNote:       archiveNote,
		Vaults:            registry,
		// The registry is already indexed by canonical path (that is how it refuses two names for one
		// vault), so this vault's name is a lookup on the path it resolved to — including under LoadAt,
		// where the Config represents whichever vault was named rather than the configured one.
		VaultName: namesByPath[vault],
	}, nil
}

// vaultNamePattern constrains registry names: lowercase ASCII letters, digits, and dashes, so a name
// can later prefix a cross-vault link (vault:title) and never needs quoting or escaping.
var vaultNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Vaults returns the named vault registry from the machine config (name -> absolute path, symlinks
// intact). An unset registry is an empty map, never an error.
func Vaults() (map[string]string, error) {
	mc, err := loadMachineConfig()
	if err != nil {
		return nil, err
	}
	registry, _, err := resolveVaults(vaultEntries(mc))
	return registry, err
}

// configuredVault returns the vault path the machine config selects, or "" when it selects none.
//
// A vault is designated one way at a time. Without a registry there are no names, so `vault_dir`
// gives a path. With a registry every vault already has a name and a path, so `default_vault` picks
// one by name and the path is written once, under `vaults:`. Allowing both would mean writing the
// same vault twice — and, because a bare word is a valid relative path, a name typed into
// `vault_dir` would silently resolve under the working directory and get a vault skeleton laid down
// there (the typo-creates-a-vault failure ADR 0004 exists to prevent).
func configuredVault(mc machineFileConfig, registry map[string]string) (string, error) {
	name := strings.TrimSpace(mc.DefaultVault)
	dir := strings.TrimSpace(mc.VaultDir)
	if len(registry) > 0 {
		if dir != "" {
			return "", fmt.Errorf("vault_dir cannot be combined with a vaults: registry; name the active vault with default_vault instead")
		}
		if name == "" {
			return "", nil
		}
		path, ok := registry[name]
		if !ok {
			return "", fmt.Errorf("default_vault: %q is not in vaults: (have %s)", name, strings.Join(sortedNames(registry), ", "))
		}
		return path, nil
	}
	if name != "" {
		return "", fmt.Errorf("default_vault: %q names a vault, but no vaults: registry is configured", name)
	}
	if dir != "" && !filepath.IsAbs(expandHome(dir)) {
		return "", fmt.Errorf("vault_dir: %q must be an absolute path (or start with ~/)", dir)
	}
	return dir, nil
}

// vaultEntries is the registry before validation: the machine config's vaults: map, overlaid with the
// TRACK_VAULTS_<NAME> environment entries. Each variable sets one entry, the way TRACK_CACHE_DIR sets
// one key — so an environment entry adds a vault (or replaces the same-named one) and never displaces
// the rest of the registry.
//
// This is what lets a checkout carry a vault. A repository cannot register itself: the registry is
// machine state, and a synced or cloned vault must never introduce vault paths (ADR 0051). But the
// shell entering that checkout can say so on the user's behalf — a devshell hook, a Makefile, a
// direnv .envrc the user allowed — which keeps the naming where consent already lives.
func vaultEntries(mc machineFileConfig) map[string]string {
	const prefix = "TRACK_VAULTS_"
	entries := make(map[string]string, len(mc.Vaults))
	for name, path := range mc.Vaults {
		entries[name] = path
	}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, prefix) {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		// An environment name cannot hold the dash a vault name uses, and a vault name cannot hold an
		// underscore, so lowercasing and mapping _ to - round-trips without ambiguity.
		name := strings.ReplaceAll(strings.ToLower(kv[len(prefix):eq]), "_", "-")
		value := strings.TrimSpace(kv[eq+1:])
		if name == "" || value == "" {
			continue
		}
		entries[name] = value
	}
	return entries
}

// resolveVaults validates registry names and expands each path. Paths must be absolute (after ~
// expansion): resolving a vault relative to the current directory would make the same name mean a
// different vault per invocation.
//
// It returns the registry (name -> cleaned path) and its inverse keyed by canonical path. The inverse
// is not an extra pass: it is the one-vault-one-name check's own bookkeeping, handed back so a caller
// that knows a vault's path — Load, for its own vault — reads the name off it instead of scanning the
// registry again.
func resolveVaults(vaults map[string]string) (map[string]string, map[string]string, error) {
	out := make(map[string]string, len(vaults))
	// One vault, one name. Two names for the same directory would make the vault's identity
	// ambiguous everywhere a name is reported rather than accepted — which of them labels a search
	// hit, which one a qualified id carries, which one a cross-vault link is written with — so the
	// registry refuses it outright instead of every reader having to pick a winner.
	byPath := make(map[string]string, len(vaults))
	for _, name := range sortedNames(vaults) {
		path := vaults[name]
		if !vaultNamePattern.MatchString(name) {
			return nil, nil, fmt.Errorf("vaults: name %q must be lowercase letters, digits, and dashes", name)
		}
		expanded := expandHome(strings.TrimSpace(path))
		if !filepath.IsAbs(expanded) {
			return nil, nil, fmt.Errorf("vaults: %s: %q must be an absolute path (or start with ~/)", name, path)
		}
		clean := filepath.Clean(expanded)
		// Compare canonically so two spellings of one directory (a symlink, a trailing slash) are
		// caught too, not just literally equal strings.
		key := clean
		if canonical, err := canonicalPath(clean); err == nil {
			key = canonical
		}
		if first, dup := byPath[key]; dup {
			return nil, nil, fmt.Errorf("vaults: %s and %s name the same vault (%s); give a vault exactly one name", first, name, clean)
		}
		byPath[key] = name
		out[name] = clean
	}
	return out, byPath, nil
}

// sortedNames returns map keys in a deterministic order, so a duplicate-path error always names the
// same pair regardless of map iteration order.
func sortedNames(vaults map[string]string) []string {
	names := make([]string, 0, len(vaults))
	for name := range vaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CanonicalPath makes a path absolute with symlinks resolved, tolerating missing trailing
// components — the same normalization VaultDir gets, so a registry path compares against it reliably.
func CanonicalPath(path string) (string, error) {
	return canonicalPath(path)
}

// validateVaultConfig rejects vault-config values that could steer a filesystem path outside the
// vault. The vault config syncs with the vault, so it is untrusted input (ADR 0050): extensions
// become part of every derived note path, the journal date format becomes the journal's file name,
// and a template spec may be a vault-relative path — none of them may carry separators upward, "..",
// or an absolute root. Environment overrides are machine-local and stay unrestricted.
func validateVaultConfig(vc vaultFileConfig) error {
	for _, ext := range vc.Extensions {
		if len(ext) < 2 || !strings.HasPrefix(ext, ".") || strings.ContainsAny(ext, `/\`) || strings.Contains(ext, "..") {
			return fmt.Errorf("vault config extensions: %q is not a plain %q-style extension", ext, ".md")
		}
	}
	if f := vc.JournalDateFormat; strings.ContainsAny(f, `/\`) || strings.Contains(f, "..") {
		return fmt.Errorf("vault config journal_date_format: %q must not contain path separators or \"..\" (it becomes the journal file name)", f)
	}
	for key, spec := range map[string]string{"default_template": vc.DefaultTemplate, "journal_template": vc.JournalTemplate} {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		if filepath.IsAbs(spec) || strings.Contains(spec, "..") {
			return fmt.Errorf("vault config %s: %q must be a template name or a vault-relative path without \"..\"", key, spec)
		}
	}
	return nil
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

// ConfigPath returns the fixed machine config path, or TRACK_CONFIG when set for tests and one-off runs.
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

// VaultConfigPath returns the vault config path for a vault directory. It is derived from the vault
// path alone so Load can read it before a Config exists.
func VaultConfigPath(vaultDir string) string {
	return filepath.Join(vaultDir, ".track", "config.yml")
}

func loadMachineConfig() (machineFileConfig, error) {
	var cfg machineFileConfig
	path := ConfigPath()
	if err := strictDecodeFile(path, &cfg); err != nil {
		if isUnknownFieldError(err) {
			// db_path was removed rather than moved elsewhere: the index is a derived cache keyed by
			// the vault path, so one fixed file could never serve the vaults a registry names.
			if strings.Contains(err.Error(), "db_path") {
				return machineFileConfig{}, fmt.Errorf("%w (db_path was removed; the index is a derived cache keyed by the vault path — use cache_dir to relocate it)", err)
			}
			return machineFileConfig{}, fmt.Errorf("%w (vault-scope keys such as properties, queries, and icons now live in <vault>/.track/config.yml)", err)
		}
		return machineFileConfig{}, err
	}
	return cfg, nil
}

func loadVaultConfig(vaultDir string) (vaultFileConfig, error) {
	var cfg vaultFileConfig
	path := VaultConfigPath(vaultDir)
	if err := strictDecodeFile(path, &cfg); err != nil {
		if isUnknownFieldError(err) {
			return vaultFileConfig{}, fmt.Errorf("%w (machine-scope keys — vault_dir, cache_dir, web.theme, web.colors_path — belong in the user config file, never in a vault)", err)
		}
		return vaultFileConfig{}, err
	}
	return cfg, nil
}

// isUnknownFieldError reports whether a strict-decode error is about an unrecognized key, so the
// which-file-owns-this-key hint is attached only where it applies — not to read failures or plain
// YAML syntax errors.
func isUnknownFieldError(err error) bool {
	return strings.Contains(err.Error(), "not found in type")
}

// strictDecodeFile reads a YAML config file into out, rejecting unknown keys so a value placed in the
// wrong file (or a typo) fails loudly instead of being silently ignored. A missing or empty file is a
// zero value, not an error; a second YAML document is an error, never silently dropped.
func strictDecodeFile(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config %s: a config file must be a single YAML document", path)
	}
	return nil
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
