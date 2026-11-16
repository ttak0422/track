package config

// Read-only path/config observation. Resolution itself performs no store,
// index, lease, listener or scaffold operation. The internal bound-startup
// experiment has no public CLI or native admission caller. This is not a signed capability,
// availability model, note/link audit, readiness proof or filesystem sandbox.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const manifestPeers = 32
const manifestConfigBytes = 1 << 20
const manifestTotalBytes = 8 << 20
const manifestPathBytes = 4096

var ErrLaunchManifestChanged = errors.New("launch manifest inputs changed")

type LaunchSelector struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type LaunchContext struct {
	Home                string `json:"home"`
	Cwd                 string `json:"cwd"`
	XDGCacheHome        string `json:"xdg_cache_home"`
	XDGConfigHome       string `json:"xdg_config_home"`
	EnvironmentRevision string `json:"environment_revision"`
}
type LaunchVault struct {
	Name               string `json:"name"`
	DisplayPath        string `json:"display_path"`
	CanonicalPath      string `json:"canonical_path"`
	RootIdentity       string `json:"root_identity"`
	OrdinaryDB         string `json:"ordinary_db"`
	ResolvedOrdinaryDB string `json:"resolved_ordinary_db"`
	OrdinaryAnchor     string `json:"ordinary_anchor"`
	OrdinaryIdentity   string `json:"ordinary_identity"`
	PrivateDB          string `json:"private_db"`
}
type ConfigProvenance struct {
	Path          string `json:"path"`
	CanonicalPath string `json:"canonical_path"`
	Present       bool   `json:"present"`
	Identity      string `json:"identity"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode"`
	ModifiedNS    int64  `json:"modified_ns"`
	SHA256        string `json:"sha256"`
}
type ReadOnlyLaunchManifest struct {
	Version         int                `json:"version"`
	Selector        LaunchSelector     `json:"selector"`
	PrivateRoot     string             `json:"private_root"`
	PrivateMode     uint32             `json:"private_mode"`
	PrivateIdentity string             `json:"private_identity"`
	Context         LaunchContext      `json:"context"`
	Active          LaunchVault        `json:"active"`
	Peers           []LaunchVault      `json:"peers"`
	Configurations  []ConfigProvenance `json:"configurations"`
	Revision        string             `json:"revision"`
}

func manifestDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func manifestContext() (LaunchContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return LaunchContext{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return LaunchContext{}, err
	}
	values := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "TRACK_") || key == "HOME" || strings.HasPrefix(key, "XDG_") {
			values = append(values, entry)
		}
	}
	sort.Strings(values)
	if len(values) > 256 || len(strings.Join(values, "\n")) > 64<<10 {
		return LaunchContext{}, errors.New("resolution environment exceeds manifest bounds")
	}
	c := LaunchContext{Home: home, Cwd: cwd, XDGCacheHome: os.Getenv("XDG_CACHE_HOME"), XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"), EnvironmentRevision: manifestDigest(values)}
	for _, path := range []string{c.Home, c.Cwd, c.XDGCacheHome, c.XDGConfigHome} {
		if len(path) > manifestPathBytes {
			return LaunchContext{}, errors.New("resolution context path exceeds bound")
		}
	}
	return c, nil
}

// Capture a bounded regular configuration file, or explicit absent state. Config
// symlinks are resolved as by Go's normal loader; hardlinks/special files are
// conservatively refused. Preflight and reread are not hostile-race protection.
func manifestConfig(path string) (ConfigProvenance, []byte, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return ConfigProvenance{}, nil, err
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return ConfigProvenance{}, nil, err
	}
	p := ConfigProvenance{Path: path, CanonicalPath: canonical}
	if len(path) > manifestPathBytes || len(canonical) > manifestPathBytes {
		return p, nil, errors.New("config path exceeds bound")
	}
	info, err := os.Stat(canonical)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil, nil
	}
	if err != nil {
		return p, nil, err
	}
	if !info.Mode().IsRegular() || !cacheSingleLink(info) || info.Size() > manifestConfigBytes {
		return p, nil, errors.New("config must be bounded single-link regular file")
	}
	identity, err := manifestFileIdentity(info)
	if err != nil {
		return p, nil, err
	}
	file, err := os.Open(canonical)
	if err != nil {
		return p, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return p, nil, ErrLaunchManifestChanged
	}
	raw, err := io.ReadAll(io.LimitReader(file, manifestConfigBytes+1))
	if err != nil {
		return p, nil, err
	}
	if len(raw) > manifestConfigBytes {
		return p, nil, errors.New("config exceeds byte bound")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return p, nil, ErrLaunchManifestChanged
	}
	sum := sha256.Sum256(raw)
	p.Present = true
	p.Identity = identity
	p.Size = info.Size()
	p.Mode = uint32(info.Mode())
	p.ModifiedNS = info.ModTime().UnixNano()
	p.SHA256 = hex.EncodeToString(sum[:])
	return p, raw, nil
}
func manifestRoot(path string) (string, string, error) {
	canonical, err := canonicalPath(path)
	if err != nil {
		return "", "", err
	}
	if len(path) > manifestPathBytes || len(canonical) > manifestPathBytes {
		return "", "", errors.New("vault path exceeds bound")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", errors.New("manifest requires existing vault directories")
	}
	identity, err := manifestFileIdentity(info)
	return canonical, identity, err
}

// Preserve the canonical name map captured by resolveVaults. A later alias
// must not silently reinterpret a declared name while configs are being read.
func manifestDeclaredRoot(name, display string, original map[string]string, declared bool) (string, string, error) {
	path, identity, err := manifestRoot(display)
	if err != nil {
		return "", "", err
	}
	if declared && original[path] != name {
		return "", "", ErrLaunchManifestChanged
	}
	return path, identity, nil
}

func manifestCacheLocation(path string) (string, string, string, error) {
	if err := absoluteClean(path); err != nil {
		return "", "", "", err
	}
	resolved, err := resolveCacheAlias(path)
	if err != nil {
		return "", "", "", err
	}
	anchor := resolved
	for {
		info, e := os.Stat(anchor)
		if e == nil {
			id, e := manifestFileIdentity(info)
			return resolved, anchor, id, e
		}
		if !errors.Is(e, os.ErrNotExist) {
			return "", "", "", e
		}
		parent := filepath.Dir(anchor)
		if parent == anchor {
			return "", "", "", e
		}
		anchor = parent
	}
}

// ResolveReadOnlyLaunchManifest uses the current process's actual Go resolution
// context, without modifying it. Call only in an explicitly controlled context.
// It fails closed unless every declared root/config and private-cache protection
// can be verified. Cancellation is checked between synchronous filesystem calls;
// it is not a hard wall-clock deadline. Returned data is copied observation, not
// authorization. All inputs must be revalidated again before any future writes.
func ResolveReadOnlyLaunchManifest(ctx context.Context, selector LaunchSelector, privateRoot string) (ReadOnlyLaunchManifest, error) {
	var zero ReadOnlyLaunchManifest
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if (selector.Name != "" && selector.Path != "") || len(selector.Name) > 256 || len(selector.Path) > manifestPathBytes {
		return zero, errors.New("invalid launch selector")
	}
	if selector.Name != "" && !vaultNamePattern.MatchString(selector.Name) {
		return zero, errors.New("invalid registered selector")
	}
	privateCanonical, privateIdentity, err := manifestRoot(privateRoot)
	if err != nil {
		return zero, err
	}
	if privateCanonical != privateRoot {
		return zero, errors.New("private root must be canonical")
	}
	privateInfo, err := os.Stat(privateRoot)
	if err != nil {
		return zero, err
	}
	before, err := manifestContext()
	if err != nil {
		return zero, err
	}
	machine, raw, err := manifestConfig(ConfigPath())
	if err != nil {
		return zero, err
	}
	var mc machineFileConfig
	if err = strictDecodeBytes(machine.Path, raw, &mc); err != nil {
		return zero, err
	}
	entries := vaultEntries(mc)
	if len(entries) > manifestPeers {
		return zero, errors.New("registry exceeds manifest bound")
	}
	for name, path := range entries {
		if len(name) > 256 || len(path) > manifestPathBytes {
			return zero, errors.New("registry field exceeds manifest bound")
		}
	}
	registry, names, err := resolveVaults(entries)
	if err != nil {
		return zero, err
	}
	fixed := selector.Path
	if selector.Name != "" {
		var ok bool
		fixed, ok = registry[selector.Name]
		if !ok {
			return zero, errors.New("unknown registered selector")
		}
	}
	display, err := vaultLocation(mc, registry, fixed)
	if err != nil {
		return zero, err
	}
	activePath, activeIdentity, err := manifestRoot(display)
	if err != nil {
		return zero, err
	}
	cache, err := ordinaryCacheDirectory(mc)
	if err != nil {
		return zero, err
	}
	m := ReadOnlyLaunchManifest{Version: 1, Selector: selector, PrivateRoot: privateRoot, PrivateIdentity: privateIdentity, PrivateMode: uint32(privateInfo.Mode()), Context: before, Configurations: []ConfigProvenance{machine}, Peers: []LaunchVault{}}
	displays := map[string]string{}
	for name, path := range registry {
		displays[name] = path
	}
	activeName := names[activePath]
	if _, declared := displays[activeName]; !declared {
		displays[activeName] = display
	}
	total := len(raw)
	for _, name := range sortedNames(displays) {
		if err = ctx.Err(); err != nil {
			return zero, err
		}
		_, declared := registry[name]
		path, identity, e := manifestDeclaredRoot(name, displays[name], names, declared)
		if e != nil {
			return zero, e
		}
		provenance, bytes, e := manifestConfig(VaultConfigPath(path))
		if e != nil {
			return zero, e
		}
		total += len(bytes)
		if total > manifestTotalBytes {
			return zero, errors.New("configuration aggregate exceeds manifest bound")
		}
		var vc vaultFileConfig
		if e = strictDecodeBytes(provenance.Path, bytes, &vc); e != nil {
			return zero, e
		}
		if e = validateVaultConfig(vc); e != nil {
			return zero, e
		}
		if e = validateProperties(vc.Properties); e != nil {
			return zero, e
		}
		v := LaunchVault{Name: name, DisplayPath: displays[name], CanonicalPath: path, RootIdentity: identity, OrdinaryDB: filepath.Join(expandHome(cache), vaultCacheKey(path), indexDBName())}
		if len(v.OrdinaryDB) > manifestPathBytes {
			return zero, errors.New("ordinary DB path exceeds bound")
		}
		v.ResolvedOrdinaryDB, v.OrdinaryAnchor, v.OrdinaryIdentity, e = manifestCacheLocation(v.OrdinaryDB)
		if e != nil {
			return zero, e
		}
		m.Configurations = append(m.Configurations, provenance)
		m.Peers = append(m.Peers, v)
	}
	files := make([]string, 0, len(m.Configurations))
	for _, p := range m.Configurations {
		files = append(files, p.Path)
	}
	for i := range m.Peers {
		if err = ctx.Err(); err != nil {
			return zero, err
		}
		v := &m.Peers[i]
		cfg := &Config{VaultDir: v.CanonicalPath, DBPath: v.OrdinaryDB, Vaults: registry}
		v.PrivateDB, err = ReadOnlyCachePath(privateRoot, cfg, files)
		if err != nil {
			return zero, err
		}
		if v.Name == activeName {
			if v.CanonicalPath != activePath || v.RootIdentity != activeIdentity {
				return zero, ErrLaunchManifestChanged
			}
			m.Active = *v
			m.Active.DisplayPath = display
		}
	}
	if err = verifyManifestInputs(ctx, m); err != nil {
		return zero, err
	}
	bytes, err := json.Marshal(m)
	if err != nil {
		return zero, err
	}
	if len(bytes) > 1<<20 {
		return zero, errors.New("manifest output exceeds byte bound")
	}
	m.Revision = manifestDigest(m)
	return m, nil
}

// Shared final verifier also permits deterministic capture/mutate/verify tests.
func verifyManifestInputs(ctx context.Context, m ReadOnlyLaunchManifest) error {
	var err error
	registry := map[string]string{}
	for _, v := range m.Peers {
		registry[v.Name] = v.DisplayPath
	}
	files := make([]string, 0, len(m.Configurations))
	for _, p := range m.Configurations {
		files = append(files, p.Path)
	}
	// Revalidate the complete captured set, not only the last config read.
	for _, p := range m.Configurations {
		if err = ctx.Err(); err != nil {
			return err
		}
		current, _, e := manifestConfig(p.Path)
		if e != nil {
			return e
		}
		if current != p {
			return ErrLaunchManifestChanged
		}
	}
	for _, v := range m.Peers {
		path, id, e := manifestRoot(v.DisplayPath)
		if e != nil {
			return e
		}
		if path != v.CanonicalPath || id != v.RootIdentity {
			return ErrLaunchManifestChanged
		}
	}
	selectedPath, selectedIdentity, err := manifestRoot(m.Active.DisplayPath)
	if err != nil {
		return err
	}
	if selectedPath != m.Active.CanonicalPath || selectedIdentity != m.Active.RootIdentity {
		return ErrLaunchManifestChanged
	}
	// Recheck private separation and cache aliases after all captured input reads.
	for _, v := range m.Peers {
		if err = ctx.Err(); err != nil {
			return err
		}
		resolved, anchor, identity, e := manifestCacheLocation(v.OrdinaryDB)
		if e != nil {
			return e
		}
		if resolved != v.ResolvedOrdinaryDB || anchor != v.OrdinaryAnchor || identity != v.OrdinaryIdentity {
			return ErrLaunchManifestChanged
		}
		cfg := &Config{VaultDir: v.CanonicalPath, DBPath: v.OrdinaryDB, Vaults: registry}
		path, e := ReadOnlyCachePath(m.PrivateRoot, cfg, files)
		if e != nil {
			return e
		}
		if path != v.PrivateDB {
			return ErrLaunchManifestChanged
		}
	}
	currentPrivate, currentIdentity, err := manifestRoot(m.PrivateRoot)
	if err != nil {
		return err
	}
	currentInfo, err := os.Stat(m.PrivateRoot)
	if err != nil {
		return err
	}
	if currentPrivate != m.PrivateRoot || currentIdentity != m.PrivateIdentity || uint32(currentInfo.Mode()) != m.PrivateMode {
		return ErrLaunchManifestChanged
	}
	after, err := manifestContext()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(m.Context, after) {
		return ErrLaunchManifestChanged
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Verify is comparison against a caller-supplied observation, not validation of
// an unforgeable capability. It performs a fresh complete resolution each time.
func VerifyReadOnlyLaunchManifest(ctx context.Context, selector LaunchSelector, privateRoot, expectedRevision string) (ReadOnlyLaunchManifest, error) {
	if len(expectedRevision) != 64 {
		return ReadOnlyLaunchManifest{}, ErrLaunchManifestChanged
	}
	m, err := ResolveReadOnlyLaunchManifest(ctx, selector, privateRoot)
	if err != nil {
		return ReadOnlyLaunchManifest{}, err
	}
	if m.Revision != expectedRevision {
		return ReadOnlyLaunchManifest{}, ErrLaunchManifestChanged
	}
	return m, nil
}
