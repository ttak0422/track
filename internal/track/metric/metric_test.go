package metric

import (
	"math"
	"testing"
)

// eq compares two series treating NaN as equal to NaN, so warm-up gaps can be asserted directly.
func eq(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if math.IsNaN(want[i]) {
			if !math.IsNaN(got[i]) {
				t.Fatalf("[%d] = %v, want NaN (%v)", i, got[i], got)
			}
			continue
		}
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("[%d] = %v, want %v (%v)", i, got[i], want[i], got)
		}
	}
}

func TestSMA(t *testing.T) {
	nan := math.NaN()
	// window 3: first two are warm-up NaN, then trailing means.
	eq(t, SMA([]float64{1, 2, 3, 4, 5}, 3), []float64{nan, nan, 2, 3, 4})
	// a NaN inside the window is skipped, averaging the remaining finite samples.
	eq(t, SMA([]float64{1, nan, 3, 4}, 3), []float64{nan, nan, 2, 3.5})
	// a window with no finite sample is NaN.
	eq(t, SMA([]float64{nan, nan, nan}, 2), []float64{nan, nan, nan})
	// window < 1 is all NaN.
	eq(t, SMA([]float64{1, 2}, 0), []float64{nan, nan})
}

func TestEMA(t *testing.T) {
	nan := math.NaN()
	// window 1 → alpha 1 → tracks the input exactly once seeded.
	eq(t, EMA([]float64{2, 4, 6}, 1), []float64{2, 4, 6})
	// window 3 → alpha 0.5; seeded at first sample.
	eq(t, EMA([]float64{1, 3, 5}, 3), []float64{1, 2, 3.5})
	// leading NaN delays the seed; a later NaN holds the prior average.
	eq(t, EMA([]float64{nan, 4, nan, 8}, 1), []float64{nan, 4, 4, 8})
}

func TestCumSum(t *testing.T) {
	nan := math.NaN()
	eq(t, CumSum([]float64{1, 2, 3}), []float64{1, 3, 6})
	// leading NaN stays NaN; later NaN is skipped but the total persists.
	eq(t, CumSum([]float64{nan, 2, nan, 3}), []float64{nan, 2, 2, 5})
}

func TestDiff(t *testing.T) {
	nan := math.NaN()
	eq(t, Diff([]float64{1, 3, 6, 10}), []float64{nan, 2, 3, 4})
	// either operand NaN → NaN.
	eq(t, Diff([]float64{1, nan, 3}), []float64{nan, nan, nan})
}
