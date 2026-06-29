package site

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/render"
)

// assetRef matches an "assets/<path>" reference as written in note bodies (the form printed by
// `track asset import` and used in Markdown image/links). The capture is the path under assets/,
// stopping at whitespace or characters that close a Markdown link, HTML attribute, or anchor.
var assetRef = regexp.MustCompile(`assets/([^\s"')>?#]+)`)

// rewriteAssetRefs rewrites every "assets/<rel>" reference in a note body to its published name
// (assets/<slug><ext>), matching how copyAssets names the copied files, so the timestamp/source file
// name never appears in the published HTML. References with "" or ".." are left untouched, as
// collectAssets skips them too. References written inside a fenced code block or inline code span are
// literal documentation examples, not real attachments, so they are left exactly as written.
func rewriteAssetRefs(body string) string {
	masked := maskCode(body)
	var b strings.Builder
	last := 0
	for _, loc := range assetRef.FindAllStringIndex(masked, -1) {
		start, end := loc[0], loc[1]
		match := body[start:end]
		rel := strings.TrimSpace(strings.TrimPrefix(match, "assets/"))
		b.WriteString(body[last:start])
		if rel == "" || strings.Contains(rel, "..") {
			b.WriteString(match)
		} else {
			b.WriteString("assets/" + publishAssetName(rel))
		}
		last = end
	}
	b.WriteString(body[last:])
	return b.String()
}

// collectAssets returns the distinct "assets/<path>" file references found in a note body, ignoring any
// written inside a code block or inline code span (those are examples, not attachments to publish).
func collectAssets(body string) []string {
	masked := maskCode(body)
	seen := map[string]bool{}
	for _, loc := range assetRef.FindAllStringSubmatchIndex(masked, -1) {
		rel := strings.TrimSpace(body[loc[2]:loc[3]])
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
		dst := filepath.Join(outDir, config.AssetsDirName, publishAssetName(rel))
		if isSpecAsset(rel) {
			if err = renderSpecAsset(src, dst); err != nil {
				return copied, missing, err
			}
		} else if err = copyFile(src, dst); err != nil {
			return copied, missing, err
		}
		copied = append(copied, rel)
	}
	return copied, missing, nil
}

// renderSpecAsset reads a View Spec asset (inline-data chart) and writes its rendered SVG to dst. A
// malformed spec fails the build loudly rather than being silently skipped, so a broken chart is not
// published as a dead reference.
func renderSpecAsset(src, dst string) error {
	specJSON, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	svg, err := render.SVGFromSpec(specJSON)
	if err != nil {
		return fmt.Errorf("render spec asset %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(svg), 0o644)
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
