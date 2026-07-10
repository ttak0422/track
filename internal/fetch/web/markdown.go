package web

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// renderMarkdown converts a content node to Markdown: blocks joined by blank lines, links and
// images resolved against base. It is deliberately a best-effort converter for readable prose —
// unknown elements degrade to their text, never break the output.
func renderMarkdown(content *html.Node, base *url.URL) string {
	r := &mdRenderer{base: base}
	r.container(content)
	return strings.Join(r.blocks, "\n\n")
}

type mdRenderer struct {
	base   *url.URL
	blocks []string
}

func (r *mdRenderer) push(block string) {
	if block = strings.TrimRight(block, " \n"); block != "" {
		r.blocks = append(r.blocks, block)
	}
}

// container renders a node whose children are a mix of blocks and stray inline content. Runs of
// inline nodes (text directly under a div, say) are grouped into implicit paragraphs.
func (r *mdRenderer) container(n *html.Node) {
	var inline []*html.Node
	flush := func() {
		if len(inline) == 0 {
			return
		}
		var b strings.Builder
		for _, c := range inline {
			b.WriteString(r.inline(c))
		}
		r.push(tidyInline(b.String()))
		inline = nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isBlock(c) {
			flush()
			r.block(c)
		} else {
			inline = append(inline, c)
		}
	}
	flush()
}

func isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Ul, atom.Ol, atom.Blockquote, atom.Pre, atom.Hr, atom.Table,
		atom.Figure, atom.Figcaption, atom.Div, atom.Section, atom.Article,
		atom.Main, atom.Dl, atom.Dt, atom.Dd, atom.Details, atom.Summary:
		return true
	}
	return false
}

func (r *mdRenderer) block(n *html.Node) {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		if text := tidyInline(r.inlineChildren(n)); text != "" {
			r.push(strings.Repeat("#", level) + " " + strings.ReplaceAll(text, "\n", " "))
		}
	case atom.P, atom.Dt, atom.Dd, atom.Summary:
		r.push(tidyInline(r.inlineChildren(n)))
	case atom.Figcaption:
		if text := tidyInline(r.inlineChildren(n)); text != "" {
			r.push("*" + text + "*")
		}
	case atom.Ul, atom.Ol:
		r.push(strings.Join(r.list(n, 0), "\n"))
	case atom.Blockquote:
		sub := &mdRenderer{base: r.base}
		sub.container(n)
		var lines []string
		for _, line := range strings.Split(strings.Join(sub.blocks, "\n\n"), "\n") {
			lines = append(lines, strings.TrimRight("> "+line, " "))
		}
		if len(sub.blocks) > 0 {
			r.push(strings.Join(lines, "\n"))
		}
	case atom.Pre:
		r.push(fencedCode(n))
	case atom.Hr:
		r.push("---")
	case atom.Table:
		r.push(r.table(n))
	default: // div, section, article, figure, dl, ... — recurse
		r.container(n)
	}
}

// list renders ul/ol items, nesting by two-space indentation.
func (r *mdRenderer) list(n *html.Node, depth int) []string {
	ordered := n.DataAtom == atom.Ol
	indent := strings.Repeat("  ", depth)
	var lines []string
	item := 0
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		item++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", item)
		}
		var text strings.Builder
		var nested []string
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.ElementNode && (c.DataAtom == atom.Ul || c.DataAtom == atom.Ol):
				nested = append(nested, r.list(c, depth+1)...)
			case c.Type == html.ElementNode && c.DataAtom == atom.P:
				text.WriteString(" " + r.inlineChildren(c))
			default:
				text.WriteString(r.inline(c))
			}
		}
		if t := tidyInline(text.String()); t != "" {
			lines = append(lines, indent+marker+t)
		}
		lines = append(lines, nested...)
	}
	return lines
}

// table renders a pipe table: the first row becomes the header. Cell text is single-line with pipes
// escaped.
func (r *mdRenderer) table(n *html.Node) string {
	var rows [][]string
	walk(n, func(c *html.Node) bool {
		if c.Type == html.ElementNode && c.DataAtom == atom.Tr {
			var cells []string
			for td := c.FirstChild; td != nil; td = td.NextSibling {
				if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
					cell := strings.ReplaceAll(tidyInline(r.inlineChildren(td)), "\n", " ")
					cells = append(cells, strings.ReplaceAll(cell, "|", "\\|"))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return false
		}
		return true
	})
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	writeRow(rows[0])
	separator := make([]string, len(rows[0]))
	for i := range separator {
		separator[i] = "---"
	}
	writeRow(separator)
	for _, row := range rows[1:] {
		writeRow(row)
	}
	return strings.TrimRight(b.String(), "\n")
}

// fencedCode renders a <pre> block, keeping its text verbatim and picking up a language from the
// conventional class="language-x" on its <code> child.
func fencedCode(n *html.Node) string {
	lang := ""
	if code := find(n, atom.Code); code != nil {
		for _, class := range strings.Fields(attr(code, "class")) {
			for _, prefix := range []string{"language-", "lang-"} {
				if strings.HasPrefix(class, prefix) {
					lang = strings.TrimPrefix(class, prefix)
				}
			}
		}
	}
	text := strings.Trim(innerText(n), "\n")
	return "```" + lang + "\n" + text + "\n```"
}

// inlineChildren renders every child of n as inline content.
func (r *mdRenderer) inlineChildren(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(r.inline(c))
	}
	return b.String()
}

// inline renders one inline node. Formatting wrappers are skipped when empty so stray markup never
// yields bare ** or [].
func (r *mdRenderer) inline(n *html.Node) string {
	if n.Type == html.TextNode {
		return spaceRe.ReplaceAllString(n.Data, " ")
	}
	if n.Type != html.ElementNode {
		return ""
	}
	switch n.DataAtom {
	case atom.Br:
		return "\n"
	case atom.Img:
		src := resolveURL(r.base, attr(n, "src"))
		if src == "" {
			return ""
		}
		return fmt.Sprintf("![%s](%s)", collapseSpace(attr(n, "alt")), src)
	case atom.A:
		label := strings.TrimSpace(r.inlineChildren(n))
		href := resolveURL(r.base, attr(n, "href"))
		if label == "" {
			return ""
		}
		if href == "" {
			return label + " "
		}
		return fmt.Sprintf("[%s](%s)", label, href)
	case atom.Strong, atom.B:
		return wrap(strings.TrimSpace(r.inlineChildren(n)), "**")
	case atom.Em, atom.I:
		return wrap(strings.TrimSpace(r.inlineChildren(n)), "*")
	case atom.Del, atom.S:
		return wrap(strings.TrimSpace(r.inlineChildren(n)), "~~")
	case atom.Code:
		return wrap(collapseSpace(innerText(n)), "`")
	default:
		return r.inlineChildren(n)
	}
}

func wrap(s, mark string) string {
	if s == "" {
		return ""
	}
	return " " + mark + s + mark + " "
}

var horizontalSpaceRe = regexp.MustCompile(`[ \t]+`)

// tidyInline normalizes an assembled inline run: collapse horizontal whitespace, trim each line,
// and drop empty edges. <br> newlines survive.
func tidyInline(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(horizontalSpaceRe.ReplaceAllString(line, " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
