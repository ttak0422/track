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

// Item is one block of a Document: Markdown prose, a resolved chart, or a resolved table. Exactly one
// is set.
type Item struct {
	Markdown string
	Chart    *viewspec.Resolved
	Table    *viewspec.ResolvedTable
}

// RenderDocument composes prose, charts, and tables into a single self-contained HTML page. Charts
// reuse the same Chart.js config builder as the single-chart renderer, except SVG-only forms
// (candlestick, heatmap, timeline), which are inlined as static server-rendered SVG; prose is rendered
// from Markdown by marked.js at view time; tables are server-side HTML (no CDN). CDNs/scripts load
// conditionally: Chart.js only with a canvas chart, marked only with prose, the annotation plugin only
// when a chart has markers, the filter script only when a table is filterable.
func RenderDocument(doc Document) (string, error) {
	var charts []string // chart config JSON, in document order
	var mds []string    // prose sources, in prose order (aligned to data-md indices)
	tableCount := 0
	usesAnnotation := false
	hasFilter := false

	var body strings.Builder
	for _, it := range doc.Items {
		switch {
		case it.Chart != nil:
			// Forms Chart.js cannot draw (candlestick, heatmap, timeline) are inlined as static
			// server-rendered SVG instead, so an article can still compose them.
			if svgOnlyChart(it.Chart.Chart) {
				svg, err := (SVG{}).Render(*it.Chart)
				if err != nil {
					return "", err
				}
				body.WriteString(`<div class="chart-wrap chart-wrap-svg">` + strings.TrimPrefix(svg, svgXMLProlog) + "</div>\n")
				continue
			}
			cfgJSON, ann, err := chartJSConfigJSON(*it.Chart)
			if err != nil {
				return "", err
			}
			if ann {
				usesAnnotation = true
			}
			fmt.Fprintf(&body, `<div class="chart-wrap"><canvas id="chart-%d"></canvas></div>`+"\n", len(charts))
			charts = append(charts, cfgJSON)
		case it.Table != nil:
			if it.Table.Filter {
				hasFilter = true
			}
			writeTable(&body, tableCount, *it.Table)
			tableCount++
		default:
			fmt.Fprintf(&body, `<div class="prose" data-md="%d"></div>`+"\n", len(mds))
			mds = append(mds, it.Markdown)
		}
	}

	// Markdown sources travel as a JSON array consumed by marked at runtime. Go's json escapes <, >, &
	// so the array is safe to inline in a <script>.
	mdJSON, err := json.Marshal(mds)
	if err != nil {
		return "", fmt.Errorf("marshal prose: %w", err)
	}

	title := doc.Title
	if title == "" {
		title = "track document"
	}
	return renderDocumentPage(html.EscapeString(title), body.String(), charts, string(mdJSON), len(mds) > 0, usesAnnotation, hasFilter), nil
}

// writeTable renders a resolved table as server-side HTML: an optional filter box plus a <table> with
// escaped headers and cells. id makes the element unique within the page for filter wiring. Tables
// need no CDN, so they render (and filter) offline.
func writeTable(b *strings.Builder, id int, t viewspec.ResolvedTable) {
	b.WriteString(`<div class="table-wrap">` + "\n")
	if t.Filter {
		fmt.Fprintf(b, `<input class="table-filter" data-table-filter="table-%d" placeholder="Filter…" aria-label="Filter table">`+"\n", id)
	}
	fmt.Fprintf(b, `<table id="table-%d">`+"\n<thead><tr>", id)
	for _, c := range t.Columns {
		b.WriteString("<th>" + html.EscapeString(c) + "</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range t.Rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString("<td>" + html.EscapeString(cell) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n</div>\n")
}

// renderDocumentPage assembles the article HTML: the prepared body (prose placeholders + chart
// canvases + tables), the chart configs, and the prose sources. CDNs load conditionally — the
// annotation plugin only when a chart uses markers, marked only when there is prose. The script wires
// marked over the prose placeholders, instantiates each chart, and wires table filters (no CDN).
func renderDocumentPage(escapedTitle, body string, charts []string, mdJSON string, hasProse, usesAnnotation, hasFilter bool) string {
	var scripts []string
	if len(charts) > 0 {
		scripts = append(scripts, chartJSCDN)
	}
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
		".chart-wrap{position:relative;height:360px;margin:24px 0}.prose{margin:16px 0}" +
		".chart-wrap-svg{height:auto}.chart-wrap-svg svg{max-width:100%;height:auto}" +
		".table-wrap{margin:24px 0;overflow-x:auto}.table-filter{margin-bottom:8px;padding:6px 8px;width:100%;box-sizing:border-box}" +
		"table{border-collapse:collapse;width:100%}th,td{border:1px solid #ddd;padding:6px 10px;text-align:left}thead th{background:#f5f5f5}</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString(body)
	b.WriteString("<script>\n")
	b.WriteString("const charts = [" + strings.Join(charts, ",") + "];\n")
	b.WriteString(`charts.forEach((c, i) => new Chart(document.getElementById("chart-" + i), c));` + "\n")
	if hasProse {
		b.WriteString("const prose = " + mdJSON + ";\n")
		b.WriteString(`document.querySelectorAll(".prose").forEach(el => { el.innerHTML = marked.parse(prose[+el.dataset.md]); });` + "\n")
	}
	if hasFilter {
		b.WriteString(`document.querySelectorAll(".table-filter").forEach(input => {` + "\n")
		b.WriteString(`  const rows = document.getElementById(input.dataset.tableFilter).tBodies[0].rows;` + "\n")
		b.WriteString(`  input.addEventListener("input", () => {` + "\n")
		b.WriteString(`    const q = input.value.toLowerCase();` + "\n")
		b.WriteString(`    for (const r of rows) r.style.display = r.textContent.toLowerCase().includes(q) ? "" : "none";` + "\n")
		b.WriteString(`  });` + "\n})\n")
	}
	b.WriteString("</script>\n</body>\n</html>\n")
	return b.String()
}
