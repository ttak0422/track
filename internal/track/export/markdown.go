package export

import (
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/note"
	"gopkg.in/yaml.v3"
)

// markdownRenderer renders notes back to Markdown: wiki links and template action links are flattened
// to plain text, babel blocks become plain language-tagged fences honoring :exports, and metadata is an
// optional YAML frontmatter block.
type markdownRenderer struct{}

// NewMarkdownRenderer returns the Markdown renderer.
func NewMarkdownRenderer() Renderer { return markdownRenderer{} }

func (markdownRenderer) WikiLink(inner string) string { return flattenWiki(inner) }

func (markdownRenderer) ActionLink(label string) string { return strings.TrimSpace(label) }

func (markdownRenderer) CodeBlock(b babel.Block, exports string, result *babel.RunResult) string {
	switch exports {
	case "none":
		return ""
	case "results":
		return renderResults(b, result)
	case "both":
		src := renderSource(b)
		res := renderResults(b, result)
		if res == "" {
			return src
		}
		return src + "\n\n" + res
	default: // "code" and anything unrecognized
		return renderSource(b)
	}
}

func (markdownRenderer) Frontmatter(meta note.Metadata) string {
	fm := frontmatter{
		Title:   meta.Title,
		Created: meta.Created,
		Tags:    meta.Tags,
		Aliases: meta.Aliases,
	}
	if fm.Title == "" && fm.Created == "" && len(fm.Tags) == 0 && len(fm.Aliases) == 0 {
		return ""
	}
	out, err := yaml.Marshal(fm)
	if err != nil {
		return ""
	}
	return "---\n" + string(out) + "---\n\n"
}

// frontmatter is the subset of note metadata exported as a YAML header. A dedicated struct keeps
// internal fields (version, babel blocks) out of the output and lets yaml.Marshal handle escaping.
type frontmatter struct {
	Title   string   `yaml:"title,omitempty"`
	Created string   `yaml:"created,omitempty"`
	Tags    []string `yaml:"tags,omitempty"`
	Aliases []string `yaml:"aliases,omitempty"`
}

func renderSource(b babel.Block) string {
	return "```" + b.Language + "\n" + b.Body + "\n```"
}

func renderResults(b babel.Block, result *babel.RunResult) string {
	if result == nil {
		return ""
	}
	content := resultContent(b, result)
	if content == "" {
		return ""
	}
	return "```\n" + content + "\n```"
}

// resultContent picks the text to show for a stored run based on the block's :results tokens.
// verbatim/scalar shows the raw value; otherwise (the default "output") it shows stdout, then stderr.
func resultContent(b babel.Block, result *babel.RunResult) string {
	tokens := b.HeaderArgs["results"]
	if containsAny(tokens, "verbatim", "scalar") && result.Value != "" {
		return strings.TrimRight(result.Value, "\n")
	}
	parts := make([]string, 0, 2)
	if result.Stdout != "" {
		parts = append(parts, strings.TrimRight(result.Stdout, "\n"))
	}
	if result.Stderr != "" {
		parts = append(parts, strings.TrimRight(result.Stderr, "\n"))
	}
	if len(parts) == 0 && result.Value != "" {
		parts = append(parts, strings.TrimRight(result.Value, "\n"))
	}
	return strings.Join(parts, "\n")
}

// flattenWiki turns a [[...]] inner string into plain text: the display text after "|" wins, otherwise
// the note key with any heading anchor dropped. A trailing "#" with no heading text (e.g. "C#") stays.
func flattenWiki(inner string) string {
	if i := strings.IndexByte(inner, '|'); i >= 0 {
		if disp := strings.TrimSpace(inner[i+1:]); disp != "" {
			return disp
		}
		inner = inner[:i]
	}
	target := strings.TrimSpace(inner)
	if j := strings.IndexByte(target, '#'); j >= 0 {
		rest := target[j:]
		k := 0
		for k < len(rest) && rest[k] == '#' {
			k++
		}
		if strings.TrimSpace(rest[k:]) != "" {
			return strings.TrimSpace(target[:j])
		}
	}
	return target
}

func containsAny(tokens []string, wants ...string) bool {
	for _, t := range tokens {
		for _, w := range wants {
			if t == w {
				return true
			}
		}
	}
	return false
}
