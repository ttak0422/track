package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/ttak0422/track/internal/track/dataset"
)

// ScrapeOptions tunes exposition ingest. EntityLabels names the label folded into the record's
// entity field, tried in order; every other label folds into the name (ADR 0076). Stamp is the time
// attached to unstamped samples — scrape time when zero.
type ScrapeOptions struct {
	EntityLabels []string
	Stamp        time.Time
}

// DefaultEntityLabels is the label search order for the entity field.
func DefaultEntityLabels() []string { return []string{"entity", "symbol", "instance"} }

// Scrape parses OpenMetrics / Prometheus exposition text with the ecosystem's own parser and maps
// every sample onto a metric-kind record. Gauges and untyped keep the family name (info and
// stateset families arrive as untyped from the parser, payload in labels); counters keep the
// `_total` sample name; histogram families flatten to `_bucket`/`_sum`/`_count`
// (`_gsum`/`_gcount` for gauge histograms); summaries flatten to the bare name (carrying the
// `quantile` label) plus `_sum`/`_count`. Non-finite values cannot travel in JSON and are skipped.
// HELP/UNIT metadata has no field in the model and is dropped.
func Scrape(r io.Reader, opt ScrapeOptions) ([]dataset.Record, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(r)
	if err != nil {
		return nil, fmt.Errorf("parse exposition: %w", err)
	}
	if opt.EntityLabels == nil {
		opt.EntityLabels = DefaultEntityLabels()
	}
	stamp := opt.Stamp
	if stamp.IsZero() {
		stamp = time.Now().UTC()
	}
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []dataset.Record
	for _, name := range names {
		mf := families[name]
		for _, m := range mf.GetMetric() {
			labels := make(map[string]string, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			entity, entityKey := "", ""
			for _, k := range opt.EntityLabels {
				if v, ok := labels[k]; ok {
					entity, entityKey = v, k
					break
				}
			}
			rest := make(map[string]string, len(labels))
			for k, v := range labels {
				if k != entityKey {
					rest[k] = v
				}
			}
			ts := stamp
			if m.TimestampMs != nil {
				ts = time.UnixMilli(m.GetTimestampMs()).UTC()
			}
			emit := func(sample string, extra map[string]string, value float64) {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return
				}
				merged := make(map[string]string, len(rest)+len(extra))
				for k, v := range rest {
					merged[k] = v
				}
				for k, v := range extra {
					merged[k] = v
				}
				rec := dataset.Record{
					"version": dataset.SchemaVersion,
					"name":    Fold(sample, merged),
					"time":    ts.Format(time.RFC3339),
					"value":   value,
				}
				if entity != "" {
					rec["entity"] = entity
				}
				out = append(out, rec)
			}
			switch mf.GetType() {
			case dto.MetricType_GAUGE, dto.MetricType_UNTYPED:
				emit(name, nil, gaugeLikeValue(m, mf.GetType()))
			case dto.MetricType_COUNTER:
				emit(name+"_total", nil, m.GetCounter().GetValue())
			case dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
				h := m.GetHistogram()
				sumName, countName := name+"_sum", name+"_count"
				if mf.GetType() == dto.MetricType_GAUGE_HISTOGRAM {
					sumName, countName = name+"_gsum", name+"_gcount"
				}
				for _, b := range h.GetBucket() {
					emit(name+"_bucket", map[string]string{"le": boundString(b.GetUpperBound())}, float64(b.GetCumulativeCount()))
				}
				emit(sumName, nil, h.GetSampleSum())
				emit(countName, nil, float64(h.GetSampleCount()))
			case dto.MetricType_SUMMARY:
				s := m.GetSummary()
				for _, q := range s.GetQuantile() {
					emit(name, map[string]string{"quantile": strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64)}, q.GetValue())
				}
				emit(name+"_sum", nil, s.GetSampleSum())
				emit(name+"_count", nil, float64(s.GetSampleCount()))
			default:
				return nil, fmt.Errorf("unsupported metric type %q on %q", mf.GetType(), name)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ni, _ := out[i].String("name")
		nj, _ := out[j].String("name")
		if ni != nj {
			return ni < nj
		}
		ei, _ := out[i].String("entity")
		ej, _ := out[j].String("entity")
		if ei != ej {
			return ei < ej
		}
		ti, _ := out[i].String("time")
		tj, _ := out[j].String("time")
		return ti < tj
	})
	if err := dataset.ValidateRecords(dataset.KindMetric, out); err != nil {
		return nil, err
	}
	return out, nil
}

// gaugeLikeValue reads the single value of a gauge-shaped sample. Info and stateset families
// arrive from the parser as untyped carrying value 1 with the payload in labels, which fold into
// the name.
func gaugeLikeValue(m *dto.Metric, t dto.MetricType) float64 {
	if t == dto.MetricType_UNTYPED {
		return m.GetUntyped().GetValue()
	}
	return m.GetGauge().GetValue()
}

// boundString renders a histogram upper bound the way Prometheus text does: +Inf stays symbolic.
func boundString(f float64) string {
	if math.IsInf(f, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// NormalizeAsof validates an --asof flag (a date or RFC3339) and returns the stamp to attach to
// unstamped samples: dates stay bare dates so daily series keep their shape.
func NormalizeAsof(s string) (time.Time, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		now := time.Now().UTC()
		return now, now.Format(time.RFC3339), nil
	}
	if len(s) == len("2006-01-02") {
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return time.Time{}, "", fmt.Errorf("bad --asof %q (want YYYY-MM-DD or RFC3339)", s)
		}
		t, _ := time.Parse("2006-01-02", s)
		return t, s, nil
	}
	t, err := ParseTime(s)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("bad --asof %q (want YYYY-MM-DD or RFC3339)", s)
	}
	return t, t.UTC().Format(time.RFC3339), nil
}
