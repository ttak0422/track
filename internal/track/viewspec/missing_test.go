package viewspec

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/dataset"
)

func missingRecords(t *testing.T) []dataset.Record {
	t.Helper()
	records, err := dataset.ReadJSONL(strings.NewReader(`{"name":"demo","time":"d1","value":-3,"url":"https://example.com/one"}
{"name":"demo","time":"d2","value":null,"missing_reason":"not published","url":"https://example.com/two"}
{"name":"demo","time":"d3","value":0,"url":"https://example.com/three"}
{"name":"demo","time":"d4","value":4,"url":"https://example.com/four"}
{"name":"demo","time":"d5","value":null,"missing_reason":"fetch failed"}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := dataset.ValidateRecords(dataset.KindMetric, records); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestMissingMetricGapExtrasAndWindow(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"metrics.jsonl","kind":"metric"},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}],"detail":[{"field":"missing_reason","title":"Missing"}],"href":{"field":"url"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	res := s.Resolve(missingRecords(t))
	if got := res.Series[0].Values; len(got) != 5 || got[0] != -3 || !math.IsNaN(got[1]) || got[2] != 0 || got[3] != 4 || !math.IsNaN(got[4]) {
		t.Fatalf("values = %v", got)
	}
	ex := res.Series[0].Extras[1]
	if ex.Href != "https://example.com/two" || len(ex.Detail) != 1 || ex.Detail[0].Value != "not published" {
		t.Fatalf("gap lost provenance: %+v", ex)
	}
	s.Encoding.Y[0].Window = 2
	res = s.Resolve(missingRecords(t))
	for i, v := range res.Series[0].Values {
		if i == 3 {
			if v != 2 {
				t.Fatalf("observed window = %v", v)
			}
		} else if !math.IsNaN(v) {
			t.Fatalf("window %d interpolated missing as %v", i, v)
		}
	}
}

func TestMissingMetricSortsLastBeforeLimit(t *testing.T) {
	for _, sort := range []string{"value", "-value"} {
		s, err := Load(strings.NewReader(`{"version":2,"mark":"bar","data":{"source":"metrics.jsonl","kind":"metric"},"encoding":{"x":{"field":"value"},"y":[{"field":"time","type":"nominal","sort":"` + sort + `"}],"href":{"field":"url"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"d1", "d3", "d4", "d2", "d5"}
		if sort == "-value" {
			want = []string{"d4", "d3", "d1", "d2", "d5"}
		}
		res := s.Resolve(missingRecords(t))
		if !equalStrings(res.Labels, want) {
			t.Fatalf("%s labels=%v", sort, res.Labels)
		}
		if res.Series[0].Extras[3].Href != "https://example.com/two" {
			t.Fatal("sort lost gap provenance")
		}
		s.Encoding.Y[0].Limit = 3
		res = s.Resolve(missingRecords(t))
		if !equalStrings(res.Labels, want[:3]) {
			t.Fatalf("%s top3=%v", sort, res.Labels)
		}
	}
}

func TestMissingMetricGaugeClearsAndRecovers(t *testing.T) {
	s := Spec{Version: Version, Mark: MarkGauge, Data: DataRef{Kind: dataset.KindMetric}, Encoding: Encoding{Y: []Channel{{Field: "value"}}}}
	rows := missingRecords(t)
	for n, want := range map[int]float64{1: -3, 2: math.NaN(), 3: 0, 4: 4, 5: math.NaN()} {
		got := s.Resolve(rows[:n]).Gauge.Value
		if math.IsNaN(want) {
			if !math.IsNaN(got) {
				t.Fatalf("record %d carried stale %v", n, got)
			}
		} else if got != want {
			t.Fatalf("record %d=%v", n, got)
		}
	}
	s.Filter = &Filter{Field: "time", Equals: "d1"}
	if got := s.Resolve(rows).Gauge.Value; got != -3 {
		t.Fatalf("filtered gauge = %v", got)
	}
}

func TestMissingColorSplitPreservesGapAndFiniteTotals(t *testing.T) {
	s, err := Load(strings.NewReader(`{"version":2,"mark":"line","data":{"source":"metrics.jsonl","kind":"metric"},"encoding":{"x":{"field":"time","sort":"-value"},"y":[{"field":"value"}],"color":{"field":"entity","type":"nominal"},"detail":[{"field":"missing_reason"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	rows := []dataset.Record{
		{"name": "demo", "entity": "A", "time": "all-missing", "value": nil, "missing_reason": "outage"},
		{"name": "demo", "entity": "A", "time": "partial", "value": -2},
		{"name": "demo", "entity": "B", "time": "partial", "value": nil, "missing_reason": "not published"},
		{"name": "demo", "entity": "A", "time": "zero", "value": 0},
	}
	if err := dataset.ValidateRecords(dataset.KindMetric, rows); err != nil {
		t.Fatal(err)
	}
	res := s.Resolve(rows)
	if !equalStrings(res.Labels, []string{"zero", "partial", "all-missing"}) {
		t.Fatalf("labels=%v", res.Labels)
	}
	if !math.IsNaN(res.Series[1].Values[1]) || res.Series[1].Extras[1].Detail[0].Value != "not published" {
		t.Fatal("lost aligned missing value/reason")
	}
	s.Encoding.X.Sort = "value"
	res = s.Resolve(rows)
	if !equalStrings(res.Labels, []string{"partial", "zero", "all-missing"}) {
		t.Fatalf("ascending=%v", res.Labels)
	}
}
