package render

import (
	"fmt"
	"math"

	"github.com/ttak0422/track/internal/track/viewspec"
)

// validateAxisDomains prevents an explicit domain from silently clipping data or narrative
// overlays. Authors can widen the range or omit it and use the renderer's automatic scale.
func validateAxisDomains(res viewspec.Resolved) error {
	for _, axis := range []string{"y", "y2"} {
		lo, hi, ok := res.AxisDomain(axis)
		if !ok {
			continue
		}
		for _, s := range res.Series {
			sAxis := s.Axis
			if sAxis == "" {
				sAxis = "y"
			}
			if sAxis != axis {
				continue
			}
			for _, v := range s.Values {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					continue
				}
				if v < lo || v > hi {
					return fmt.Errorf("render: value %g on axis %s falls outside explicit domain [%g,%g]", v, axis, lo, hi)
				}
			}
		}
		for _, line := range res.Lines {
			lineAxis := line.Axis
			if lineAxis == "" {
				lineAxis = "y"
			}
			if lineAxis == axis && (line.Y < lo || line.Y > hi) {
				return fmt.Errorf("render: reference line %g on axis %s falls outside explicit domain [%g,%g]", line.Y, axis, lo, hi)
			}
		}
		for _, callout := range res.Callouts {
			if axis == "y" && (callout.Y < lo || callout.Y > hi) {
				return fmt.Errorf("render: callout %g on axis y falls outside explicit domain [%g,%g]", callout.Y, lo, hi)
			}
		}
	}
	return nil
}
