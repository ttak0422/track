package metrics

import (
	"fmt"
	"sort"

	"github.com/ttak0422/track/internal/track/dataset"
)

// Gauges is the closed transform set derive computes (ADR 0076). Short histories start later
// rather than failing: rsi14 needs 15 bars, ma25_dev 25, and the 252-day gauges a full year.
var Gauges = []string{"change_pct", "ma5_dev", "ma25_dev", "rsi14", "mom_12_1", "high52w"}

type bar struct {
	time   string
	entity string
	open   float64
	high   float64
	low    float64
	close  float64
}

// Derive reads price-kind rows (any writer: track-prices today, a J-Quants writer tomorrow —
// derive never names a source) and emits metric-kind gauge records for the requested gauges.
func Derive(rows []dataset.Record, gauges []string) ([]dataset.Record, error) {
	if len(gauges) == 0 {
		gauges = Gauges
	}
	want := make(map[string]bool, len(gauges))
	for _, g := range gauges {
		known := false
		for _, k := range Gauges {
			if g == k {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown gauge %q (want one of %s)", g, joinStrings(Gauges, " "))
		}
		want[g] = true
	}
	bars := make([]bar, 0, len(rows))
	for i, r := range rows {
		t, ok := r.String("time")
		if !ok {
			return nil, fmt.Errorf("row %d: missing time", i)
		}
		c, ok := r.Float("close")
		if !ok {
			return nil, fmt.Errorf("row %d (%s): missing close", i, t)
		}
		b := bar{time: t, close: c}
		if e, ok := r.String("entity"); ok {
			b.entity = e
		}
		if want["high52w"] {
			h, ok := r.Float("high")
			if !ok {
				return nil, fmt.Errorf("row %d (%s): high52w needs high", i, t)
			}
			b.high = h
		}
		bars = append(bars, b)
	}
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].time < bars[j].time })
	closes := make([]float64, len(bars))
	highs := make([]float64, len(bars))
	for i, b := range bars {
		closes[i], highs[i] = b.close, b.high
	}
	emit := func(name string, b bar, v float64) dataset.Record {
		rec := dataset.Record{
			"version": dataset.SchemaVersion,
			"name":    name,
			"time":    b.time,
			"value":   round4(v),
		}
		if b.entity != "" {
			rec["entity"] = b.entity
		}
		return rec
	}
	var out []dataset.Record
	var ag, al float64 // Wilder averages
	for i, b := range bars {
		if want["change_pct"] && i > 0 && closes[i-1] != 0 {
			out = append(out, emit("change_pct", b, (b.close/closes[i-1]-1)*100))
		}
		for _, nd := range []struct {
			n   int
			key string
		}{{5, "ma5_dev"}, {25, "ma25_dev"}} {
			if want[nd.key] && i+1 >= nd.n {
				sum := 0.0
				for _, c := range closes[i+1-nd.n : i+1] {
					sum += c
				}
				ma := sum / float64(nd.n)
				if ma != 0 {
					out = append(out, emit(nd.key, b, (b.close/ma-1)*100))
				}
			}
		}
		if want["rsi14"] && i > 0 {
			chg := b.close - closes[i-1]
			g, l := chg, 0.0
			if chg < 0 {
				g, l = 0.0, -chg
			}
			switch {
			case i == 14:
				gs, ls := 0.0, 0.0
				for j := 1; j <= 14; j++ {
					d := closes[j] - closes[j-1]
					if d > 0 {
						gs += d
					} else {
						ls -= d
					}
				}
				ag, al = gs/14, ls/14
			case i > 14:
				ag, al = (ag*13+g)/14, (al*13+l)/14
			}
			if i >= 14 {
				if al == 0 {
					out = append(out, emit("rsi14", b, 100))
				} else {
					out = append(out, emit("rsi14", b, 100-100/(1+ag/al)))
				}
			}
		}
		if want["mom_12_1"] && i >= 252 && closes[i-252] != 0 {
			out = append(out, emit("mom_12_1", b, (closes[i-21]/closes[i-252]-1)*100))
		}
		if want["high52w"] && i+1 >= 252 {
			mx := highs[i+1-252]
			for _, h := range highs[i+1-252 : i+1] {
				if h > mx {
					mx = h
				}
			}
			if mx != 0 {
				out = append(out, emit("high52w", b, b.close/mx))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ti, _ := out[i].String("time")
		tj, _ := out[j].String("time")
		if ti != tj {
			return ti < tj
		}
		ni, _ := out[i].String("name")
		nj, _ := out[j].String("name")
		return ni < nj
	})
	if err := dataset.ValidateRecords(dataset.KindMetric, out); err != nil {
		return nil, err
	}
	return out, nil
}

func round4(v float64) float64 {
	if v >= 0 {
		return float64(int(v*10000+0.5)) / 10000
	}
	return float64(int(v*10000-0.5)) / 10000
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
