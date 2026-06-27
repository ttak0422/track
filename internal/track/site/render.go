package site

import (
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/note"
)

// siteRenderer turns a note's track-specific spans into intermediate Markdown for the static-site
// build. Wiki links to notes inside the export set become Markdown links to their generated page;
// links to anything outside the set are flattened to inert plain text, since those pages are not
// published. Action links are editor-only and flattened. Babel blocks pass through as plain
// language-tagged fences for the Markdown-to-HTML stage to render.
type siteRenderer struct {
	resolve  func(key string) (string, bool) // wiki-link key -> published page slug
	inSet    map[string]bool                 // slugs that have a published page
	rootSlug string
}

func (r siteRenderer) WikiLink(inner string) string {
	key, display := splitWiki(inner)
	if key != "" && r.resolve != nil {
		if slug, ok := r.resolve(key); ok && r.inSet[slug] {
			return "[" + display + "](" + pageFile(slug, r.rootSlug) + ")"
		}
	}
	return display
}

func (siteRenderer) ActionLink(label string) string { return strings.TrimSpace(label) }

func (siteRenderer) CodeBlock(b babel.Block, _ string, _ *babel.RunResult) string {
	return "```" + b.Language + "\n" + b.Body + "\n```"
}

func (siteRenderer) Frontmatter(note.Metadata) string { return "" }

// splitWiki parses a [[...]] inner string into its resolution key and display text, mirroring the
// link package's splitDisplay/splitHeading semantics: the text after "|" is the display (falling back
// to the key), and a "#heading" anchor is dropped from the key. A trailing "#" with no heading text
// (e.g. "C#") stays part of the key.
func splitWiki(inner string) (key, display string) {
	target := inner
	if i := strings.IndexByte(inner, '|'); i >= 0 {
		target = inner[:i]
		display = strings.TrimSpace(inner[i+1:])
	}
	target = strings.TrimSpace(target)
	key = target
	if j := strings.IndexByte(target, '#'); j >= 0 {
		rest := target[j:]
		k := 0
		for k < len(rest) && rest[k] == '#' {
			k++
		}
		if strings.TrimSpace(rest[k:]) != "" {
			key = strings.TrimSpace(target[:j])
		}
	}
	if display == "" {
		display = key
	}
	return key, display
}
