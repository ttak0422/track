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
	// its output is meant to be portable. The fence length is preserved (not hardcoded to three) so a
	// nested ``` sample inside a longer ```` wrapper round-trips instead of closing early.
	fence := b.Fence
	if fence < 3 {
		fence = 3
	}
	f := strings.Repeat("`", fence)
	return f + b.Info + "\n" + b.Body + "\n" + f
}

func (webRenderer) Frontmatter(note.Metadata) string { return "" }

// WebBody renders a note body into the Markdown a web frontend draws: the webRenderer transform above,
// and nothing else. Every web surface goes through here (the live /api/render, the vault export, the
// directory export), so a note reads the same wherever it is published.
//
// The body is rendered whole. An inline "key:: value" field is data that belongs *in* the prose (ADR
// 0032) — a journal's "weight:: 68.2" is a line of the journal — so it renders as the text it is, and
// also appears in the property strip because the indexer reads it from the same line. Note-level
// metadata (a title, an icon) is not written in the body at all: it lives in the note's sidecar.
func WebBody(body string) (string, error) {
	res, err := Export(&note.Note{Body: body}, NewWebRenderer(), Options{})
	if err != nil {
		return "", err
	}
	return res.Markdown, nil
}
