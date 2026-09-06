package metrics

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
	"github.com/ttak0422/track/internal/track/viewspec"
)

var fenceJSON = regexp.MustCompile("(?s)```viewspec\n(.*?)```")

func TestParseQuery(t *testing.T) {
	q, err := ParseQuery(`http_requests{method="GET", code="200"}`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Family != "http_requests" || len(q.Matchers) != 2 {
		t.Fatalf("got %+v", q)
	}
	if _, err := ParseQuery(`foo{a=~"b.*"}`); err == nil {
		t.Fatal("regex matcher should fail")
	}
	if _, err := ParseQuery(`rate(foo[5m])`); err == nil {
		t.Fatal("function call should fail")
	}
	if _, err := ParseQuery(`foo`); err != nil {
		t.Fatal(err)
	}
}

func TestFoldRoundTrip(t *testing.T) {
	folded := Fold("m", map[string]string{"b": "2", "a": `x"y`})
	if folded != `m{a="x\"y",b="2"}` {
		t.Fatalf("got %q", folded)
	}
	fam, labels, err := SplitFolded(folded)
	if err != nil || fam != "m" || labels["a"] != `x"y` || labels["b"] != "2" {
		t.Fatalf("got %q %+v %v", fam, labels, err)
	}
	if !MatchRecord(folded, Query{Family: "m", Matchers: []Matcher{{"a", `x"y`}}}) {
		t.Fatal("should match")
	}
	if MatchRecord(folded, Query{Family: "m", Matchers: []Matcher{{"a", "other"}}}) {
		t.Fatal("should not match")
	}
	if !MatchRecord(folded, Query{Family: "m"}) {
		t.Fatal("bare family should match")
	}
}

func TestParseComparisonOpInLabel(t *testing.T) {
	c, err := ParseComparison(`m{note="a>b"} > 1.5`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Op != ">" || c.Value != 1.5 || c.Query.Matchers[0].Value != "a>b" {
		t.Fatalf("got %+v", c)
	}
}

func TestScrape(t *testing.T) {
	exposition := `# HELP market_close Last close.
# TYPE market_close gauge
market_close{symbol="6857.T"} 33090
market_close{symbol="998407.O"} 65020.94 1756771200000
# TYPE http_requests counter
http_requests_total{method="GET"} 42
# TYPE latency histogram
latency_bucket{le="0.1"} 5
latency_bucket{le="+Inf"} 9
latency_sum 1.5
latency_count 9
`
	recs, err := Scrape(strings.NewReader(exposition), ScrapeOptions{Stamp: mustTime(t, "2026-09-05T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]dataset.Record{}
	var closes []dataset.Record
	for _, r := range recs {
		n, _ := r.String("name")
		byName[n] = r
		if n == "market_close" {
			closes = append(closes, r)
		}
	}
	if len(closes) != 2 {
		t.Fatalf("want 2 market_close series (entity carries the symbol), have %v", keys(byName))
	}
	got := closes[0]
	if e, _ := got.String("entity"); e != "6857.T" {
		got = closes[1]
	}
	if e, _ := got.String("entity"); e != "6857.T" {
		t.Fatalf("entity = %q", e)
	}
	if tm, _ := got.String("time"); tm != "2026-09-05T00:00:00Z" {
		t.Fatalf("stamp = %q", tm)
	}
	// Explicit exposition timestamp wins over the scrape stamp; the labeled series keeps
	// its symbol folded out (symbol feeds entity) while a non-entity label stays in the name.
	tsRec := byName[`market_close{symbol="998407.O"}`]
	if tsRec == nil {
		// symbol is an entity label, so this series folds bare; find it by entity instead.
		for _, r := range closes {
			if e, _ := r.String("entity"); e == "998407.O" {
				tsRec = r
			}
		}
	}
	if tm, _ := tsRec.String("time"); tm != "2025-09-02T00:00:00Z" {
		t.Fatalf("explicit ts = %q", tm)
	}
	if _, ok := byName[`http_requests_total{method="GET"}`]; !ok {
		t.Fatal("counter should keep _total with other labels folded")
	}
	for _, n := range []string{`latency_bucket{le="0.1"}`, `latency_bucket{le="+Inf"}`, "latency_sum", "latency_count"} {
		if _, ok := byName[n]; !ok {
			t.Fatalf("missing histogram sample %s", n)
		}
	}
}

func TestDashboardAndAlert(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "m.jsonl"),
		`{"version":1,"name":"rsi14","entity":"T","time":"2026-09-03","value":28}`,
		`{"version":1,"name":"rsi14","entity":"T","time":"2026-09-04","value":27}`,
		`{"version":1,"name":"rsi14","entity":"U","time":"2026-09-03","value":60}`,
		`{"version":1,"name":"rsi14","entity":"U","time":"2026-09-04","value":61}`,
		`{"version":1,"name":"close","entity":"T","time":"2026-09-04","value":100}`)
	dash := `{"title":"M","panels":[{"type":"timeseries","title":"RSI","datasource":{"type":"track","uid":"m.jsonl"},"targets":[{"expr":"rsi14","legendFormat":"RSI"}],"fieldConfig":{"defaults":{"thresholds":{"steps":[{"value":null},{"value":30},{"value":70}]}}}},{"type":"stat","title":"By entity","datasource":{"type":"track","uid":"m.jsonl"},"targets":[{"expr":"rsi14","legendFormat":"{{entity}}"}]}]}`
	md, n, err := ResolveDashboard([]byte(dash), dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || !strings.Contains(md, "```viewspec") || !strings.Contains(md, `"y": 30`) {
		t.Fatalf("bad dashboard output:\n%s", md)
	}
	// Every emitted fence must pass the renderer's own validation, including the
	// color-split panel (y[1+] carry explicit marks, y[0] must not).
	for _, m := range fenceJSON.FindAllStringSubmatch(md, -1) {
		if _, err := viewspec.Load(strings.NewReader(m[1])); err != nil {
			t.Fatalf("emitted spec invalid: %v\n%s", err, m[1])
		}
	}
	bad := `{"title":"M","panels":[{"type":"heatmap","title":"H","targets":[{"expr":"rsi14"}]}]}`
	if _, _, err := ResolveDashboard([]byte(bad), dir); err == nil {
		t.Fatal("heatmap panel should fail")
	}
	fn := `{"title":"M","panels":[{"type":"timeseries","title":"R","datasource":{"type":"track","uid":"m.jsonl"},"targets":[{"expr":"rate(rsi14[5m])"}]}]}`
	if _, _, err := ResolveDashboard([]byte(fn), dir); err == nil {
		t.Fatal("function expr should fail")
	}
	rules := `
groups:
- name: m
  rules:
  - alert: Oversold
    expr: rsi14 < 30
    for: 2
    annotations: {summary: "oversold"}
  - alert: Never
    expr: rsi14 > 90
`
	rf, err := ParseRules([]byte(rules))
	if err != nil {
		t.Fatal(err)
	}
	firing, err := EvalRules(dir, rf)
	if err != nil {
		t.Fatal(err)
	}
	if len(firing) != 1 || firing[0].Alert != "Oversold" || firing[0].Value != 27 {
		t.Fatalf("got %+v", firing)
	}
}

func day(i int) string {
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("2006-01-02")
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]dataset.Record) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
