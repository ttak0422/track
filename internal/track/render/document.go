package render

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// markedCDN renders Markdown prose blocks client-side, so document composition needs no Go Markdown
// dependency (ADR 0019 deliberately dropped one). Like Chart.js, it is a CDN script; the generated
// page requires network access at view time.
const markedCDN = "https://cdn.jsdelivr.net/npm/marked@12"

// Document is a composed article: an ordered mix of prose and charts rendered into one HTML page. The
// caller resolves each chart's data before rendering, so this layer stays free of file IO.
type Document struct {
	Title string
	Items []Item
}

// Item is one block of a Document: either Markdown prose or a resolved chart. Exactly one is set.
type Item struct {
	Markdown string
	Chart    *viewspec.Resolved
}

// RenderDocument composes prose and charts into a single self-contained HTML page. Charts reuse the
// same Chart.js config builder as the single-chart renderer; prose is rendered from Markdown by
// marked.js at view time. CDNs are loaded conditionally: marked only when there is prose, the
// annotation plugin only when some chart has overlay markers.
func RenderDocument(doc Document) (string, error) {
	var charts []string // chart config JSON, in document order
	hasProse := false
	usesAnnotation := false

	var body strings.Builder
	mdIndex := 0
	for _, it := range doc.Items {
		switch {
		case it.Chart != nil:
			cfgJSON, ann, err := chartJSConfigJSON(*it.Chart)
			if err != nil {
				return "", err
			}
			if ann {
				usesAnnotation = true
			}
			fmt.Fprintf(&body, `<div class="chart-wrap"><canvas id="chart-%d"></canvas></div>`+"\n", len(charts))
			charts = append(charts, cfgJSON)
		default:
			hasProse = true
			fmt.Fprintf(&body, `<div class="prose" data-md="%d"></div>`+"\n", mdIndex)
			mdIndex++
		}
	}

	// Markdown sources travel as a JSON array consumed by marked at runtime. Go's json escapes <, >, &
	// so the array is safe to inline in a <script>.
	var mds []string
	for _, it := range doc.Items {
		if it.Chart == nil {
			mds = append(mds, it.Markdown)
		}
	}
	mdJSON, err := json.Marshal(mds)
	if err != nil {
		return "", fmt.Errorf("marshal prose: %w", err)
	}

	title := doc.Title
	if title == "" {
		title = "track document"
	}
	return renderDocumentPage(html.EscapeString(title), body.String(), charts, string(mdJSON), hasProse, usesAnnotation), nil
}

// renderDocumentPage assembles the article HTML: the prepared body (prose placeholders + chart
// canvases), the chart configs, and the prose sources. CDNs load conditionally — the annotation plugin
// only when a chart uses markers, marked only when there is prose. The script wires marked over the
// prose placeholders and instantiates each chart.
func renderDocumentPage(escapedTitle, body string, charts []string, mdJSON string, hasProse, usesAnnotation bool) string {
	scripts := []string{chartJSCDN}
	if usesAnnotation {
		scripts = append(scripts, annotationCDN)
	}
	if hasProse {
		scripts = append(scripts, markedCDN)
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	b.WriteString("<title>" + escapedTitle + "</title>\n")
	for _, src := range scripts {
		b.WriteString(`<script src="` + src + `"></script>` + "\n")
	}
	b.WriteString("<style>body{margin:0 auto;max-width:880px;padding:24px;font-family:system-ui,sans-serif;line-height:1.6}" +
		".chart-wrap{position:relative;height:360px;margin:24px 0}.prose{margin:16px 0}</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString(body)
	b.WriteString("<script>\n")
	b.WriteString("const charts = [" + strings.Join(charts, ",") + "];\n")
	b.WriteString(`charts.forEach((c, i) => new Chart(document.getElementById("chart-" + i), c));` + "\n")
	if hasProse {
		b.WriteString("const prose = " + mdJSON + ";\n")
		b.WriteString(`document.querySelectorAll(".prose").forEach(el => { el.innerHTML = marked.parse(prose[+el.dataset.md]); });` + "\n")
	}
	b.WriteString("</script>\n</body>\n</html>\n")
	return b.String()
}
