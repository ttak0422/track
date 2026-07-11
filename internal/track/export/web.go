package export

import (
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/note"
)

// webRenderer sanitizes a note body for the web frontend. Unlike the Markdown renderer it keeps
// [[...]] wiki links verbatim, because the frontend resolves them into navigable, hover-previewable
// links; it only flattens track action links, which are editor-only and not web-navigable. Code blocks
// pass through as plain language-tagged fences and metadata is dropped, since the frontend renders the
// body only and has its own code-block and link presentation.
type webRenderer struct{}

// NewWebRenderer returns a Renderer that prepares a note body for the web frontend: action links become
// plain text while wiki links, code, and ordinary Markdown are left for the frontend to render.
func NewWebRenderer() Renderer { return webRenderer{} }

func (webRenderer) WikiLink(inner string) string { return "[[" + inner + "]]" }

func (webRenderer) ActionLink(label string) string { return strings.TrimSpace(label) }

func (webRenderer) CodeBlock(b babel.Block, _ string, _ *babel.RunResult) string {
	// The full info string is kept, not just the language: fence header arguments carry rendering
	// options resolved after sanitization (a ```track-query fence's :layout, for one), and the
	// frontend reads only the first token anyway. Only the Markdown renderer strips them, because
	// its output is meant to be portable.
	return "```" + b.Info + "\n" + b.Body + "\n```"
}

func (webRenderer) Frontmatter(note.Metadata) string { return "" }
