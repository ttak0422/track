// Package backup writes a lossless archive of canonical vault files, never the index.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const FormatVersion = 1

type Vault struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	// Source is deliberately omitted from the portable manifest.
	Source string `json:"-"`
}

type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type Manifest struct {
	Format            string    `json:"format"`
	Version           int       `json:"version"`
	Complete          bool      `json:"complete"`
	CompletenessScope string    `json:"completeness_scope"`
	Consistency       string    `json:"consistency"`
	StartedAt         time.Time `json:"started_at"`
	FinishedAt        time.Time `json:"finished_at"`
	Vaults            []Vault   `json:"vaults"`
	Exclusions        []string  `json:"exclusions"`
	Entries           []Entry   `json:"entries"`
}

// Write creates stage exclusively. Its caller publishes it only after Write succeeds.
// Run this in a disposable process: cloud filesystem reads may ignore cancellation.
func Write(vaults []Vault, stage string, protectedFiles ...string) (*Manifest, error) {
	output, err := filepath.Abs(stage)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return WriteAt(vaults, root, filepath.Base(output), protectedFiles...)
}

// WriteAt anchors staging and cleanup to a directory already validated by the CLI.
func WriteAt(vaults []Vault, destination *os.Root, stage string, protectedFiles ...string) (*Manifest, error) {
	if !fs.ValidPath(stage) || strings.ContainsAny(stage, "/\\") || stage == "." {
		return nil, fmt.Errorf("stage must be a basename")
	}

	if len(vaults) == 0 {
		return nil, fmt.Errorf("no vaults to back up")
	}
	var protected []fs.FileInfo
	for _, name := range protectedFiles {
		info, err := os.Stat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot check protected machine configuration")
		}
		protected = append(protected, info)
	}
	m := &Manifest{
		Format: "track-backup", Version: FormatVersion,
		Consistency:       "checked-file-copy; not an atomic filesystem snapshot",
		CompletenessScope: "all supported canonical files observed locally; remote sync state and preexisting loss are not verified",
		StartedAt:         time.Now().UTC(), Vaults: vaults, Entries: []Entry{},
		Exclusions: []string{
			"configured machine file and hard links; environment overrides (no content redaction)",
			"external SQLite indexes and caches",
			"root .git and regular .DS_Store files",
		},
	}
	type source struct {
		root  *os.Root
		files map[string]fs.FileInfo
	}
	sources := make([]source, 0, len(vaults))
	defer func() {
		for _, s := range sources {
			s.root.Close()
		}
	}()
	prefixes := map[string]bool{}
	for _, v := range vaults {
		if !fs.ValidPath(v.Prefix) || strings.Contains(v.Prefix, "\\") || v.Prefix == "." || strings.Split(v.Prefix, "/")[0] == "manifest.json" || prefixes[v.Prefix] {
			return nil, fmt.Errorf("invalid or duplicate vault prefix")
		}
		for prefix := range prefixes {
			if strings.HasPrefix(v.Prefix, prefix+"/") || strings.HasPrefix(prefix, v.Prefix+"/") {
				return nil, fmt.Errorf("overlapping vault prefixes")
			}
		}
		prefixes[v.Prefix] = true
		canonical, err := filepath.EvalSymlinks(v.Source)
		if err != nil {
			return nil, fmt.Errorf("vault unavailable: %w", err)
		}
		parent, err := filepath.EvalSymlinks(destination.Name())
		if err != nil {
			return nil, err
		}
		if within(canonical, filepath.Join(parent, stage)) {
			return nil, fmt.Errorf("backup destination must be outside every source vault")
		}
		sourceInfo, err := os.Lstat(canonical)
		if err != nil {
			return nil, err
		}
		if isPlaceholder(sourceInfo) {
			return nil, fmt.Errorf("vault root is a cloud placeholder; finish syncing first")
		}
		root, err := os.OpenRoot(canonical)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source{root: root})
		for _, required := range []string{"note", "journal", "template", "assets", "data", ".track", ".track/notes"} {
			info, err := root.Lstat(required)
			if err != nil {
				return nil, fmt.Errorf("vault incomplete: %s: %w", required, err)
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isPlaceholder(info) {
				return nil, fmt.Errorf("vault %s must be a real directory", required)
			}
		}
		files, err := inventory(root, protected...)
		if err != nil {
			return nil, err
		}
		sources[len(sources)-1].files = files
	}
	out, err := destination.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		out.Close()
		if !success {
			destination.Remove(stage)
		}
	}()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for i, v := range vaults {
		s := sources[i]
		paths := make([]string, 0, len(s.files))
		for name := range s.files {
			paths = append(paths, name)
		}
		slices.Sort(paths)
		if err := tw.WriteHeader(&tar.Header{Name: v.Prefix + "/", Typeflag: tar.TypeDir, Mode: 0o700}); err != nil {
			return nil, err
		}
		for _, name := range paths {
			info := s.files[name]
			entry := Entry{Path: path.Join(v.Prefix, name), Kind: "file", Size: info.Size()}
			header := &tar.Header{Name: entry.Path, Mode: int64(0o600 | info.Mode().Perm()&0o100), Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
			if info.IsDir() {
				header.Typeflag, header.Mode, header.Size, header.Name = tar.TypeDir, 0o700, 0, header.Name+"/"
				entry.Kind, entry.Size = "directory", 0
				if err := tw.WriteHeader(header); err != nil {
					return nil, err
				}
			} else {
				sum, err := copyFile(s.root, name, info, tw, header)
				if err != nil {
					return nil, fmt.Errorf("backup %s: %w", name, err)
				}
				entry.SHA256 = sum
			}
			m.Entries = append(m.Entries, entry)
		}
	}
	// A second complete inventory detects additions, removals, replacements and edits,
	// including writes to a file copied earlier while a later file was being read.
	for _, s := range sources {
		after, err := inventory(s.root, protected...)
		if err != nil {
			return nil, err
		}
		if len(after) != len(s.files) {
			return nil, fmt.Errorf("vault changed during backup; stop editing/syncing and retry")
		}
		for name, before := range s.files {
			if !same(before, after[name]) {
				return nil, fmt.Errorf("vault changed during backup: %s", name)
			}
		}
	}
	m.Complete = true
	m.FinishedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(raw); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if err := out.Sync(); err != nil {
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	success = true
	return m, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Only canonical layout is accepted. Unknown top-level data fails closed instead
// of accidentally archiving a personal directory or silently omitting future state.
func inventory(root *os.Root, protected ...fs.FileInfo) (map[string]fs.FileInfo, error) {
	files := map[string]fs.FileInfo{}
	allowed := map[string]bool{"note": true, "journal": true, "template": true, "assets": true, "data": true, "calendar": true, "apps": true, ".track": true}
	err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if name == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path.Base(name) == ".DS_Store" {
			info, err := root.Lstat(name)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unexpected non-file Finder metadata entry: %s", name)
			}
			return nil
		}
		if !strings.Contains(name, "/") && !allowed[name] {
			return fmt.Errorf("unrecognized vault root entry %s; backup refused rather than omitting it", name)
		}
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("unsafe archive path: %s", name)
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported: %s", name)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file or directory: %s", name)
		}
		for _, secret := range protected {
			if os.SameFile(info, secret) {
				return fmt.Errorf("machine configuration is stored inside the vault (or hard-linked there); backup refused to protect credentials")
			}
		}
		if isPlaceholder(info) || (strings.HasPrefix(path.Base(name), ".") && strings.HasSuffix(name, ".icloud")) {
			return fmt.Errorf("cloud placeholder is not locally available: %s; finish syncing first", name)
		}
		files[name] = info
		return nil
	})
	return files, err
}

func same(a, b fs.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime() == b.ModTime() && changeTime(a) == changeTime(b)
}

func copyFile(root *os.Root, name string, before fs.FileInfo, tw *tar.Writer, header *tar.Header) (string, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !same(before, info) || !info.Mode().IsRegular() || isPlaceholder(info) {
		return "", fmt.Errorf("file changed or is unavailable")
	}
	if err := tw.WriteHeader(header); err != nil {
		return "", err
	}
	sum := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(tw, sum), file, before.Size()); err != nil {
		return "", err
	}
	var extra [1]byte
	n, err := file.Read(extra[:])
	if n != 0 || err != io.EOF {
		return "", fmt.Errorf("file changed or read failed: %v", err)
	}
	after, err := file.Stat()
	if err != nil {
		return "", err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if !same(before, after) || !same(before, current) {
		return "", fmt.Errorf("file changed during read")
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
