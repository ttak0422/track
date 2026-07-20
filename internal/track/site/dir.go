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
// The bodies are pure Markdown and stay that way: no frontmatter, and no note-level fact smuggled into
// the prose as an "icon::" or "tags::" inline field (ADR 0002/0032). What a page needs said about it —
// its icon, its tags — is said once, in the site's own config at "<srcDir>/site.yml" (see siteConfig), which travels
// with the content it publishes. The machine's ambient user config is never read. With no site.yml at
// all this is a plain, config-free directory export whose entry page is the "index" convention.
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
	if err := checkPages(sc.Pages, idForSlug, srcDir); err != nil {
		return Result{}, err
	}

	// The entry page, named the same way a wiki link is: by file base name or by page title. The site's
	// own "home" names it; with no config, the "index" convention does. Nothing found is a loud error —
	// a site whose landing page silently moved is worse than one that fails to build. File base names
	// are matched first, and never through the merged link map: titles and base names share that
	// namespace, so a page whose H1 happens to spell another page's file name would otherwise steal the
	// front door — the same silent move, dressed as a link. Both spellings are matched exactly before
	// "index.md" is forgiven its extension, and only a ".md" one is: filepath.Ext would strip any dot
	// suffix, so a page titled "v1.2" would otherwise land on v1.md's file base name instead.
	entry := sc.Home
	if entry == "" {
		entry = "index"
	}
	root, ok := idForSlug[entry]
	if !ok {
		root, ok = keyToID[entry] // no file is named that: a page title, then.
	}
	if !ok && strings.EqualFold(filepath.Ext(entry), ".md") {
		root, ok = idForSlug[strings.TrimSuffix(entry, filepath.Ext(entry))]
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
		// A page's note-level metadata — its icon and its tags — comes from the site config's pages
		// map, the directory's stand-in for a vault note's sidecar (ADR 0049). Tags drive tag pages,
		// #tag search, and query FROM filters, exactly as sidecar tags do on a vault site.
		page := sc.Pages[f.slug]
		docs = append(docs, doc{
			id:    id,
			title: f.title,
			kind:  "note",
			tags:  page.Tags,
			path:  f.slug + ".md",
			body:  body,
			keys:  []string{f.slug, f.title},
			// One icon resolver for every surface: the page's own icon, then the site's tag map, then its
			// kind map (a directory page is always kind "note") — config.NoteIcon, the same precedence a
			// vault note resolves by, with the pages entry standing where the sidecar stands.
			icon:     sc.icons().NoteIcon("note", page.Tags, page.Icon),
			assets:   collectAssets(f.body),
			assetSrc: assetSrc,
			// A docs directory may keep canonical JSONL next to its assets, mirroring the vault's data/.
			dataDir: filepath.Join(srcDir, "data"),
			// Directory sources have no vault config, so tasks parse with the default state set.
			tasks: docTasks(f.body, nil),
			// Plain Markdown files have no sidecar, so inline "key:: value" fields are their only
			// properties: prose data, indexed from the line it is written on (ADR 0032).
			props: note.InlineFields(f.body),
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
	// sites never include it (the CLI rejects --calendar with --src). There is no vault config either,
	// so "saved:" query references do not resolve on a directory site.
	return writeBundle(docs, edges, root, false, baseURL, nil, frontendDir, outDir)
}

// checkPages rejects a pages entry that names no page, and one that says nothing. The first is a typo —
// a page renamed, or its name misspelled — and the page it meant to describe would otherwise publish
// with the wrong icon and no tags, without a word. The second — no icon, no tags, an entry YAML decodes
// to its zero value — is its sibling. Both are no-ops in a file that is only exercised at publish time,
// and an entry that does nothing is never what its author meant. Pages are checked in name order so the
// same directory always fails on the same entry.
func checkPages(pages map[string]sitePage, idForSlug map[string]int64, srcDir string) error {
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := idForSlug[name]; !ok {
			return fmt.Errorf("site config: pages entry %q names no page: there is no %s.md in %s",
				name, name, srcDir)
		}
		p := pages[name]
		if p.Icon == "" && len(p.Tags) == 0 {
			return fmt.Errorf("site config: pages entry %q says nothing: give %s.md an icon or tags, or drop the entry",
				name, name)
		}
	}
	return nil
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
// page, its pages' metadata, and its icons — so the same content publishes the same way on anyone's
// machine and in CI.
// Anything that changes per deployment of the same content (--base-url, --out, --frontend) stays a
// build flag, not site config. Absent file = zero value = a plain directory export, unchanged.
//
// It is also where a *page's* note-level metadata lives, for the reason a vault's does not: a vault note's
// sidecar is written and renamed by "track new" and "track open", so no one hand-maintains it, while a
// published directory is just files in a repository with no tool between the author and them. A per-page
// sidecar there would be boilerplate to hand-write and hand-rename; one map in the site's own config is
// not (ADR 0049).
type siteConfig struct {
	// Home is the site's entry page, by file base name or page title. Unset, the "index" convention.
	Home string `yaml:"home"`
	// Pages is the per-page note-level metadata, keyed by file base name — the directory's stand-in
	// for a vault note's sidecar (ADR 0049): one map here instead of one hand-maintained file per page.
	Pages map[string]sitePage `yaml:"pages"`
	// Icons resolves the icon beside each page's title.
	Icons siteIcons `yaml:"icons"`
}

// sitePage is what one published page says about itself: the same two facts a vault note's sidecar
// feeds the icon resolver and the index — its override icon and its tags — because it fills the same
// slot. Never written in the page's body (ADR 0002/0032).
type sitePage struct {
	Icon string   `yaml:"icon"`
	Tags []string `yaml:"tags"`
}

// siteIcons is what a published directory can say about its icon *mappings*: tags and kinds, the
// ambient config's maps with the same shape and meaning, so knowledge carries over from a vault
// unchanged. The per-page override lives in the pages map (sitePage.Icon), the slot a vault note's
// sidecar icon occupies; it is not a fourth precedence level. So the order stays config.NoteIcon's:
// a page's own icon, then its tags, then its kind.
type siteIcons struct {
	Tags  map[string]string `yaml:"tags"`
	Kinds map[string]string `yaml:"kinds"`
}

// icons adapts the site's maps to the one icon resolver every other surface uses, so a published page
// and a vault note can never drift into two different precedence rules.
func (sc siteConfig) icons() *config.Config {
	return &config.Config{Icons: config.IconMap{Tags: sc.Icons.Tags, Kinds: sc.Icons.Kinds}}
}

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

// firstHeading returns the text of the first level-1 ATX heading in body, or "" when there is none.
func firstHeading(body string) string {
	for _, h := range link.Headings(body) {
		if h.Level == 1 {
			return h.Text
		}
	}
	return ""
}
