// Package export writes a track note out to a portable format. The current target is Markdown.
// The design rationale is docs/adr/0011-markdown-export-pipeline.md and the behaviour is specified in
// docs/spec/export.md.
//
// Export is a lightweight transform pipeline rather than a full document AST: it reuses the engine's
// existing line-range-aware parsers (link, babel) to find the track-specific spans that must be
// rewritten, and passes everything else through unchanged. A Renderer turns each span into output, so
// the output format stays swappable without touching the pipeline.
package export

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/note"
)

// Renderer turns the track-specific spans of a note into output for one format. It is the analogue of
// an org-export backend; the first implementation is Markdown (see markdown.go).
type Renderer interface {
	// WikiLink renders a [[...]] occurrence given its inner text (the part between the brackets).
	WikiLink(inner string) string
	// ActionLink renders a track action markdown link given its label (empty when the link had none).
	ActionLink(label string) string
	// CodeBlock renders a babel block under the resolved exports mode, with the stored result if any.
	CodeBlock(b babel.Block, exports string, result *babel.RunResult) string
	// Frontmatter renders a leading metadata block, or "" when there is nothing to emit.
	Frontmatter(meta note.Metadata) string
}

// Options is the communication channel threaded through a run, the analogue of org-export's info plist.
type Options struct {
	// Frontmatter prepends a metadata block when true.
	Frontmatter bool
	// DefaultExports is the exports mode for babel blocks that omit :exports; empty means "code".
	DefaultExports string
}

// Result carries the rendered document plus any non-fatal warnings (e.g. results requested for a block
// that has no stored run).
type Result struct {
	Markdown string
	Warnings []string
}

var (
	// actionLink matches a markdown link whose destination is an angle-bracketed track action,
	// capturing the label. A plain [text](http://...) link has no "<" after "(" and is left untouched.
	actionLink = regexp.MustCompile(`\[([^\[\]]*)\]\(<[^>]*>\)`)
	// bareAction matches an angle-bracketed action that stands without a label.
	bareAction = regexp.MustCompile(`<(?:journal|note)\?[^>]*>`)
	// wikiLink matches a single [[...]] occurrence, capturing the inner text.
	wikiLink = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)
)

// Export renders a note with the given renderer and options.
func Export(n *note.Note, r Renderer, opts Options) (Result, error) {
	var res Result
	var out []string

	if opts.Frontmatter {
		if fm := r.Frontmatter(n.Meta); fm != "" {
			// The trailing "" becomes a blank line between the frontmatter and the body after join.
			out = append(out, strings.TrimRight(fm, "\n"), "")
		}
	}

	defaultExports := opts.DefaultExports
	if defaultExports == "" {
		defaultExports = "code"
	}

	body := strings.TrimRight(n.Body, "\n")
	lines := strings.Split(body, "\n")
	blocksByStart := make(map[int]babel.Block)
	for _, blk := range babel.ParseBlocks(body) {
		blocksByStart[blk.StartLine] = blk
	}

	for i := 0; i < len(lines); {
		if blk, ok := blocksByStart[i]; ok {
			exports := firstHeader(blk, "exports", defaultExports)
			var result *babel.RunResult
			if meta, ok := n.Meta.Blocks[blk.ID(n.ID)]; ok {
				result = meta.LastRun
			}
			if result == nil && (exports == "results" || exports == "both") {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("block %q: :exports %s requested but no stored result", blk.ID(n.ID), exports))
			}
			if rendered := r.CodeBlock(blk, exports, result); rendered != "" {
				out = append(out, rendered)
			}
			i = blk.EndLine + 1
			continue
		}
		if isFence(lines[i]) {
			// A plain (non-babel) fenced block passes through verbatim: no link rewriting inside code.
			out = append(out, lines[i])
			i++
			for i < len(lines) && !isFence(lines[i]) {
				out = append(out, lines[i])
				i++
			}
			if i < len(lines) {
				out = append(out, lines[i])
				i++
			}
			continue
		}
		out = append(out, transformInline(lines[i], r))
		i++
	}

	res.Markdown = strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	return res, nil
}

// transformInline rewrites the track-specific inline spans of a single body line. Action links are
// handled before wiki links; their single-bracket label cannot match the double-bracket wiki pattern,
// so the order only matters for clarity.
func transformInline(line string, r Renderer) string {
	line = actionLink.ReplaceAllStringFunc(line, func(m string) string {
		return r.ActionLink(actionLink.FindStringSubmatch(m)[1])
	})
	line = bareAction.ReplaceAllString(line, "")
	line = wikiLink.ReplaceAllStringFunc(line, func(m string) string {
		return r.WikiLink(wikiLink.FindStringSubmatch(m)[1])
	})
	return line
}

func firstHeader(b babel.Block, key, def string) string {
	if vs := b.HeaderArgs[key]; len(vs) > 0 {
		return vs[0]
	}
	return def
}

func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}
