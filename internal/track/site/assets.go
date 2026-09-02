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

// assetNamer resolves an "assets/<rel>" reference to the name it publishes under. The name comes from the
// file's contents (see publishAssetName), so it hides the source file name and moves when the file
// changes. Names are cached for the build: one asset is usually referenced by several notes, and the body
// rewrite, the cover metadata, and the copy all have to arrive at the same name for it.
type assetNamer struct {
	cache map[string]string
}

func newAssetNamer() *assetNamer { return &assetNamer{cache: map[string]string{}} }

func (n *assetNamer) name(srcDir, rel string) string {
	key := srcDir + "\x00" + rel
	if name, ok := n.cache[key]; ok {
		return name
	}
	name := publishAssetName(rel, readAsset(srcDir, rel))
	n.cache[key] = name
	return name
}

// readAsset reads one asset's bytes, or nil when it cannot be read — including the traversal cases
// copyAssets rejects, which must never be opened just to name them.
func readAsset(srcDir, rel string) []byte {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return nil
	}
	content, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	return content
}

// rewriteAssetRefs rewrites every "assets/<rel>" reference in a note body to its published name
// (assets/<slug><ext>), matching how copyAssets names the copied files, so the timestamp/source file
// name never appears in the published HTML. References with "" or ".." are left untouched, as
// collectAssets skips them too. References written inside a fenced code block or inline code span are
// literal documentation examples, not real attachments, so they are left exactly as written.
func rewriteAssetRefs(body, srcDir string, names *assetNamer) string {
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
			b.WriteString("assets/" + names.name(srcDir, rel))
		}
		last = end
	}
	b.WriteString(body[last:])
	return b.String()
}

// CollectAssets returns the distinct "assets/<path>" file references found in a note body, ignoring any
// written inside a code block or inline code span (those are examples, not attachments to publish).
func CollectAssets(body string) []string {
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
// skipped and reported, so a broken reference does not fail the whole build. noteSlug maps note
// provenance inside spec assets, exactly as resolveViewSpecBlocks does for fenced charts.
func copyAssets(srcDir, outDir string, rels []string, noteSlug func(string) (string, bool), key []byte, names *assetNamer) (copied, missing []string, err error) {
	for _, rel := range rels {
		// The choke point rejects traversal for every rel source (collectAssets applies the same
		// rule, but a cover image from a hand-edited sidecar arrives unfiltered): an absolute or
		// ".." rel would read — and publish — a file outside the vault's assets tree.
		if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			missing = append(missing, rel)
			continue
		}
		src := filepath.Join(srcDir, filepath.FromSlash(rel))
		info, statErr := os.Stat(src)
		if statErr != nil || info.IsDir() {
			missing = append(missing, rel)
			continue
		}
		dst := filepath.Join(outDir, config.AssetsDirName, names.name(srcDir, rel))
		if isSpecAsset(rel) {
			if err = renderSpecAsset(src, dst, noteSlug, key); err != nil {
				return copied, missing, err
			}
		} else if err = copyFile(src, dst); err != nil {
			return copied, missing, err
		}
		copied = append(copied, rel)
	}
	return copied, missing, nil
}

// renderSpecAsset reads a View Spec asset (inline-data chart) and writes its resolved ECharts option to
// dst; the frontend fetches it and draws an interactive chart with its bundled ECharts. A malformed spec
// fails the build loudly rather than being silently skipped, so a broken chart is not published as a dead
// reference. Note provenance is slug-rewritten like the fence path, so a spec asset never publishes an
// internal note id.
//
// The option is a chart's data in machine shape — exactly what the data bundle is locked for (ADR 0069) —
// so it is published locked, as "<slug>.echarts.bin". The reference in the body keeps saying
// ".echarts.json": it names what the file holds, which is how the frontend knows to draw a chart with it
// (web/src/api.ts fetchAssetText swaps the extension the same way).
func renderSpecAsset(src, dst string, noteSlug func(string) (string, bool), key []byte) error {
	specJSON, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	opt, err := render.EChartsOptionFromSpecDir(specJSON, "")
	if err != nil {
		return fmt.Errorf("render spec asset %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	locked, err := lock(key, []byte(rewriteNoteRefs(opt, noteSlug)))
	if err != nil {
		return err
	}
	return os.WriteFile(strings.TrimSuffix(dst, ".json")+".bin", locked, 0o644)
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
