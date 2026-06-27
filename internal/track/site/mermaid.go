package site

import "regexp"

// mermaidBlock matches a fenced ```mermaid block as rendered by goldmark: a <pre><code> with the
// mermaid language class. The captured group is the (HTML-escaped) diagram source; mermaid reads it
// back via the element's textContent, which the browser un-escapes, so the escaping is preserved.
var mermaidBlock = regexp.MustCompile(`(?s)<pre><code class="language-mermaid">(.*?)</code></pre>`)

// transformMermaid rewrites goldmark's mermaid code blocks into <div class="mermaid"> containers that
// the mermaid runtime renders, and reports whether any were found (so the page can pull in the script
// only when needed). Operating on the rendered HTML keeps it robust to blank lines inside a diagram,
// which would otherwise break a raw-HTML pass through the Markdown stage.
func transformMermaid(htmlBody string) (string, bool) {
	found := false
	out := mermaidBlock.ReplaceAllStringFunc(htmlBody, func(m string) string {
		found = true
		src := mermaidBlock.FindStringSubmatch(m)[1]
		return `<div class="mermaid">` + src + `</div>`
	})
	return out, found
}

// mermaidScript loads the mermaid runtime and renders the diagrams on the page. It is included only on
// pages that contain a diagram. The runtime is pulled from a CDN, matching how an SSG site is normally
// hosted (e.g. GitHub Pages).
const mermaidScript = `<script type="module">
import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
mermaid.initialize({ startOnLoad: true });
</script>
`
