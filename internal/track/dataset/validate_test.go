package dataset

import (
	"encoding/json"
	"strings"
	"testing"
)

func rec(t *testing.T, s string) Record {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var r Record
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return r
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		rec  string
		ok   bool
	}{
		{"price ok", KindPrice, `{"version":1,"entity":"AAPL","time":"d1","open":1,"high":2,"low":1,"close":2}`, true},
		{"price ok extra field", KindPrice, `{"entity":"AAPL","time":"d1","open":1,"high":2,"low":1,"close":2,"adj":1.5}`, true},
		{"price missing close", KindPrice, `{"entity":"AAPL","time":"d1","open":1,"high":2,"low":1}`, false},
		{"price blank entity", KindPrice, `{"entity":"  ","time":"d1","open":1,"high":2,"low":1,"close":2}`, false},
		{"price non-numeric high", KindPrice, `{"entity":"AAPL","time":"d1","open":1,"high":"x","low":1,"close":2}`, false},
		{"price zero is valid", KindPrice, `{"entity":"AAPL","time":"d1","open":0,"high":0,"low":0,"close":0}`, true},
		{"metric ok", KindMetric, `{"name":"PI","time":"d1","value":3}`, true},
		{"metric missing time", KindMetric, `{"name":"PI","value":3}`, false},
		{"event ok, optional absent", KindEvent, `{"time":"d1","title":"launch"}`, true},
		{"event missing title", KindEvent, `{"time":"d1"}`, false},
		{"unknown kind", Kind("bogus"), `{"x":1}`, false},
		{"future version rejected", KindMetric, `{"version":2,"name":"PI","time":"d1","value":3}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.kind, rec(t, c.rec))
			if c.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestValidateRecordsReportsIndex(t *testing.T) {
	recs := []Record{
		rec(t, `{"name":"A","time":"t","value":1}`),
		rec(t, `{"name":"B","time":"t"}`), // missing value
	}
	err := ValidateRecords(KindMetric, recs)
	if err == nil || !strings.Contains(err.Error(), "record 2") {
		t.Fatalf("expected error pointing at record 2, got %v", err)
	}
}
