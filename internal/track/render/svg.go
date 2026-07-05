package render

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

func init() { Register(SVG{}) }

// SVG renders a resolved View Spec as a self-contained, dependency-free SVG document. // ECharts renderer it loads no scripts and no CDN, so the output is a static image suitable for
// embedding in notes, emails, or a static site. It draws the category-axis chart types (line, area,
// bar, hbar, scatter, candlestick) plus bubble (a linear-axis {x,y,r} scatter) and overlay markers.
type SVG struct{}

// Name identifies this renderer for selection (track render --renderer svg).
func (SVG) Name() string { return "svg" }

// svgGeom is the fixed canvas layout: overall size and the plot-area insets (room for the title, the
// y-axis labels on the left, and the x-axis labels at the bottom).
type svgGeom struct {
	w, h                     float64
	left, right, top, bottom float64
}

func (g svgGeom) plotW() float64 { return g.w - g.left - g.right }
func (g svgGeom) plotH() float64 { return g.h - g.top - g.bottom }

// seriesPalette cycles per series index and is shared by both renderers, so the same spec draws its
// series (including a color-channel category split) in the same deterministic colors everywhere. Six
// distinct hues are plenty for a readable chart; the heatmap's quantitative color ramp (heatColor) is
// a separate scale and unaffected.
var seriesPalette = []string{"#4e79a7", "#f28e2b", "#e15759", "#76b7b2", "#59a14f", "#edc948"}

func seriesColor(i int) string { return seriesPalette[i%len(seriesPalette)] }

// Render produces a complete SVG document for the resolved spec.
func (SVG) Render(res viewspec.Resolved) (string, error) {
	switch res.Chart {
	case viewspec.ChartBubble:
		return renderBubble(res), nil
	case viewspec.ChartHeatmap, viewspec.ChartTimeline:
		return renderGrid(res), nil
	}
	g := svgGeom{w: 800, h: 480, left: 56, right: 16, top: 40, bottom: 56}
	lo, hi := valueRange(res)

	var b strings.Builder
	writeSVGHeader(&b, g, res.Spec.Title)
	writeAxes(&b, g, res, lo, hi)
	if res.Chart == viewspec.ChartHBar {
		writeHBars(&b, g, res, lo, hi)
	} else {
		writeBands(&b, g, res)
		writeSeries(&b, g, res, lo, hi)
		writeMarkers(&b, g, res)
		writeRefLines(&b, g, res, lo, hi)
		writeCallouts(&b, g, res, lo, hi)
	}
	// A candlestick's four series (open/high/low/close) are components of one mark, not user series,
	// and its up/down coloring is fixed — a legend would mislabel it.
	if res.Chart != viewspec.ChartCandlestick {
		writeLegend(&b, g, res)
	}

	b.WriteString("</svg>\n")
	return b.String(), nil
}

// valueRange finds the value span across all finite points in every series. For bar charts the
// baseline is pinned to zero so bars are measured from a common origin rather than floating; stacked
// bars span the per-category stack totals instead of individual values. A degenerate (zero-width)
// range is padded so the chart still has height.
func valueRange(res viewspec.Resolved) (lo, hi float64) {
	if res.Stacked {
		return stackedValueRange(res)
	}
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, s := range res.Series {
		for _, v := range s.Values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 1) { // no finite data at all
		lo, hi = 0, 1
	}
	// Bars and areas measure from a zero baseline, so the axis must include it (a combo chart needs
	// it as soon as any of its series draws as one).
	needsZero := res.Chart == viewspec.ChartBar || res.Chart == viewspec.ChartHBar || res.Chart == viewspec.ChartArea
	for i := range res.Series {
		if f := res.SeriesForm(i); f == viewspec.ChartBar || f == viewspec.ChartArea {
			needsZero = true
		}
	}
	if needsZero {
		lo, hi = math.Min(lo, 0), math.Max(hi, 0)
	}
	if lo == hi {
		lo, hi = lo-1, hi+1
	}
	return lo, hi
}

// stackedValueRange spans the stacked bar totals: per category, positive values pile up and negative
// values pile down, so the axis must reach the summed extents (and zero, the common baseline).
func stackedValueRange(res viewspec.Resolved) (lo, hi float64) {
	lo, hi = 0, 0
	for i := range res.Labels {
		pos, neg := 0.0, 0.0
		for _, s := range res.Series {
			if i >= len(s.Values) {
				continue
			}
			v := s.Values[i]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			if v >= 0 {
				pos += v
			} else {
				neg += v
			}
		}
		lo, hi = math.Min(lo, neg), math.Max(hi, pos)
	}
	if lo == hi {
		lo, hi = lo-1, hi+1
	}
	return lo, hi
}

// bandCenters returns the x pixel at the center of each of n category slots across the plot width.
func bandCenters(g svgGeom, n int) []float64 {
	cs := make([]float64, n)
	if n == 0 {
		return cs
	}
	band := g.plotW() / float64(n)
	for i := range cs {
		cs[i] = g.left + band*(float64(i)+0.5)
	}
	return cs
}

// yPixel maps a value to its vertical pixel position (top of plot = hi, bottom = lo).
func yPixel(g svgGeom, lo, hi, v float64) float64 {
	return g.top + g.plotH()*(1-(v-lo)/(hi-lo))
}

// xLabelStep thins dense category labels: with n categories across plotW pixels, only every step-th
// label is drawn so neighbours don't collide (a daily time series easily has 60+ categories).
// ponytail: fixed 64px per label; measure real text width if labels get much longer than dates
func xLabelStep(n int, plotW float64) int {
	step := int(math.Ceil(float64(n) * 64 / plotW))
	if step < 1 {
		return 1
	}
	return step
}

// writeAxes draws the plot frame, three value gridlines+labels, and the category labels. hbar
// transposes the axes: its value axis runs horizontally (gridlines vertical, labels along the bottom)
// and its categories run down the left, so value and category labels never collide.
func writeAxes(b *strings.Builder, g svgGeom, res viewspec.Resolved, lo, hi float64) {
	// Plot border.
	fmt.Fprintf(b, `<rect x="%g" y="%g" width="%g" height="%g" fill="none" stroke="#cccccc"/>`+"\n",
		g.left, g.top, g.plotW(), g.plotH())

	if res.Chart == viewspec.ChartHBar {
		// Value axis horizontal: vertical gridlines + value labels along the bottom.
		for _, frac := range []float64{0, 0.5, 1} {
			v := lo + (hi-lo)*frac
			x := xPixel(g, lo, hi, v)
			fmt.Fprintf(b, `<line x1="%s" y1="%g" x2="%s" y2="%g" stroke="#eeeeee"/>`+"\n",
				num(x), g.top, num(x), g.top+g.plotH())
			fmt.Fprintf(b, `<text x="%s" y="%g" font-size="11" text-anchor="middle" fill="#666666">%s</text>`+"\n",
				num(x), g.top+g.plotH()+16, num(v))
		}
		// Categories down the left.
		centers := bandCentersVertical(g, len(res.Labels))
		for i, lbl := range res.Labels {
			fmt.Fprintf(b, `<text x="%g" y="%s" font-size="11" text-anchor="end" dominant-baseline="middle" fill="#333333">%s</text>`+"\n",
				g.left-6, num(centers[i]), html.EscapeString(lbl))
		}
		return
	}

	// Value axis vertical: horizontal gridlines + value labels on the left.
	for _, frac := range []float64{0, 0.5, 1} {
		v := lo + (hi-lo)*frac
		y := yPixel(g, lo, hi, v)
		fmt.Fprintf(b, `<line x1="%g" y1="%s" x2="%g" y2="%s" stroke="#eeeeee"/>`+"\n",
			g.left, num(y), g.left+g.plotW(), num(y))
		fmt.Fprintf(b, `<text x="%g" y="%s" font-size="11" text-anchor="end" dominant-baseline="middle" fill="#666666">%s</text>`+"\n",
			g.left-6, num(y), num(v))
	}
	// Categories along the bottom, thinned when too dense to read.
	centers := bandCenters(g, len(res.Labels))
	step := xLabelStep(len(res.Labels), g.plotW())
	for i, lbl := range res.Labels {
		if i%step != 0 {
			continue
		}
		fmt.Fprintf(b, `<text x="%s" y="%g" font-size="11" text-anchor="middle" fill="#333333">%s</text>`+"\n",
			num(centers[i]), g.top+g.plotH()+16, html.EscapeString(lbl))
	}
}

// writeSeries draws each y series by its own form — the chart's, or the series' mark override in a
// combo chart — over a shared category x axis. Bars draw first so lines and areas stay readable on
// top of them.
func writeSeries(b *strings.Builder, g svgGeom, res viewspec.Resolved, lo, hi float64) {
	centers := bandCenters(g, len(res.Labels))
	if res.Chart == viewspec.ChartCandlestick {
		writeCandles(b, g, res, centers, lo, hi)
		return
	}
	writeBars(b, g, res, centers, lo, hi)
	for si, s := range res.Series {
		switch res.SeriesForm(si) {
		case viewspec.ChartArea:
			writeAreaFill(b, g, centers, s.Values, lo, hi, seriesColor(si))
			writePolyline(b, g, centers, s.Values, lo, hi, seriesColor(si))
		case viewspec.ChartScatter:
			for i, v := range s.Values {
				if math.IsNaN(v) || i >= len(centers) {
					continue
				}
				fmt.Fprintf(b, `<circle cx="%s" cy="%s" r="3.5" fill="%s"/>`+"\n",
					num(centers[i]), num(yPixel(g, lo, hi, v)), seriesColor(si))
			}
		case viewspec.ChartLine:
			writePolyline(b, g, centers, s.Values, lo, hi, seriesColor(si))
		}
	}
}

// writePolyline draws a series as connected segments, breaking the line at NaN gaps so a missing
// value reads as a hole rather than a straight line through it.
func writePolyline(b *strings.Builder, g svgGeom, centers, vals []float64, lo, hi float64, color string) {
	var run []string
	flush := func() {
		if len(run) >= 2 {
			fmt.Fprintf(b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`+"\n",
				strings.Join(run, " "), color)
		}
		run = run[:0]
	}
	for i, v := range vals {
		if math.IsNaN(v) || i >= len(centers) {
			flush()
			continue
		}
		run = append(run, num(centers[i])+","+num(yPixel(g, lo, hi, v)))
	}
	flush()
}

// writeAreaFill shades the region between a series and the zero baseline (valueRange pins zero into
// the area's range) as one polygon per NaN-free run, so a missing value reads as a hole exactly like
// the line it underlays. The stroke itself is drawn by writePolyline on top.
func writeAreaFill(b *strings.Builder, g svgGeom, centers, vals []float64, lo, hi float64, color string) {
	baseY := num(yPixel(g, lo, hi, 0))
	var run []string
	var xs []string // the run's x coordinates, to close the polygon along the baseline
	flush := func() {
		if len(run) >= 2 {
			closing := xs[len(xs)-1] + "," + baseY + " " + xs[0] + "," + baseY
			fmt.Fprintf(b, `<polygon points="%s %s" fill="%s" fill-opacity="0.3"/>`+"\n",
				strings.Join(run, " "), closing, color)
		}
		run, xs = run[:0], xs[:0]
	}
	for i, v := range vals {
		if math.IsNaN(v) || i >= len(centers) {
			flush()
			continue
		}
		x := num(centers[i])
		run = append(run, x+","+num(yPixel(g, lo, hi, v)))
		xs = append(xs, x)
	}
	flush()
}

// candleUp/candleDown color a rising (close >= open) and falling candle; they reuse the shared
// palette's green and red so candlesticks match the rest of the chart family.
const (
	candleUp   = "#59a14f"
	candleDown = "#e15759"
)

// writeCandles draws one OHLC candle per category: a high–low wick behind an open–close body, green
// when the close is at or above the open and red otherwise. The resolved series come in the fixed
// open/high/low/close order (viewspec.CandleSeries); a candle missing any component is skipped.
func writeCandles(b *strings.Builder, g svgGeom, res viewspec.Resolved, centers []float64, lo, hi float64) {
	if len(res.Series) != 4 || len(centers) == 0 {
		return
	}
	open, high, low, close := res.Series[0].Values, res.Series[1].Values, res.Series[2].Values, res.Series[3].Values
	band := g.plotW() / float64(len(centers))
	bw := band * 0.6
	for i := range centers {
		if i >= len(open) || i >= len(high) || i >= len(low) || i >= len(close) {
			continue
		}
		o, h, l, c := open[i], high[i], low[i], close[i]
		if math.IsNaN(o) || math.IsNaN(h) || math.IsNaN(l) || math.IsNaN(c) {
			continue
		}
		color := candleUp
		if c < o {
			color = candleDown
		}
		// Wick: the full high–low span.
		fmt.Fprintf(b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="1"/>`+"\n",
			num(centers[i]), num(yPixel(g, lo, hi, h)), num(centers[i]), num(yPixel(g, lo, hi, l)), color)
		// Body: the open–close span, kept at least 1px tall so a doji (open == close) stays visible.
		yo, yc := yPixel(g, lo, hi, o), yPixel(g, lo, hi, c)
		top, hgt := math.Min(yo, yc), math.Max(math.Abs(yo-yc), 1)
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
			num(centers[i]-bw/2), num(top), num(bw), num(hgt), color)
	}
}

// writeBars draws grouped vertical bars: each category band is split evenly across the series so
// multiple series sit side by side. Stacked bars instead pile the series onto one full-width bar per
// category: positives grow up from zero, negatives grow down, each segment starting where the
// previous series ended.
func writeBars(b *strings.Builder, g svgGeom, res viewspec.Resolved, centers []float64, lo, hi float64) {
	// Only the bar-form series draw here (in a combo chart the rest are lines/areas), and the band
	// slots are split among them alone so bars keep their width next to overlaid lines.
	var bars []int
	for si := range res.Series {
		if res.SeriesForm(si) == viewspec.ChartBar {
			bars = append(bars, si)
		}
	}
	n := len(bars)
	if n == 0 || len(centers) == 0 {
		return
	}
	band := g.plotW() / float64(len(centers))
	if res.Stacked {
		bw := band * 0.6
		pos := make([]float64, len(centers))
		neg := make([]float64, len(centers))
		for _, si := range bars {
			for i, v := range res.Series[si].Values {
				if math.IsNaN(v) || math.IsInf(v, 0) || i >= len(centers) {
					continue
				}
				base := &pos[i]
				if v < 0 {
					base = &neg[i]
				}
				y0, y1 := yPixel(g, lo, hi, *base), yPixel(g, lo, hi, *base+v)
				*base += v
				fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
					num(centers[i]-bw/2), num(math.Min(y0, y1)), num(bw), num(math.Abs(y0-y1)), seriesColor(si))
			}
		}
		return
	}
	bw := band * 0.8 / float64(n) // 20% inter-band gap, split across the bar series
	baseY := yPixel(g, lo, hi, math.Max(lo, 0))
	for slot, si := range bars {
		for i, v := range res.Series[si].Values {
			if math.IsNaN(v) || i >= len(centers) {
				continue
			}
			x := centers[i] - band*0.4 + bw*float64(slot)
			y := yPixel(g, lo, hi, v)
			top, h := math.Min(y, baseY), math.Abs(baseY-y)
			fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
				num(x), num(top), num(bw), num(h), seriesColor(si))
		}
	}
}

// bandCentersVertical returns the y pixel at the center of each category slot down the plot height,
// used by hbar where categories run along the vertical axis.
func bandCentersVertical(g svgGeom, n int) []float64 {
	cs := make([]float64, n)
	if n == 0 {
		return cs
	}
	band := g.plotH() / float64(n)
	for i := range cs {
		cs[i] = g.top + band*(float64(i)+0.5)
	}
	return cs
}

// writeHBars draws horizontal bars: categories along the y axis, value along the x axis. Stacked
// horizontal bars pile the series onto one bar per category (positives grow right, negatives left),
// mirroring writeBars with the axes transposed.
func writeHBars(b *strings.Builder, g svgGeom, res viewspec.Resolved, lo, hi float64) {
	centers := bandCentersVertical(g, len(res.Labels))
	n := len(res.Series)
	if n == 0 || len(centers) == 0 {
		return
	}
	band := g.plotH() / float64(len(centers))
	if res.Stacked {
		bh := band * 0.6
		pos := make([]float64, len(centers))
		neg := make([]float64, len(centers))
		for si, s := range res.Series {
			for i, v := range s.Values {
				if math.IsNaN(v) || math.IsInf(v, 0) || i >= len(centers) {
					continue
				}
				base := &pos[i]
				if v < 0 {
					base = &neg[i]
				}
				x0, x1 := xPixel(g, lo, hi, *base), xPixel(g, lo, hi, *base+v)
				*base += v
				fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
					num(math.Min(x0, x1)), num(centers[i]-bh/2), num(math.Abs(x0-x1)), num(bh), seriesColor(si))
			}
		}
		return
	}
	bh := band * 0.8 / float64(n)
	baseX := xPixel(g, lo, hi, math.Max(lo, 0))
	for si, s := range res.Series {
		for i, v := range s.Values {
			if math.IsNaN(v) || i >= len(centers) {
				continue
			}
			y := centers[i] - band*0.4 + bh*float64(si)
			x := xPixel(g, lo, hi, v)
			left, w := math.Min(x, baseX), math.Abs(x-baseX)
			fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
				num(left), num(y), num(w), num(bh), seriesColor(si))
		}
	}
}

// xPixel maps a value to its horizontal pixel position (left of plot = lo, right = hi). Used by hbar.
func xPixel(g svgGeom, lo, hi, v float64) float64 {
	return g.left + g.plotW()*((v-lo)/(hi-lo))
}

// writeMarkers draws overlay markers as vertical lines at the category whose label matches the
// marker's At value, mirroring the ECharts markLine overlays for the static renderer.
func writeMarkers(b *strings.Builder, g svgGeom, res viewspec.Resolved) {
	if len(res.Markers) == 0 {
		return
	}
	idx := make(map[string]int, len(res.Labels))
	for i, l := range res.Labels {
		idx[l] = i
	}
	centers := bandCenters(g, len(res.Labels))
	for _, m := range res.Markers {
		i, ok := idx[m.At]
		if !ok || i >= len(centers) {
			continue
		}
		x := centers[i]
		// A marker carrying a source URL becomes a real link — SVG anchors work in every host. Note
		// references stay non-links here: the static renderer has no router to resolve them.
		if m.Href != "" {
			fmt.Fprintf(b, `<a href="%s" target="_blank" rel="noopener">`+"\n", html.EscapeString(m.Href))
		}
		fmt.Fprintf(b, `<line x1="%s" y1="%g" x2="%s" y2="%g" stroke="rgba(220,53,69,0.7)" stroke-width="1"/>`+"\n",
			num(x), g.top, num(x), g.top+g.plotH())
		if m.Label != "" {
			fmt.Fprintf(b, `<text x="%s" y="%g" font-size="10" fill="#dc3545" transform="rotate(90 %s %g)">%s</text>`+"\n",
				num(x+3), g.top+4, num(x+3), g.top+4, html.EscapeString(m.Label))
		}
		if m.Href != "" {
			b.WriteString("</a>\n")
		}
	}
}

// writeBands shades each band overlay's x range: a translucent rectangle spanning the full plot
// height from the left edge of the From category's slot to the right edge of the To category's slot
// (inclusive), drawn before the series so data stays on top. A band whose From or To matches no
// category label is skipped.
func writeBands(b *strings.Builder, g svgGeom, res viewspec.Resolved) {
	if len(res.Bands) == 0 || len(res.Labels) == 0 {
		return
	}
	idx := make(map[string]int, len(res.Labels))
	for i, l := range res.Labels {
		idx[l] = i
	}
	band := g.plotW() / float64(len(res.Labels))
	for _, bd := range res.Bands {
		i, ok := idx[bd.From]
		j, ok2 := idx[bd.To]
		if !ok || !ok2 {
			continue
		}
		if j < i {
			i, j = j, i
		}
		x := g.left + band*float64(i)
		w := band * float64(j-i+1)
		fmt.Fprintf(b, `<rect x="%s" y="%g" width="%s" height="%g" fill="#6c757d" fill-opacity="0.15"/>`+"\n",
			num(x), g.top, num(w), g.plotH())
		if bd.Label != "" {
			fmt.Fprintf(b, `<text x="%s" y="%g" font-size="10" fill="#6c757d">%s</text>`+"\n",
				num(x+4), g.top+12, html.EscapeString(bd.Label))
		}
	}
}

// writeRefLines draws each reference-line overlay as a dashed horizontal line at its y value, labeled
// at the right edge, mirroring the ECharts reference lines. The SVG renderer has a single value
// scale, so the line's axis choice (y/y2) is ignored here.
// ponytail: a line outside the data's value range is skipped, not drawn; expand valueRange over
// res.Lines if off-scale thresholds need to show.
func writeRefLines(b *strings.Builder, g svgGeom, res viewspec.Resolved, lo, hi float64) {
	for _, l := range res.Lines {
		if l.Y < lo || l.Y > hi {
			continue
		}
		y := yPixel(g, lo, hi, l.Y)
		fmt.Fprintf(b, `<line x1="%g" y1="%s" x2="%g" y2="%s" stroke="rgba(220,53,69,0.7)" stroke-width="1" stroke-dasharray="4 4"/>`+"\n",
			g.left, num(y), g.left+g.plotW(), num(y))
		if l.Label != "" {
			fmt.Fprintf(b, `<text x="%g" y="%s" font-size="10" text-anchor="end" fill="#dc3545">%s</text>`+"\n",
				g.left+g.plotW()-4, num(y-4), html.EscapeString(l.Label))
		}
	}
}

// writeCallouts draws each callout overlay as a speech bubble: a dot on the point, a short leader
// line up, and the text in a bordered box. The box is kept inside the plot horizontally; a callout
// whose x matches no category label or whose y is off-scale is skipped, like the other overlays.
func writeCallouts(b *strings.Builder, g svgGeom, res viewspec.Resolved, lo, hi float64) {
	if len(res.Callouts) == 0 || len(res.Labels) == 0 {
		return
	}
	idx := make(map[string]int, len(res.Labels))
	for i, l := range res.Labels {
		idx[l] = i
	}
	centers := bandCenters(g, len(res.Labels))
	for _, c := range res.Callouts {
		i, ok := idx[c.X]
		if !ok || c.Y < lo || c.Y > hi {
			continue
		}
		x, y := centers[i], yPixel(g, lo, hi, c.Y)
		// The point and its leader up to the bubble.
		fmt.Fprintf(b, `<circle cx="%s" cy="%s" r="3.5" fill="rgba(220,53,69,0.9)"/>`+"\n", num(x), num(y))
		boxW := calloutTextWidth(c.Label) + 12
		boxH := 18.0
		boxY := y - 14 - boxH
		if boxY < g.top {
			boxY = g.top // keep the bubble inside the plot; the leader just gets shorter
		}
		boxX := x - boxW/2
		if boxX < g.left {
			boxX = g.left
		}
		if boxX+boxW > g.left+g.plotW() {
			boxX = g.left + g.plotW() - boxW
		}
		fmt.Fprintf(b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="rgba(220,53,69,0.9)" stroke-width="1"/>`+"\n",
			num(x), num(y-3.5), num(x), num(boxY+boxH))
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" rx="3" fill="#ffffff" stroke="rgba(220,53,69,0.9)"/>`+"\n",
			num(boxX), num(boxY), num(boxW), num(boxH))
		fmt.Fprintf(b, `<text x="%s" y="%s" font-size="11" text-anchor="middle" fill="#333333">%s</text>`+"\n",
			num(boxX+boxW/2), num(boxY+boxH-5), html.EscapeString(c.Label))
	}
}

// calloutTextWidth estimates the pixel width of bubble text at font-size 11: narrow for ASCII, wide
// for CJK and other full-width runes.
// ponytail: heuristic glyph widths; measure real text metrics if bubbles start clipping
func calloutTextWidth(s string) float64 {
	w := 0.0
	for _, r := range s {
		if r < 0x2E80 { // Latin, digits, punctuation
			w += 6.5
		} else { // CJK and other full-width scripts
			w += 11.5
		}
	}
	return w
}

// writeLegend lists the series labels with their color swatches in the top-right of the plot.
func writeLegend(b *strings.Builder, g svgGeom, res viewspec.Resolved) {
	x := g.left + g.plotW() - 8
	y := g.top + 14
	for si, s := range res.Series {
		yi := y + float64(si)*16
		fmt.Fprintf(b, `<rect x="%g" y="%g" width="10" height="10" fill="%s"/>`+"\n", x-10, yi-9, seriesColor(si))
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="11" text-anchor="end" fill="#333333">%s</text>`+"\n",
			x-14, yi, html.EscapeString(s.Label))
	}
}

// renderBubble draws a bubble chart: {x,y,r} points on linear x and y axes (unlike the category-axis
// line/bar/scatter charts). Each y series is a color; a point missing x or y is skipped and a
// missing/non-positive radius falls back to a small default, mirroring the ECharts bubble renderer.
func renderBubble(res viewspec.Resolved) string {
	g := svgGeom{w: 800, h: 480, left: 56, right: 16, top: 40, bottom: 40}
	xlo, xhi, ylo, yhi := bubbleRange(res.Series)

	var b strings.Builder
	writeSVGHeader(&b, g, res.Spec.Title)

	// Plot border.
	fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g" fill="none" stroke="#cccccc"/>`+"\n",
		g.left, g.top, g.plotW(), g.plotH())
	// Both axes are linear here: vertical gridlines + x labels along the bottom, horizontal gridlines +
	// y labels on the left, at the same three fractions the category charts use.
	for _, frac := range []float64{0, 0.5, 1} {
		x := g.left + g.plotW()*frac
		fmt.Fprintf(&b, `<line x1="%s" y1="%g" x2="%s" y2="%g" stroke="#eeeeee"/>`+"\n",
			num(x), g.top, num(x), g.top+g.plotH())
		fmt.Fprintf(&b, `<text x="%s" y="%g" font-size="11" text-anchor="middle" fill="#666666">%s</text>`+"\n",
			num(x), g.top+g.plotH()+16, num(xlo+(xhi-xlo)*frac))
		y := g.top + g.plotH()*frac
		fmt.Fprintf(&b, `<line x1="%g" y1="%s" x2="%g" y2="%s" stroke="#eeeeee"/>`+"\n",
			g.left, num(y), g.left+g.plotW(), num(y))
		fmt.Fprintf(&b, `<text x="%g" y="%s" font-size="11" text-anchor="end" dominant-baseline="middle" fill="#666666">%s</text>`+"\n",
			g.left-6, num(y), num(yhi-(yhi-ylo)*frac))
	}

	for si, s := range res.Series {
		color := seriesColor(si)
		for _, p := range s.Points {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) {
				continue
			}
			r := p.R
			if math.IsNaN(r) || r <= 0 {
				r = 4
			}
			r = math.Min(r, 40) // keep a stray huge value from swamping the plot
			fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="%s" fill="%s" fill-opacity="0.6" stroke="%s"/>`+"\n",
				num(xPixel(g, xlo, xhi, p.X)), num(yPixel(g, ylo, yhi, p.Y)), num(r), color, color)
		}
	}
	writeLegend(&b, g, res)
	b.WriteString("</svg>\n")
	return b.String()
}

// bubbleRange spans the finite x and y coordinates across every bubble series, padding a degenerate
// (zero-width) range so the chart still has extent.
func bubbleRange(series []viewspec.Series) (xlo, xhi, ylo, yhi float64) {
	xlo, ylo = math.Inf(1), math.Inf(1)
	xhi, yhi = math.Inf(-1), math.Inf(-1)
	for _, s := range series {
		for _, p := range s.Points {
			if !math.IsNaN(p.X) {
				xlo, xhi = math.Min(xlo, p.X), math.Max(xhi, p.X)
			}
			if !math.IsNaN(p.Y) {
				ylo, yhi = math.Min(ylo, p.Y), math.Max(yhi, p.Y)
			}
		}
	}
	if math.IsInf(xlo, 1) {
		xlo, xhi = 0, 1
	}
	if math.IsInf(ylo, 1) {
		ylo, yhi = 0, 1
	}
	if xlo == xhi {
		xlo, xhi = xlo-1, xhi+1
	}
	if ylo == yhi {
		ylo, yhi = ylo-1, yhi+1
	}
	return xlo, xhi, ylo, yhi
}

// num formats a pixel/value coordinate to two decimals so the SVG stays compact and golden-file
// output is stable across platforms (Go's float formatting is deterministic at fixed precision).
func num(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
