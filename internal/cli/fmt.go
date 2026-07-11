package cli

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/mdfmt"
)

// markdownExts are the extensions picked up when a directory is expanded (explicit files are formatted
// regardless of extension, since the user named them).
var markdownExts = map[string]bool{".md": true, ".markdown": true}

// cmdFmt applies canonical Markdown formatting (see package mdfmt) to note files. With --all it walks
// the vault's note and journal directories; otherwise it formats the given files and directories. With
// --check it writes nothing and exits non-zero when any file would change, which is what CI runs.
func cmdFmt(args []string) int {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	check := fs.Bool("check", false, "report files that would change and exit non-zero; do not write")
	all := fs.Bool("all", false, "format every note and journal file in the vault")
	if err := fs.Parse(args); err != nil {
		return fail("parse args: %v", err)
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
		files, err = vaultMarkdownFiles(cfg)
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
		out := mdfmt.Format(string(src))
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
