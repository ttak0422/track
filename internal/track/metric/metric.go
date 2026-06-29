// Package metric computes derived time-series values over an ordered slice of samples: moving
// averages, cumulative sums, and first differences. The functions are pure ([]float64 → []float64)
// and renderer-independent, so the View Spec layer and any future data tooling can reuse them.
//
// All functions honor track's "NaN is a gap" convention: a NaN input is a missing sample rather than
// a zero, and positions that cannot be computed yet (a not-yet-filled window, the first difference,
// the run before the first finite value) are returned as NaN so a renderer draws a hole, not a false
// value.
package metric

import "math"

// SMA returns the simple moving average over a trailing window of size window. Position i averages the
// finite samples in v[i-window+1 .. i]; the leading window-1 positions are NaN (warm-up), NaN samples
// inside a window are skipped, and a window with no finite sample yields NaN. A window < 1 returns all
// NaN, since an average over no samples is undefined.
func SMA(v []float64, window int) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		if window < 1 || i < window-1 {
			out[i] = math.NaN()
			continue
		}
		sum, n := 0.0, 0
		for j := i - window + 1; j <= i; j++ {
			if !math.IsNaN(v[j]) && !math.IsInf(v[j], 0) {
				sum += v[j]
				n++
			}
		}
		if n == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = sum / float64(n)
		}
	}
	return out
}

// EMA returns the exponential moving average with smoothing factor alpha = 2/(window+1). The average
// is seeded at the first finite sample (earlier positions are NaN); a later NaN sample carries the
// previous average forward rather than resetting it. A window < 1 returns all NaN.
func EMA(v []float64, window int) []float64 {
	out := make([]float64, len(v))
	if window < 1 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	alpha := 2.0 / float64(window+1)
	ema := math.NaN()
	for i, x := range v {
		switch {
		case math.IsNaN(x) || math.IsInf(x, 0):
			// missing sample: hold the running average (NaN until the first finite sample)
		case math.IsNaN(ema):
			ema = x // seed
		default:
			ema = alpha*x + (1-alpha)*ema
		}
		out[i] = ema
	}
	return out
}

// CumSum returns the running total. NaN samples are skipped (they contribute nothing but do not break
// the total); positions before the first finite sample are NaN, since nothing has accumulated yet.
func CumSum(v []float64) []float64 {
	out := make([]float64, len(v))
	sum := math.NaN()
	for i, x := range v {
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			if math.IsNaN(sum) {
				sum = 0
			}
			sum += x
		}
		out[i] = sum
	}
	return out
}

// Diff returns the first difference v[i]-v[i-1]. Position 0, and any position where either operand is
// NaN, is NaN.
func Diff(v []float64) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		if i == 0 || math.IsNaN(v[i]) || math.IsNaN(v[i-1]) || math.IsInf(v[i], 0) || math.IsInf(v[i-1], 0) {
			out[i] = math.NaN()
			continue
		}
		out[i] = v[i] - v[i-1]
	}
	return out
}
