package viewspec

import (
	"math"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/dataset"
)

const goodSpec = `{
  "version": 1,
  "type": "line",
  "title": "AAPL",
  "data": {"source": "prices.jsonl", "kind": "price"},
  "x": {"field": "time"},
  "y": [{"field": "close", "label": "Close"}],
  "filter": {"field": "entity", "equals": "AAPL"}
}`

func TestLoadValid(t *testing.T) {
	s, err := Load(strings.NewReader(goodSpec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Type != ChartLine || s.Data.Kind != dataset.KindPrice || len(s.Y) != 1 {
		t.Fatalf("unexpected spec: %+v", s)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(strings.NewReader(`{"version":1,"type":"line","data":{"source":"x","kind":"price"},"x":{"field":"t"},"y":[{"field":"c"}],"bogus":1}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want unknown-field error, got %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]Spec{
		"missing version": {Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"bad type":        {Version: 1, Type: "pie", Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"bad kind":        {Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: "bogus"}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"no source":       {Version: 1, Type: ChartLine, Data: DataRef{Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
		"no x":            {Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, Y: []Encoding{{Field: "c"}}},
		"no y":            {Version: 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}},
		"future version":  {Version: Version + 1, Type: ChartLine, Data: DataRef{Source: "x", Kind: dataset.KindPrice}, X: Encoding{Field: "t"}, Y: []Encoding{{Field: "c"}}},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestResolveFiltersAndAligns(t *testing.T) {
	s, _ := Load(strings.NewReader(goodSpec))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","entity":"AAPL","close":10}` + "\n" +
			`{"time":"d2","entity":"MSFT","close":99}` + "\n" +
			`{"time":"d3","entity":"AAPL","close":12}` + "\n"))
	res := s.Resolve(recs)
	if want := []string{"d1", "d3"}; !equalStrings(res.Labels, want) {
		t.Fatalf("labels = %v, want %v", res.Labels, want)
	}
	if len(res.Series) != 1 || res.Series[0].Label != "Close" {
		t.Fatalf("series = %+v", res.Series)
	}
	if got := res.Series[0].Values; got[0] != 10 || got[1] != 12 {
		t.Fatalf("values = %v", got)
	}
}

func TestResolveMissingYBecomesNaN(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":1,"type":"line","data":{"source":"x","kind":"metric"},"x":{"field":"time"},"y":[{"field":"value"}]}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","value":5}` + "\n" + `{"time":"d2"}` + "\n"))
	res := s.Resolve(recs)
	if res.Series[0].Values[0] != 5 {
		t.Fatalf("first value = %v", res.Series[0].Values[0])
	}
	if !math.IsNaN(res.Series[0].Values[1]) {
		t.Fatalf("missing value should be NaN, got %v", res.Series[0].Values[1])
	}
}

func TestOverlayMarkersDefaultsAndSkips(t *testing.T) {
	// At/Label unset → defaults to "time"/"text"; a record without the at field is skipped.
	ov := Overlay{Source: "ann.jsonl", Kind: dataset.KindAnnotation}
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"time":"d1","text":"hello"}` + "\n" + `{"text":"no time here"}` + "\n"))
	ms := ov.Markers(recs)
	if len(ms) != 1 || ms[0].At != "d1" || ms[0].Label != "hello" {
		t.Fatalf("markers = %+v", ms)
	}
}

func TestOverlayMarkersCustomFields(t *testing.T) {
	ov := Overlay{Source: "events.jsonl", Kind: dataset.KindEvent, At: "time", Label: "title"}
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d2","title":"tariff"}`))
	ms := ov.Markers(recs)
	if len(ms) != 1 || ms[0].Label != "tariff" {
		t.Fatalf("markers = %+v", ms)
	}
}

func TestResolveAssignsAxis(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":1,"type":"line","data":{"source":"x","kind":"price"},` +
		`"x":{"field":"time"},"y":[{"field":"close"},{"field":"vix","axis":"y2"}]}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(`{"time":"d1","close":1,"vix":2}`))
	res := s.Resolve(recs)
	if res.Series[0].Axis != "y" {
		t.Fatalf("default axis = %q, want y", res.Series[0].Axis)
	}
	if res.Series[1].Axis != "y2" {
		t.Fatalf("explicit axis = %q, want y2", res.Series[1].Axis)
	}
}

func TestResolveBubbleBuildsPoints(t *testing.T) {
	s, _ := Load(strings.NewReader(`{"version":1,"type":"bubble","data":{"source":"x","kind":"metric"},` +
		`"x":{"field":"ret"},"y":[{"field":"vol"}],"size":{"field":"sz"}}`))
	recs, _ := dataset.ReadJSONL(strings.NewReader(
		`{"ret":12,"vol":40,"sz":1000}` + "\n" + `{"ret":-5,"vol":18,"sz":600}` + "\n"))
	res := s.Resolve(recs)
	if len(res.Labels) != 0 {
		t.Fatalf("bubble should not produce category labels, got %v", res.Labels)
	}
	if len(res.Series) != 1 || len(res.Series[0].Points) != 2 {
		t.Fatalf("series/points = %+v", res.Series)
	}
	p := res.Series[0].Points[0]
	if p.X != 12 || p.Y != 40 || p.R != 1000 {
		t.Fatalf("point = %+v", p)
	}
}

func TestValidateBubbleRequiresSize(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartBubble,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		X:    Encoding{Field: "ret"}, Y: []Encoding{{Field: "vol"}},
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("want size error, got %v", err)
	}
}

func TestValidateRejectsBadAxis(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartLine,
		Data: DataRef{Source: "x", Kind: dataset.KindPrice},
		X:    Encoding{Field: "time"}, Y: []Encoding{{Field: "close", Axis: "y3"}},
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "axis") {
		t.Fatalf("want axis error, got %v", err)
	}
}

func TestValidateRejectsBadOverlay(t *testing.T) {
	s := Spec{
		Version: 1, Type: ChartLine,
		Data: DataRef{Source: "x", Kind: dataset.KindMetric},
		X:    Encoding{Field: "time"}, Y: []Encoding{{Field: "value"}},
		Overlays: []Overlay{{Kind: dataset.KindEvent}}, // missing source
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "overlays[0].source") {
		t.Fatalf("want overlay source error, got %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
