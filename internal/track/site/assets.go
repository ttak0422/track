package site

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
)

// assetRef matches an "assets/<path>" reference as written in note bodies (the form printed by
// `track asset import` and used in Markdown image/links). The capture is the path under assets/,
// stopping at whitespace or characters that close a Markdown link, HTML attribute, or anchor.
var assetRef = regexp.MustCompile(`assets/([^\s"')>?#]+)`)

// collectAssets returns the distinct "assets/<path>" file references found in a note body.
func collectAssets(body string) []string {
	seen := map[string]bool{}
	for _, m := range assetRef.FindAllStringSubmatch(body, -1) {
		rel := strings.TrimSpace(m[1])
		if rel == "" || strings.Contains(rel, "..") {
			continue
		}
		seen[rel] = true
	}
	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// copyAssets copies each referenced asset from srcDir into <outDir>/assets, preserving the relative
// path so the "assets/<path>" references in the generated HTML resolve. Missing source files are
// skipped and reported, so a broken reference does not fail the whole build.
func copyAssets(srcDir, outDir string, rels []string) (copied, missing []string, err error) {
	for _, rel := range rels {
		src := filepath.Join(srcDir, filepath.FromSlash(rel))
		info, statErr := os.Stat(src)
		if statErr != nil || info.IsDir() {
			missing = append(missing, rel)
			continue
		}
		dst := filepath.Join(outDir, config.AssetsDirName, filepath.FromSlash(rel))
		if err = copyFile(src, dst); err != nil {
			return copied, missing, err
		}
		copied = append(copied, rel)
	}
	return copied, missing, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
