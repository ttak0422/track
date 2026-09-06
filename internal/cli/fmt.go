package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/mdfmt"
	"github.com/ttak0422/track/internal/track/note"
)

// markdownExts are the extensions picked up when a directory is expanded (explicit files are formatted
// regardless of extension, since the user named them).
var markdownExts = map[string]bool{".md": true, ".markdown": true}

// cmdFmt applies canonical formatting (see package mdfmt) to note files and, with --all, to the
// vault's metadata sidecars. With --all it walks the vault's note and journal directories plus the
// sidecar directory, ordering each sidecar's top-level keys canonically; otherwise it formats the
// given files and directories. With --check it writes nothing and exits non-zero when any file would
// change, which is what CI runs.
func cmdFmt(args []string) int {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	check := fs.Bool("check", false, "report files that would change and exit non-zero; do not write")
	all := fs.Bool("all", false, "format every note, journal, and sidecar metadata file in the vault")
	if code, ok := parseArgs(fs, args); !ok {
		return code
	}
	paths := fs.Args()

	switch {
	case *all && len(paths) > 0:
		return fail("pass either --all or explicit paths, not both")
	case !*all && len(paths) == 0:
		return fail("specify files to format or pass --all")
	}

	var files []string
	var err error
	if *all {
		cfg, e := config.Load()
		if e != nil {
			return fail("%v", e)
		}
		files, err = vaultFormatFiles(cfg)
	} else {
		files, err = expandMarkdownPaths(paths)
	}
	if err != nil {
		return fail("%v", err)
	}

	changed := []string{}
	for _, f := range files {
		src, e := os.ReadFile(f)
		if e != nil {
			return fail("read %s: %v", f, e)
		}
		out := formatContent(f, src)
		if out == string(src) {
			continue
		}
		changed = append(changed, f)
		if !*check {
			if e := os.WriteFile(f, []byte(out), 0o644); e != nil {
				return fail("write %s: %v", f, e)
			}
		}
	}
	sort.Strings(changed)

	code := emit(map[string]any{"checked": len(files), "changed": changed})
	if *check && len(changed) > 0 {
		return 1
	}
	return code
}

// formatContent picks the formatter for one file: markdown bodies go through mdfmt, metadata sidecar
// YAML through formatSidecar. Anything else is formatted as markdown, matching the pre-sidecar
// contract for explicitly named files.
func formatContent(path string, src []byte) string {
	if filepath.Ext(path) == ".yaml" {
		return formatSidecar(src)
	}
	return mdfmt.Format(string(src))
}

// vaultFormatFiles lists the files `fmt --all` formats: note and journal markdown files, then the
// metadata sidecars under .track/notes whose key order is normalized.
func vaultFormatFiles(cfg *config.Config) ([]string, error) {
	markdown, err := vaultMarkdownFiles(cfg)
	if err != nil {
		return nil, err
	}
	sidecars, err := sidecarFiles(cfg.MetadataDir())
	if err != nil {
		return nil, err
	}
	files := append(markdown, sidecars...)
	sort.Strings(files)
	return files, nil
}

// sidecarFiles lists the *.yaml metadata sidecars in dir, skipping a missing directory.
func sidecarFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// formatSidecar reorders a metadata sidecar's top-level keys into the canonical order (see
// note.MetadataKeyOrder), preserving every value's style, quoting, and comments verbatim through a
// yaml.Node round-trip. A document that is not a single YAML mapping, carries a duplicate top-level
// key, or has no canonical keys is returned unchanged; one already in canonical order round-trips to
// itself, so format --check stays quiet on clean sidecars.
func formatSidecar(src []byte) string {
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(src))
	if err := dec.Decode(&doc); err != nil {
		return string(src)
	}
	// Refuse a second document: reordering must not silently drop anything past the first "---".
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return string(src)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return string(src)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || len(root.Content)%2 != 0 {
		return string(src)
	}

	// Refuse duplicates: reordering would silently drop an earlier repeated key.
	seen := make(map[string]bool, len(note.MetadataKeyOrder))
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k.Kind == yaml.ScalarNode && seen[k.Value] {
			return string(src)
		}
		seen[k.Value] = true
	}

	// Bucket canonical keys by their target rank; unknown keys keep their relative order after them.
	rank := make(map[string]int, len(note.MetadataKeyOrder))
	for i, k := range note.MetadataKeyOrder {
		rank[k] = i
	}
	type entry struct{ key, value *yaml.Node }
	known := make(map[int]entry, len(note.MetadataKeyOrder))
	var rest []*yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Kind == yaml.ScalarNode {
			if r, ok := rank[k.Value]; ok {
				known[r] = entry{k, v}
				continue
			}
		}
		rest = append(rest, k, v)
	}
	if len(known) == 0 {
		return string(src)
	}

	reordered := make([]*yaml.Node, 0, len(root.Content))
	for i := range note.MetadataKeyOrder {
		if e, ok := known[i]; ok {
			reordered = append(reordered, e.key, e.value)
		}
	}
	reordered = append(reordered, rest...)
	root.Content = reordered
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return string(src)
	}
	return string(out)
}

// vaultMarkdownFiles lists the markdown note and journal files under the vault.
func vaultMarkdownFiles(cfg *config.Config) ([]string, error) {
	var files []string
	for _, dir := range []string{cfg.NoteDir(), cfg.JournalDir()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if containsExt(cfg.Extensions, filepath.Ext(e.Name())) {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

// expandMarkdownPaths turns explicit paths into a file list: files are taken as-is, directories are
// walked for markdown files.
func expandMarkdownPaths(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && markdownExts[filepath.Ext(path)] {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// containsExt reports whether ext is one of the configured note extensions.
func containsExt(exts []string, ext string) bool {
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}
