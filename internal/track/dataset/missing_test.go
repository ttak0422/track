package dataset

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMetricMissingObservationContract(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"number", `"value":3`, true},
		{"zero", `"value":0`, true},
		{"negative", `"value":-2`, true},
		{"legacy numeric string", `"value":"3.5"`, true},
		{"explicit missing", `"value":null,"missing_reason":"source unavailable"`, true},
		{"missing value", `"entity":"demo"`, false},
		{"omitted value with reason", `"missing_reason":"not published"`, false},
		{"null without reason", `"value":null`, false},
		{"empty reason", `"value":null,"missing_reason":""`, false},
		{"blank reason", `"value":null,"missing_reason":"  \t"`, false},
		{"numeric reason", `"value":null,"missing_reason":42`, false},
		{"null reason", `"value":null,"missing_reason":null`, false},
		{"contradictory reason", `"value":0,"missing_reason":"unavailable"`, false},
		{"invalid number", `"value":"unknown"`, false},
		{"blank number", `"value":" "`, false},
		{"boolean number", `"value":false`, false},
		{"nan", `"value":"NaN"`, false},
		{"infinite", `"value":"+Inf"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rec(t, `{"version":1,"name":"demo","time":"2026-10-01",`+tc.fields+`}`)
			if err := Validate(KindMetric, r); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := Validate(KindMetric, Record{"name": "demo", "time": "t", "value": v}); err == nil {
			t.Fatal("non-finite value accepted")
		}
	}
	for _, r := range []Record{{"name": "demo", "value": nil, "missing_reason": "unavailable"}, {"time": "t", "value": nil, "missing_reason": "unavailable"}} {
		if err := Validate(KindMetric, r); err == nil {
			t.Fatal("missing identity accepted")
		}
	}
	if err := Validate(KindPrice, rec(t, `{"entity":"demo","time":"t","open":1,"high":1,"low":1,"close":null,"missing_reason":"unavailable"}`)); err == nil {
		t.Fatal("price null accepted")
	}
}

func TestMetricTypedJSONAndFieldSchema(t *testing.T) {
	zero := 0.0
	for _, m := range []Metric{
		{Version: 1, Name: "demo", Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Value: &zero},
		{Version: 1, Name: "demo", Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), MissingReason: "not published"},
	} {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		records, err := ReadJSONL(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateRecords(KindMetric, records); err != nil {
			t.Fatalf("%s: %v", data, err)
		}
		var roundTrip Metric
		if err := json.Unmarshal(data, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if (roundTrip.Value == nil) != (m.Value == nil) {
			t.Fatalf("lost missing state: %s", data)
		}
	}
	for _, f := range KindFields(KindMetric) {
		if f.Name == "value" && (f.Type != "number|null" || !f.Required) {
			t.Fatalf("value schema = %+v", f)
		}
	}
}
