// Package editorview presents machine-local, readable symlinks without changing note identity.
// Aliases are pinned indefinitely: this package deliberately has no cleanup, rename or copy-back.
package editorview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
)

const Env = "TRACK_EDITOR_VIEW"
const manifestVersion = 1

type Entry struct {
	ID         int64  `json:"id"`
	FileKind   string `json:"file_kind"`
	Path       string `json:"path"`
	EditorPath string `json:"editor_path"`
	Title      string `json:"title"`
	VaultDir   string `json:"vault_dir"`
}
type manifest struct {
	Version int              `json:"version"`
	Vault   string           `json:"vault"`
	Entries map[string]Entry `json:"entries"`
}

func Enabled() bool { return os.Getenv(Env) != "" }

// Root requires an explicit owner-private location outside all known vaults. A chosen directory is
// an operator assertion that it is local and not synchronized: unknown sync products cannot be detected.
func Root(cfg *config.Config) (string, error) {
	root := os.Getenv(Env)
	if root == "" {
		return "", errors.New("readable editor view is disabled; set TRACK_EDITOR_VIEW to an absolute, nonsynced local directory")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "", errors.New("readable editor views require a local Linux or macOS filesystem; other platforms are unsupported")
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("TRACK_EDITOR_VIEW must be an absolute path")
	}
	root, err := config.CanonicalPath(root)
	if err != nil {
		return "", err
	}
	for _, component := range strings.Split(filepath.ToSlash(root), "/") {
		lower := strings.ToLower(component)
		if strings.HasPrefix(lower, "onedrive") || lower == "dropbox" || lower == "google drive" || lower == "googledrive" || lower == "mobile documents" || lower == "cloudstorage" || lower == "icloud drive" {
			return "", fmt.Errorf("editor view must not be in a known sync directory: %s", root)
		}
	}
	vaults := []string{cfg.VaultDir}
	for _, v := range cfg.Vaults {
		vaults = append(vaults, v)
	}
	for _, v := range vaults {
		cv, err := config.CanonicalPath(v)
		if err != nil {
			return "", err
		}
		if within(root, cv) || within(cv, root) {
			return "", fmt.Errorf("editor view and vault must not overlap: %s", cv)
		}
	}
	return root, nil
}
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func vaultKey(v string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(v)))
	return hex.EncodeToString(sum[:8])
}
func key(kind string, id int64) string { return kind + "/" + strconv.FormatInt(id, 10) }

// SafeTitle v1 preserves Unicode as authored, strips controls/formatting and forbidden punctuation,
// and truncates only the title at a UTF-8 boundary. Full identity is always in the filename suffix.
// NFC conversion is intentionally deferred: IDs and filesystem collision checks provide uniqueness.
func SafeTitle(title string, maxBytes int) string {
	var b strings.Builder
	for _, r := range title {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune(`/\\<>:"|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	title = strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if title == "" || title == "." || title == ".." {
		title = "Untitled"
	}
	if strings.HasPrefix(title, ".") {
		title = "_" + title
	}
	device := strings.ToUpper(strings.SplitN(title, ".", 2)[0])
	reserved := device == "CON" || device == "PRN" || device == "AUX" || device == "NUL"
	if len(device) == 4 && (strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT")) && device[3] >= '1' && device[3] <= '9' {
		reserved = true
	}
	if reserved {
		title = "_" + title
	}
	if len(title) > maxBytes {
		title = title[:maxBytes]
		for !utf8.ValidString(title) {
			title = title[:len(title)-1]
		}
	}
	title = strings.TrimRight(title, ". ")
	if title == "" {
		title = "Untitled"
		if len(title) > maxBytes {
			title = title[:maxBytes]
		}
	}
	return title
}
func privateDir(path string, create bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && create {
		if err = os.Mkdir(path, 0700); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("editor view directory is not a real directory: %s", path)
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("editor view directory must be owner-private (0700): %s", path)
	}
	return nil
}
func layout(cfg *config.Config, create bool) (string, error) {
	root, err := Root(cfg)
	if err != nil {
		return "", err
	}
	if create {
		if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
			return "", err
		}
	}
	if err = privateDir(root, create); err != nil {
		return "", err
	}
	vault, err := config.CanonicalPath(cfg.VaultDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, vaultKey(vault))
	if err = privateDir(dir, create); err != nil {
		return "", err
	}
	return dir, nil
}
func readManifest(cfg *config.Config, dir string, allowMissing bool) (manifest, error) {
	vault, err := config.CanonicalPath(cfg.VaultDir)
	if err != nil {
		return manifest{}, err
	}
	m := manifest{Version: manifestVersion, Vault: vault, Entries: map[string]Entry{}}
	path := filepath.Join(dir, "manifest.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && allowMissing {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return m, fmt.Errorf("unsafe editor manifest: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	if m.Version != manifestVersion || m.Vault != vault || m.Entries == nil {
		return m, errors.New("editor manifest version or canonical vault identity mismatch")
	}
	for k, e := range m.Entries {
		if k != key(e.FileKind, e.ID) {
			return m, errors.New("editor manifest identity key mismatch")
		}
	}
	return m, nil
}
func writeManifest(dir string, m manifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, filepath.Join(dir, "manifest.json"))
}
func canonicalEntry(cfg *config.Config, kind string, id int64, title string) (Entry, error) {
	if (kind != config.KindNote && kind != config.KindJournal) || id <= 0 {
		return Entry{}, errors.New("editor view supports positive note/journal IDs only")
	}
	vault, err := config.CanonicalPath(cfg.VaultDir)
	if err != nil {
		return Entry{}, err
	}
	path := filepath.Join(vault, kind, strconv.FormatInt(id, 10)+cfg.PrimaryExt())
	// Reject symlinked canonical kind directories and masters, including dangling links.
	d, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return Entry{}, err
	}
	if !d.IsDir() || d.Mode()&os.ModeSymlink != 0 {
		return Entry{}, errors.New("canonical kind directory must not be a symlink")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, fmt.Errorf("canonical note unavailable; do not recreate it through an alias: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Entry{}, errors.New("canonical note must be a regular file")
	}
	return Entry{ID: id, FileKind: kind, Path: path, Title: title, VaultDir: vault, EditorPath: path}, nil
}
func validateAlias(cfg *config.Config, dir string, e Entry) (Entry, error) {
	expected, err := canonicalEntry(cfg, e.FileKind, e.ID, e.Title)
	if err != nil {
		return Entry{}, err
	}
	if e.Path != expected.Path || e.VaultDir != expected.VaultDir || filepath.Dir(e.EditorPath) != filepath.Join(dir, e.FileKind) || !strings.HasSuffix(filepath.Base(e.EditorPath), "--"+strconv.FormatInt(e.ID, 10)+cfg.PrimaryExt()) {
		return Entry{}, errors.New("editor manifest entry does not match canonical identity")
	}
	if err = privateDir(filepath.Dir(e.EditorPath), false); err != nil {
		return Entry{}, err
	}
	info, err := os.Lstat(e.EditorPath)
	if err != nil {
		return Entry{}, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return Entry{}, fmt.Errorf("editor recovery conflict: %s is no longer a symlink; its content is preserved, do not overwrite the master", e.EditorPath)
	}
	target, err := os.Readlink(e.EditorPath)
	if err != nil {
		return Entry{}, err
	}
	if target != e.Path {
		return Entry{}, fmt.Errorf("editor alias target changed: %s", e.EditorPath)
	}
	return e, nil
}

// Ensure creates or reuses a pinned manifest-owned alias. Unknown occupants are never replaced.
func Ensure(cfg *config.Config, kind string, id int64, title string) (Entry, error) {
	e, err := canonicalEntry(cfg, kind, id, title)
	if err != nil {
		return Entry{}, err
	}
	if !Enabled() {
		return e, nil
	}
	dir, err := layout(cfg, true)
	if err != nil {
		return Entry{}, err
	}
	lock := filepath.Join(dir, ".publish-lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return Entry{}, fmt.Errorf("editor view publication is locked; if a process crashed, reconcile the lock manually: %w", err)
	}
	defer os.Remove(lock)
	m, err := readManifest(cfg, dir, true)
	if err != nil {
		return Entry{}, err
	}
	k := key(kind, id)
	if old, ok := m.Entries[k]; ok {
		old.Title = title
		return validateAlias(cfg, dir, old)
	}
	kindDir := filepath.Join(dir, kind)
	if err = privateDir(kindDir, true); err != nil {
		return Entry{}, err
	}
	suffix := "--" + strconv.FormatInt(id, 10) + cfg.PrimaryExt()
	budget := 240 - len(suffix)
	if pathBudget := 3800 - len(kindDir) - 1 - len(suffix); pathBudget < budget {
		budget = pathBudget
	}
	if budget < 8 {
		return Entry{}, errors.New("editor view root is too long for a readable filename")
	}
	e.EditorPath = filepath.Join(kindDir, SafeTitle(title, budget)+suffix)
	if _, err = os.Lstat(e.EditorPath); !os.IsNotExist(err) {
		if err != nil {
			return Entry{}, err
		}
		return Entry{}, fmt.Errorf("unowned editor view occupant: %s", e.EditorPath)
	}
	if err = os.Symlink(e.Path, e.EditorPath); err != nil {
		return Entry{}, err
	}
	m.Entries[k] = e
	if err = writeManifest(dir, m); err != nil {
		return Entry{}, fmt.Errorf("alias created but manifest publication failed; preserve and reconcile %s: %w", e.EditorPath, err)
	}
	return e, nil
}

// Resolve validates a canonical path or exact, manifest-owned alias. Numeric alias suffixes alone
// never confer identity. It performs no writes and never repairs a dangling or replaced alias.
func Resolve(cfg *config.Config, path string) (Entry, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Entry{}, err
	}
	absolute = filepath.Clean(absolute)
	if Enabled() {
		root, err := Root(cfg)
		if err != nil {
			return Entry{}, err
		}
		if within(absolute, root) {
			dir, err := layout(cfg, false)
			if err != nil {
				return Entry{}, err
			}
			m, err := readManifest(cfg, dir, false)
			if err != nil {
				return Entry{}, err
			}
			for k, e := range m.Entries {
				if e.EditorPath == absolute {
					if k != key(e.FileKind, e.ID) {
						return Entry{}, errors.New("editor manifest identity key mismatch")
					}
					return validateAlias(cfg, dir, e)
				}
			}
			return Entry{}, errors.New("path is not a manifest-owned editor alias")
		}
	}
	// Canonical vault-prefix symlinks remain supported, but arbitrary title-file symlinks do not.
	id, err := note.IDFromPath(absolute)
	if err != nil {
		return Entry{}, err
	}
	kind, ok := cfg.KindFromPath(absolute)
	if !ok {
		return Entry{}, errors.New("path is not a canonical vault note or approved editor alias")
	}
	e, err := canonicalEntry(cfg, kind, id, "")
	if err != nil {
		return Entry{}, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Entry{}, err
	}
	if resolved != e.Path {
		return Entry{}, errors.New("path target does not match canonical note identity")
	}
	return e, nil
}

// ViewRoot returns the validated vault-specific root (it does not create it).
func ViewRoot(cfg *config.Config) (string, error) { return layout(cfg, false) }

// IsViewPath is only a routing hint. Call Discover/Resolve before trusting an identity.
func IsViewPath(path string) bool {
	root := os.Getenv(Env)
	if root == "" || !filepath.IsAbs(root) {
		return false
	}
	root, err := config.CanonicalPath(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	return err == nil && within(filepath.Clean(path), root)
}

// Discover obtains a view path's vault from its private manifest and then validates the complete
// identity. This is used at the CLI boundary before the active vault has been selected.
func Discover(path string) (*config.Config, Entry, error) {
	if !IsViewPath(path) {
		return nil, Entry{}, errors.New("path is outside the configured editor view")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, Entry{}, err
	}
	root, err := config.CanonicalPath(os.Getenv(Env))
	if err != nil {
		return nil, Entry{}, err
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil {
		return nil, Entry{}, err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 || (parts[1] != config.KindNote && parts[1] != config.KindJournal) {
		return nil, Entry{}, errors.New("not an editor alias layout")
	}
	if err = privateDir(root, false); err != nil {
		return nil, Entry{}, err
	}
	dir := filepath.Dir(filepath.Dir(absolute))
	if err = privateDir(dir, false); err != nil {
		return nil, Entry{}, err
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return nil, Entry{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, Entry{}, errors.New("unsafe editor manifest")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, Entry{}, err
	}
	var m manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return nil, Entry{}, err
	}
	cfg, err := config.LoadAt(m.Vault)
	if err != nil {
		return nil, Entry{}, err
	}
	expected, err := layout(cfg, false)
	if err != nil {
		return nil, Entry{}, err
	}
	if expected != dir {
		return nil, Entry{}, errors.New("editor manifest directory does not match its vault identity")
	}
	e, err := Resolve(cfg, absolute)
	return cfg, e, err
}

// Initialize prepares an empty view without adding aliases. Publication is serialized exactly like
// Ensure; an interrupted lock is never guessed stale or automatically removed by a later process.
func Initialize(cfg *config.Config) (string, error) {
	dir, err := layout(cfg, true)
	if err != nil {
		return "", err
	}
	lock := filepath.Join(dir, ".publish-lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return "", fmt.Errorf("editor view publication is locked: %w", err)
	}
	defer os.Remove(lock)
	m, err := readManifest(cfg, dir, true)
	if err != nil {
		return "", err
	}
	if _, err = os.Lstat(filepath.Join(dir, "manifest.json")); os.IsNotExist(err) {
		if err = writeManifest(dir, m); err != nil {
			return "", err
		}
	}
	return dir, nil
}
