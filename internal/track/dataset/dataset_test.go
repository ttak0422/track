package dataset

import (
	"math"
	"strings"
	"testing"
)

func TestReadJSONLSkipsBlankLinesAndParses(t *testing.T) {
	in := `
{"version":1,"entity":"AAPL","close":194}

{"version":1,"entity":"MSFT","close":404}
`
	recs, err := ReadJSONL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if s, _ := recs[0].String("entity"); s != "AAPL" {
		t.Fatalf("entity = %q", s)
	}
	if f, ok := recs[1].Float("close"); !ok || f != 404 {
		t.Fatalf("close = %v ok=%v", f, ok)
	}
}

func TestReadJSONLMalformedLineErrorsWithLineNumber(t *testing.T) {
	in := "{\"a\":1}\nnot json\n"
	_, err := ReadJSONL(strings.NewReader(in))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want line 2 error, got %v", err)
	}
}

func TestRecordFloatAcceptsStringAndReportsMissing(t *testing.T) {
	recs, err := ReadJSONL(strings.NewReader(`{"a":"3.5","b":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	r := recs[0]
	if f, ok := r.Float("a"); !ok || f != 3.5 {
		t.Fatalf("string number a = %v ok=%v", f, ok)
	}
	if _, ok := r.Float("b"); ok {
		t.Fatalf("non-numeric b should be !ok")
	}
	if _, ok := r.Float("missing"); ok {
		t.Fatalf("absent field should be !ok")
	}
}

func TestRecordFloatPreservesPrecision(t *testing.T) {
	recs, _ := ReadJSONL(strings.NewReader(`{"v":12345678901234567}`))
	if f, ok := recs[0].Float("v"); !ok || math.Abs(f-12345678901234567) > 2 {
		t.Fatalf("large number lost precision: %v", f)
	}
}

func TestKindValid(t *testing.T) {
	if !KindPrice.Valid() {
		t.Fatal("price should be valid")
	}
	if Kind("bogus").Valid() {
		t.Fatal("bogus should be invalid")
	}
}
