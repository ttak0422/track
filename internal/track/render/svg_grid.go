package render

import (
	"fmt"
	"html"
	"math"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// svgXMLProlog opens every standalone SVG document; inlining a chart into HTML strips it
// (RenderDocument), since an XML declaration is invalid mid-page.
const svgXMLProlog = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// writeSVGHeader emits the document prolog shared by every SVG chart: the XML declaration, the root
// <svg> sized to the canvas, a white background, and the centered title (when present).
func writeSVGHeader(b *strings.Builder, g svgGeom, title string) {
	b.WriteString(svgXMLProlog)
	fmt.Fprintf(b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" font-family="sans-serif">`+"\n",
		g.w, g.h, g.w, g.h)
	b.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>` + "\n")
	if title != "" {
		fmt.Fprintf(b, `<text x="%g" y="24" font-size="16" font-weight="bold" text-anchor="middle">%s</text>`+"\n",
			g.w/2, html.EscapeString(title))
	}
}

// renderGrid draws the 2D chart types (heatmap, timeline) onto the SVG canvas. Both share a grid of
// x columns × y rows; heatmap colors each cell by value, timeline places a sized dot per record.
func renderGrid(res viewspec.Resolved) string {
	g := svgGeom{w: 800, h: 480, left: 110, right: 16, top: 40, bottom: 56}
	if res.Chart == viewspec.ChartHeatmap {
		g.right = 72 // room for the color legend
	}
	grid := res.Grid
	if grid == nil {
		grid = &viewspec.Grid{}
	}
	var b strings.Builder
	writeSVGHeader(&b, g, res.Spec.Title)
	writeGridAxes(&b, g, grid)
	if res.Chart == viewspec.ChartHeatmap {
		writeHeatmapCells(&b, g, grid, res)
	} else {
		writeTimelineCells(&b, g, grid)
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// writeGridAxes draws the plot border, column labels along the bottom (thinned when too dense to
// read), and row labels down the left.
func writeGridAxes(b *strings.Builder, g svgGeom, grid *viewspec.Grid) {
	fmt.Fprintf(b, `<rect x="%g" y="%g" width="%g" height="%g" fill="none" stroke="#cccccc"/>`+"\n",
		g.left, g.top, g.plotW(), g.plotH())
	step := xLabelStep(len(grid.Cols), g.plotW())
	for i, c := range bandCenters(g, len(grid.Cols)) {
		if i%step != 0 {
			continue
		}
		fmt.Fprintf(b, `<text x="%s" y="%g" font-size="11" text-anchor="middle" fill="#333333">%s</text>`+"\n",
			num(c), g.top+g.plotH()+16, html.EscapeString(grid.Cols[i]))
	}
	for i, c := range bandCentersVertical(g, len(grid.Rows)) {
		fmt.Fprintf(b, `<text x="%g" y="%s" font-size="11" text-anchor="end" dominant-baseline="middle" fill="#333333">%s</text>`+"\n",
			g.left-6, num(c), html.EscapeString(grid.Rows[i]))
	}
}

// gridValueRange finds the value span across cells with a finite value, padding a degenerate range so
// scaling never divides by zero. With no finite values it falls back to [0,1].
func gridValueRange(cells []viewspec.Cell) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, c := range cells {
		if math.IsNaN(c.Value) || math.IsInf(c.Value, 0) {
			continue
		}
		lo, hi = math.Min(lo, c.Value), math.Max(hi, c.Value)
	}
	if math.IsInf(lo, 1) {
		return 0, 1
	}
	if lo == hi {
		hi = lo + 1
	}
	return lo, hi
}

// writeHeatmapCells fills one rectangle per cell, colored by value (light→dark, or the diverging
// market ramp with scale "diverging"), and draws a color legend in the right margin. A cell with no
// value is rendered light gray.
func writeHeatmapCells(b *strings.Builder, g svgGeom, grid *viewspec.Grid, res viewspec.Resolved) {
	nc, nr := len(grid.Cols), len(grid.Rows)
	if nc == 0 || nr == 0 {
		return
	}
	lo, hi := gridValueRange(grid.Cells)
	if res.DivergingColor() {
		lo, hi = divergingRange(lo, hi)
	}
	ramp := rampFor(res)
	cw, ch := g.plotW()/float64(nc), g.plotH()/float64(nr)
	for _, c := range grid.Cells {
		fill := "#eeeeee"
		if !math.IsNaN(c.Value) {
			fill = ramp((c.Value - lo) / (hi - lo))
		}
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s" stroke="#ffffff"/>`+"\n",
			num(g.left+cw*float64(c.Col)), num(g.top+ch*float64(c.Row)), num(cw), num(ch), fill)
	}
	writeHeatLegend(b, g, lo, hi, ramp)
}

// writeHeatLegend draws a vertical color ramp in the right margin with min/max value labels, so the
// heatmap's colors can be read back to numbers.
func writeHeatLegend(b *strings.Builder, g svgGeom, lo, hi float64, ramp func(float64) string) {
	const steps = 10
	lx := g.left + g.plotW() + 12
	sh := g.plotH() / steps
	for i := range steps {
		t := 1 - float64(i)/(steps-1) // top of the ramp is the high value
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="12" height="%s" fill="%s"/>`+"\n",
			num(lx), num(g.top+sh*float64(i)), num(sh+0.5), ramp(t))
	}
	fmt.Fprintf(b, `<text x="%s" y="%g" font-size="10" fill="#666666">%s</text>`+"\n", num(lx+14), g.top+8, num(hi))
	fmt.Fprintf(b, `<text x="%s" y="%g" font-size="10" fill="#666666">%s</text>`+"\n", num(lx+14), g.top+g.plotH(), num(lo))
}

// writeTimelineCells plots one dot per record at its (column, row) lane position. The dot radius
// scales with the cell value when a size encoding is present; otherwise a fixed radius is used. Each
// lane gets its own color so rows stay distinguishable.
func writeTimelineCells(b *strings.Builder, g svgGeom, grid *viewspec.Grid) {
	xc := bandCenters(g, len(grid.Cols))
	yc := bandCentersVertical(g, len(grid.Rows))
	// Faint lane guides so dots read against their row.
	for _, y := range yc {
		fmt.Fprintf(b, `<line x1="%g" y1="%s" x2="%g" y2="%s" stroke="#f2f2f2"/>`+"\n",
			g.left, num(y), g.left+g.plotW(), num(y))
	}
	lo, hi := gridValueRange(grid.Cells)
	for _, c := range grid.Cells {
		if c.Col >= len(xc) || c.Row >= len(yc) {
			continue
		}
		r := 4.0
		if !math.IsNaN(c.Value) {
			r = 3 + 7*(c.Value-lo)/(hi-lo) // 3..10 px
		}
		fmt.Fprintf(b, `<circle cx="%s" cy="%s" r="%s" fill="%s" fill-opacity="0.8"/>`+"\n",
			num(xc[c.Col]), num(yc[c.Row]), num(r), seriesColor(c.Row))
	}
}

// heatColor interpolates a single-hue blue ramp (light → dark) for a normalized value t in [0,1],
// clamping out-of-range inputs so a degenerate range still produces a valid color.
func heatColor(t float64) string {
	t = math.Max(0, math.Min(1, t))
	return fmt.Sprintf("#%02x%02x%02x", lerp8(247, 8, t), lerp8(251, 48, t), lerp8(255, 107, t))
}

// lerp8 linearly interpolates between two 0-255 channel values and rounds to an int byte.
func lerp8(a, b, t float64) int { return int(a + (b-a)*t + 0.5) }
