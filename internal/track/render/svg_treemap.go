package render

import (
	"fmt"
	"html"
	"math"
	"slices"
	"strings"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// renderTreemap draws the treemap form onto the SVG canvas: area-proportional leaf rectangles
// colored by their value, squarified inside the plot — or, with a group channel, inside per-group
// regions that are themselves squarified by their summed size and carry a heading band. Groups keep
// first-seen order for ties; sizes sort descending (stable) before layout, the classic squarify
// order that keeps rectangles near-square. A node without a positive size or a finite value is
// skipped (no area cannot be drawn; no value has no color).
func renderTreemap(res viewspec.Resolved) string {
	g := svgGeom{w: 800, h: 480, left: 16, right: 16, top: 40, bottom: 16}
	var b strings.Builder
	writeSVGHeader(&b, g, res.Spec.Title)

	var nodes []viewspec.TreeNode
	lo, hi := math.Inf(1), math.Inf(-1)
	if res.Tree != nil {
		for _, n := range res.Tree.Nodes {
			if math.IsNaN(n.Size) || n.Size <= 0 || math.IsNaN(n.Value) {
				continue
			}
			lo, hi = math.Min(lo, n.Value), math.Max(hi, n.Value)
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		b.WriteString("</svg>\n")
		return b.String()
	}
	if math.IsInf(lo, 1) {
		lo, hi = 0, 1
	}
	if res.DivergingColor() {
		lo, hi = divergingRange(lo, hi)
	} else if lo == hi {
		hi = lo + 1
	}
	ramp := rampFor(res)
	color := func(v float64) string { return ramp((v - lo) / (hi - lo)) }

	plot := rectF{g.left, g.top, g.plotW(), g.plotH()}
	groups, grouped := treeGroups(nodes)
	if !grouped {
		writeTreeLeaves(&b, plot, nodes, color)
		b.WriteString("</svg>\n")
		return b.String()
	}
	sums := make([]float64, len(groups))
	for i, gr := range groups {
		for _, n := range gr.nodes {
			sums[i] += n.Size
		}
	}
	order := sortedByWeightDesc(sums)
	rects := squarify(permuteFloats64(sums, order), plot)
	for oi, gi := range order {
		r, gr := rects[oi], groups[gi]
		if r.w <= 0 || r.h <= 0 {
			continue
		}
		// Heading band: the group name over its region, like an upper label.
		band := math.Min(16, r.h)
		fmt.Fprintf(&b, `<rect x="%s" y="%s" width="%s" height="%s" fill="#e6e6e6"/>`+"\n",
			num(r.x), num(r.y), num(r.w), num(band))
		if calloutTextWidth(gr.name)+6 <= r.w && band >= 12 {
			fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="11" fill="#333333">%s</text>`+"\n",
				num(r.x+4), num(r.y+band-4), html.EscapeString(gr.name))
		}
		writeTreeLeaves(&b, rectF{r.x, r.y + band, r.w, r.h - band}, gr.nodes, color)
		// Group frame on top, so regions read as units.
		fmt.Fprintf(&b, `<rect x="%s" y="%s" width="%s" height="%s" fill="none" stroke="#999999"/>`+"\n",
			num(r.x), num(r.y), num(r.w), num(r.h))
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// treeGroup is one group's accumulated leaves, in record order.
type treeGroup struct {
	name  string
	nodes []viewspec.TreeNode
}

// treeGroups buckets nodes by their group in first-seen order; grouped is false when every node is
// groupless (a flat treemap).
func treeGroups(nodes []viewspec.TreeNode) (groups []treeGroup, grouped bool) {
	idx := map[string]int{}
	for _, n := range nodes {
		if n.Group != "" {
			grouped = true
		}
		i, ok := idx[n.Group]
		if !ok {
			i = len(groups)
			idx[n.Group] = i
			groups = append(groups, treeGroup{name: n.Group})
		}
		groups[i].nodes = append(groups[i].nodes, n)
	}
	return groups, grouped
}

// writeTreeLeaves squarifies the leaves (sorted by size descending, stable) into r and draws each as
// a filled rectangle, labelled when the text fits its cell.
func writeTreeLeaves(b *strings.Builder, r rectF, nodes []viewspec.TreeNode, color func(float64) string) {
	if r.w <= 0 || r.h <= 0 || len(nodes) == 0 {
		return
	}
	sizes := make([]float64, len(nodes))
	for i, n := range nodes {
		sizes[i] = n.Size
	}
	order := sortedByWeightDesc(sizes)
	rects := squarify(permuteFloats64(sizes, order), r)
	for oi, ni := range order {
		cell, n := rects[oi], nodes[ni]
		if cell.w <= 0 || cell.h <= 0 {
			continue
		}
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s" stroke="#ffffff"/>`+"\n",
			num(cell.x), num(cell.y), num(cell.w), num(cell.h), color(n.Value))
		// Label only when it fits; a cramped cell stays a silent color patch (the tooltip role
		// belongs to the interactive renderer).
		if calloutTextWidth(n.Label)+4 <= cell.w && cell.h >= 14 {
			fmt.Fprintf(b, `<text x="%s" y="%s" font-size="11" text-anchor="middle" dominant-baseline="middle" fill="#333333">%s</text>`+"\n",
				num(cell.x+cell.w/2), num(cell.y+cell.h/2), html.EscapeString(n.Label))
		}
	}
}

// rectF is an axis-aligned layout rectangle in canvas pixels.
type rectF struct{ x, y, w, h float64 }

// squarify lays positive weights into r as a squarified treemap (Bruls et al.): rows accumulate
// along the free rectangle's short side while the row's worst aspect ratio keeps improving, then the
// row is laid out and the free rectangle shrinks. One sub-rectangle per weight, in input order;
// the layout is fully deterministic.
func squarify(weights []float64, r rectF) []rectF {
	out := make([]rectF, len(weights))
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total <= 0 || r.w <= 0 || r.h <= 0 {
		return out
	}
	scale := r.w * r.h / total
	areas := make([]float64, len(weights))
	for i, w := range weights {
		areas[i] = w * scale
	}
	free := r
	i := 0
	for i < len(areas) {
		short := math.Min(free.w, free.h)
		if short <= 0 { // float drift consumed the free area; remaining rects stay zero-sized
			break
		}
		// Grow the row while the next area doesn't worsen its worst aspect ratio.
		end, rowSum := i+1, areas[i]
		worst := worstAspect(areas[i:i+1], rowSum, short)
		for end < len(areas) {
			next := worstAspect(areas[i:end+1], rowSum+areas[end], short)
			if next > worst {
				break
			}
			rowSum += areas[end]
			worst = next
			end++
		}
		thick := rowSum / short
		off := 0.0
		for j := i; j < end; j++ {
			l := areas[j] / thick
			if free.w <= free.h { // row across the top of the free rectangle
				out[j] = rectF{free.x + off, free.y, l, thick}
			} else { // column down its left
				out[j] = rectF{free.x, free.y + off, thick, l}
			}
			off += l
		}
		if free.w <= free.h {
			free.y += thick
			free.h -= thick
		} else {
			free.x += thick
			free.w -= thick
		}
		i = end
	}
	return out
}

// worstAspect is the worst (largest) aspect ratio in a row of areas laid along a side of the given
// length with total row area sum.
func worstAspect(row []float64, sum, short float64) float64 {
	thick := sum / short
	worst := 0.0
	for _, a := range row {
		l := a / thick
		worst = math.Max(worst, math.Max(l/thick, thick/l))
	}
	return worst
}

// sortedByWeightDesc returns the indices of weights ordered largest-first; the sort is stable, so
// ties keep first-seen order and the layout stays deterministic.
func sortedByWeightDesc(weights []float64) []int {
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return compareFloatsDesc(weights[a], weights[b]) })
	return order
}

// compareFloatsDesc orders two float64s largest-first.
func compareFloatsDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

// permuteFloats64 returns vs reordered so element i comes from vs[idx[i]].
func permuteFloats64(vs []float64, idx []int) []float64 {
	out := make([]float64, len(idx))
	for i, j := range idx {
		out[i] = vs[j]
	}
	return out
}

// divergingRange centers a value range symmetrically on zero — the diverging scale's domain, so an
// unchanged value always sits on the neutral midpoint.
func divergingRange(lo, hi float64) (float64, float64) {
	m := math.Max(math.Abs(lo), math.Abs(hi))
	if m == 0 { // all-zero values: keep a non-degenerate domain
		m = 1
	}
	return -m, m
}

// rampFor picks the normalized color ramp for a quantitative cell value: the sequential light→dark
// blues by default, or the market red→neutral→green when the color channel asks for the diverging
// scale.
func rampFor(res viewspec.Resolved) func(float64) string {
	if res.DivergingColor() {
		return divergeColor
	}
	return heatColor
}

// divergeColor interpolates the market diverging ramp for a normalized t in [0,1]: 0 = candleDown
// (most negative), 0.5 = the neutral midpoint, 1 = candleUp (most positive) — the same three stops
// the ECharts renderer hands its visualMap, so both outputs read alike.
func divergeColor(t float64) string {
	t = math.Max(0, math.Min(1, t))
	if t < 0.5 {
		u := t * 2 // candleDown #e15759 → divergeNeutral #f5f5f5
		return fmt.Sprintf("#%02x%02x%02x", lerp8(0xe1, 0xf5, u), lerp8(0x57, 0xf5, u), lerp8(0x59, 0xf5, u))
	}
	u := t*2 - 1 // divergeNeutral #f5f5f5 → candleUp #59a14f
	return fmt.Sprintf("#%02x%02x%02x", lerp8(0xf5, 0x59, u), lerp8(0xf5, 0xa1, u), lerp8(0xf5, 0x4f, u))
}
