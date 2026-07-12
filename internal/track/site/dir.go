package site

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/export"
	"github.com/ttak0422/track/internal/track/link"
	"github.com/ttak0422/track/internal/track/note"
)

// BuildDir publishes a directory of plain Markdown files (e.g. repo-mounted help/docs that live outside
// any vault). Each file becomes a published note slugged by its base name; ids are assigned in name
// order. Wiki links resolve by file base name or by the file's first H1 title among the directory's
// files. An "assets" subdirectory supplies referenced media. frontendDir is the static-mode frontend
// build.
//
// There is no vault and no sidecar here, and the machine's ambient user config is never read, so a
// page's metadata — its properties, its tags, its icon — comes only from its own inline "key:: value"
// fields. The site's own settings (its entry page, its icon maps) come from an optional
// "<srcDir>/site.yml" that travels with the content (see siteConfig); with no such file this is a plain,
// config-free directory export whose entry page is the "index" convention.
func BuildDir(srcDir, baseURL, frontendDir, outDir string) (Result, error) {
	sc, err := loadSiteConfig(srcDir)
	if err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return Result{}, fmt.Errorf("read src dir: %w", err)
	}

	type file struct {
		slug  string
		title string
		body  string // raw
	}
	var files []file
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		slug := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		title := firstHeading(string(raw))
		if title == "" {
			title = slug
		}
		files = append(files, file{slug: slug, title: title, body: string(raw)})
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("no .md files found in %s", srcDir)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].slug < files[j].slug })

	// Assign ids in name order and build the key -> id map (base name and title both resolve).
	keyToID := map[string]int64{}
	idForSlug := map[string]int64{}
	for i, f := range files {
		id := int64(i + 1)
		idForSlug[f.slug] = id
		keyToID[f.slug] = id
		keyToID[f.title] = id
	}

	// The entry page, named the same way a wiki link is: by file base name or by page title. The site's
	// own "home" names it; with no config, the "index" convention does. Nothing found is a loud error —
	// a site whose landing page silently moved is worse than one that fails to build. File base names
	// are matched first, and never through the merged link map: titles and base names share that
	// namespace, so a page whose H1 happens to spell another page's file name would otherwise steal the
	// front door — the same silent move, dressed as a link.
	entry := sc.Home
	if entry == "" {
		entry = "index"
	}
	root, ok := idForSlug[entry]
	if !ok {
		root, ok = idForSlug[strings.TrimSuffix(entry, filepath.Ext(entry))]
	}
	if !ok {
		root, ok = keyToID[entry] // no file is named that: a page title, then.
	}
	if !ok {
		return Result{}, fmt.Errorf("entry page %q not found among .md files in %s", entry, srcDir)
	}

	assetSrc := filepath.Join(srcDir, "assets")
	var docs []doc
	var edges []edge
	seenEdge := map[edge]bool{}
	for _, f := range files {
		id := idForSlug[f.slug]
		body, err := export.WebBody(f.body)
		if err != nil {
			return Result{}, fmt.Errorf("render %s: %w", f.slug, err)
		}
		// Plain Markdown files have no sidecar, so inline "key:: value" fields are their only properties —
		// and their only tags.
		props := note.InlineFields(f.body)
		tags := inlineTags(props)
		docs = append(docs, doc{
			id:       id,
			title:    f.title,
			kind:     "note",
			path:     f.slug + ".md",
			body:     body,
			tags:     tags,
			keys:     []string{f.slug, f.title},
			assets:   collectAssets(f.body),
			assetSrc: assetSrc,
			// One icon resolver for every surface: the page's own "icon::" override, then the site's
			// tag map against its tags, then its kind map (a directory page is always kind "note").
			icon: sc.icons().NoteIcon("note", tags, inlineIcon(props)),
			// A docs directory may keep canonical JSONL next to its assets, mirroring the vault's data/.
			dataDir: filepath.Join(srcDir, "data"),
			props:   props,
		})
		for _, ref := range link.Refs(f.body) {
			if dst, ok := keyToID[ref.Text]; ok {
				e := edge{src: id, dst: dst}
				if !seenEdge[e] {
					seenEdge[e] = true
					edges = append(edges, e)
				}
			}
		}
	}

	// Plain Markdown files carry no activity days, so a calendar would be permanently empty; directory
	// sites never include it (the CLI rejects --calendar with --src).
	return writeBundle(docs, edges, root, false, baseURL, frontendDir, outDir)
}

// siteConfigNames are the per-site config file, discovered inside the published directory itself. Both
// spellings are accepted: ".yml" and ".yaml" are a coin flip (this repo writes config.yml but also
// renames.yaml, gen.yaml and note sidecars), and a config that is only exercised at publish time must not
// be skipped over a filename typo — that is the same wrong site shipped quietly that strict key decoding
// exists to prevent. Finding both is a loud error, not a guess.
var siteConfigNames = []string{"site.yml", "site.yaml"}

// siteConfig is the optional config of a *published site*, living with the content it publishes at
// "<srcDir>/site.yml". It is deliberately not the machine's ambient user config (~/.config/track):
// that one owns the user's machine (vault, cache, templates, editor, theme) and has no business
// deciding what a checked-in docs directory publishes. This one owns only what the site is — its entry
// page and its icons — so the same content publishes the same way on anyone's machine and in CI.
// Anything that changes per deployment of the same content (--base-url, --out, --frontend) stays a
// build flag, not site config. Absent file = zero value = a plain directory export, unchanged.
type siteConfig struct {
	// Home is the site's entry page, by file base name or page title. Unset, the "index" convention.
	Home string `yaml:"home"`
	// Icons maps a tag or a page kind to an emoji, exactly like the ambient config's icons: same shape,
	// same meaning, same precedence (see config.NoteIcon), so what you know from a vault carries over.
	Icons config.IconMap `yaml:"icons"`
}

// icons adapts the site's maps to the one icon resolver every other surface uses, so a published page
// and a vault note can never drift into two different precedence rules.
func (sc siteConfig) icons() *config.Config { return &config.Config{Icons: sc.Icons} }

// loadSiteConfig reads the site config out of srcDir if it is there; a directory without one publishes
// exactly as it did before the file existed. Decoding is strict: an unknown key — or a second YAML
// document, whose keys a single Decode would never even look at — is a loud error naming the file, never
// a silent drop, because a typo'd key in a config that only shows up at publish time would otherwise ship
// a wrong site quietly (the same reason note.ParseMetaDoc is strict).
func loadSiteConfig(srcDir string) (siteConfig, error) {
	var path string
	var raw []byte
	for _, name := range siteConfigNames {
		p := filepath.Join(srcDir, name)
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return siteConfig{}, fmt.Errorf("read site config: %w", err)
		}
		if path != "" {
			return siteConfig{}, fmt.Errorf("%s: a site config already exists as %s; keep one", p, path)
		}
		path, raw = p, b
	}
	if path == "" {
		return siteConfig{}, nil
	}
	var sc siteConfig
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&sc); err != nil && !errors.Is(err, io.EOF) {
		return siteConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := dec.Decode(new(siteConfig)); !errors.Is(err, io.EOF) {
		return siteConfig{}, fmt.Errorf("%s: want a single YAML document, found more than one", path)
	}
	return sc, nil
}

// inlineIcon returns a page's own icon override: the value of its first "icon::" inline field (a page
// has one icon; a later field is a duplicate, not an override), or "" when it has none or the value is
// empty. It plays the part a vault note's sidecar icon plays in config.NoteIcon — the per-page override
// that beats the site config's tag and kind maps. The field stays in the published props — it is an
// ordinary property, and hiding it there would be a special case that buys nothing — while its source
// line, being metadata, is kept out of the published prose like any other whole-line field
// (export.WebBody).
func inlineIcon(props []note.Prop) string {
	for _, p := range props {
		if p.Key == "icon" {
			return strings.TrimSpace(p.Value)
		}
	}
	return ""
}

// inlineTags lifts a page's "tags:: a, b" inline fields into its tags, which is where a directory page's
// tags can come from at all: it has no sidecar. InlineFields already splits a comma-separated value into
// one Prop per item, so "tags:: go, cli" is two tags. They are published with the page and are what the
// site config's icons.tags map matches against.
func inlineTags(props []note.Prop) []string {
	var out []string
	for _, p := range props {
		if p.Key == "tags" {
			if v := strings.TrimSpace(p.Value); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// firstHeading returns the text of the first level-1 ATX heading in body, or "" when there is none.
func firstHeading(body string) string {
	for _, h := range link.Headings(body) {
		if h.Level == 1 {
			return h.Text
		}
	}
	return ""
}
