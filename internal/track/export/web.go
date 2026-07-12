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
	return renderSource(b)
}

func (webRenderer) Frontmatter(note.Metadata) string { return "" }

// WebBody renders a note body into the Markdown a web frontend draws: the webRenderer transform above,
// plus whole-line inline fields blanked out (note.BlankFieldLines). Those fields are metadata the
// frontend already shows in the note's property strip, so leaving their "key:: value" source in the
// prose would print the same fact twice — once as data, once as raw syntax. Every web surface goes
// through here (the live /api/render, the vault export, the directory export), so a note reads the same
// wherever it is published. The Markdown export keeps the field lines: there the body is the note.
func WebBody(body string) (string, error) {
	res, err := Export(&note.Note{Body: note.BlankFieldLines(body)}, NewWebRenderer(), Options{})
	if err != nil {
		return "", err
	}
	return res.Markdown, nil
}
