// Package metrics implements generic monitoring on adopted specs (ADR 0076): OpenMetrics
// exposition ingest, Grafana dashboard-subset resolution, and Prometheus rule-subset alert
// evaluation over the Canonical Data Model. It knows nothing about where series come from —
// node exporters, app instrumentation, or market writers all land as metric records first.
package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Matcher is one `name="value"` label constraint. Only equality is supported: `=~`, `!=`, and
// regex matchers are a loud error, because the subset stops at what the vault can answer.
type Matcher struct {
	Name  string
	Value string
}

// Query is a metric family name plus equality matchers: `name` or `name{k="v", ...}`.
type Query struct {
	Family   string
	Matchers []Matcher
}

// Comparison is a query against a numeric threshold: `metric{matchers} OP number`.
type Comparison struct {
	Query Query
	Op    string
	Value float64
}

var validOps = []string{">", "<", ">=", "<=", "==", "!=", "="}

func isNameChar(c byte, first bool) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == ':' {
		return true
	}
	return !first && c >= '0' && c <= '9'
}

// ParseQuery parses `name` or `name{k="v", ...}`. Anything else — regex matchers, functions,
// ranges — fails naming the subset boundary.
func ParseQuery(s string) (Query, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Query{}, fmt.Errorf("empty metric query")
	}
	i := 0
	for i < len(s) && isNameChar(s[i], i == 0) {
		i++
	}
	if i == 0 {
		return Query{}, fmt.Errorf("bad metric name in %q", s)
	}
	q := Query{Family: s[:i]}
	rest := strings.TrimSpace(s[i:])
	if rest == "" {
		return q, nil
	}
	if !strings.HasPrefix(rest, "{") || !strings.HasSuffix(rest, "}") {
		return Query{}, fmt.Errorf("bad matcher block in %q (want name{k=\"v\", ...})", s)
	}
	inner := rest[1 : len(rest)-1]
	if strings.TrimSpace(inner) == "" {
		return q, nil
	}
	pos := 0
	for {
		for pos < len(inner) && (inner[pos] == ' ' || inner[pos] == '\t' || inner[pos] == ',') {
			pos++
		}
		if pos >= len(inner) {
			break
		}
		start := pos
		for pos < len(inner) && isNameChar(inner[pos], pos == start) {
			pos++
		}
		if pos == start {
			return Query{}, fmt.Errorf("bad label name in %q", s)
		}
		name := inner[start:pos]
		if pos >= len(inner) || inner[pos] != '=' {
			return Query{}, fmt.Errorf("want = after label %q in %q (only = matchers are supported)", name, s)
		}
		pos++
		if pos < len(inner) && inner[pos] == '~' {
			return Query{}, fmt.Errorf("regex matchers are outside the subset in %q", s)
		}
		if pos >= len(inner) || inner[pos] != '"' {
			return Query{}, fmt.Errorf("label value must be quoted in %q", s)
		}
		val, next, err := parseQuoted(inner, pos)
		if err != nil {
			return Query{}, fmt.Errorf("bad label value in %q: %v", s, err)
		}
		pos = next
		if pos < len(inner) && inner[pos] == '!' {
			return Query{}, fmt.Errorf("!= matchers are outside the subset in %q", s)
		}
		q.Matchers = append(q.Matchers, Matcher{Name: name, Value: val})
	}
	if len(q.Matchers) == 0 {
		return Query{}, fmt.Errorf("empty matcher block in %q", s)
	}
	return q, nil
}

// parseQuoted reads a double-quoted string at inner[pos] (the quote), honoring Prometheus escapes
// (\\, \", \n), and returns the value plus the offset just past the closing quote.
func parseQuoted(inner string, pos int) (string, int, error) {
	var b strings.Builder
	i := pos + 1
	for i < len(inner) {
		c := inner[i]
		if c == '"' {
			return b.String(), i + 1, nil
		}
		if c == '\\' {
			i++
			if i >= len(inner) {
				break
			}
			switch inner[i] {
			case '\\', '"':
				b.WriteByte(inner[i])
			case 'n':
				b.WriteByte('\n')
			default:
				return "", 0, fmt.Errorf("bad escape \\%c", inner[i])
			}
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return "", 0, fmt.Errorf("unterminated string")
}

// quote escapes a label value back into Prometheus display form.
func quote(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Fold renders a family plus labels in canonical display form with keys sorted, so the same series
// always folds to the same name. This is the name-folding of ADR 0076: labels the model cannot hold
// travel inside the name and parse back out here.
func Fold(family string, labels map[string]string) string {
	if len(labels) == 0 {
		return family
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(family)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(quote(labels[k]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// SplitFolded parses a folded name back into its family and labels.
func SplitFolded(s string) (string, map[string]string, error) {
	q, err := ParseQuery(s)
	if err != nil {
		return "", nil, err
	}
	labels := make(map[string]string, len(q.Matchers))
	for _, m := range q.Matchers {
		if _, dup := labels[m.Name]; dup {
			return "", nil, fmt.Errorf("duplicate label %q in %q", m.Name, s)
		}
		labels[m.Name] = m.Value
	}
	return q.Family, labels, nil
}

// MatchRecord reports whether a folded record name satisfies the query: same family, and every
// matcher equal. A bare-family query matches every series of that family.
func MatchRecord(recName string, q Query) bool {
	return MatchRecordWithEntity(recName, "", q)
}

// MatchRecordWithEntity is MatchRecord plus the entity field: matchers naming entity, symbol,
// or instance compare against the record's entity when the folded name carries no such label.
// This keeps multi-series metric files addressable (`http_requests{instance="web1"}`) without changing
// the subset grammar.
func MatchRecordWithEntity(recName, entity string, q Query) bool {
	fam, labels, err := SplitFolded(recName)
	if err != nil || fam != q.Family {
		return false
	}
	for _, m := range q.Matchers {
		if v, ok := labels[m.Name]; ok {
			if v != m.Value {
				return false
			}
			continue
		}
		switch m.Name {
		case "entity", "symbol", "instance":
			if entity != m.Value {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ParseComparison parses `metric{matchers} OP number` with OP in > < >= <= == != (= means ==).
// The operator is the first one outside the matcher braces and quotes, so a `>` inside a label
// value can never split the expression.
func ParseComparison(s string) (Comparison, error) {
	depth, inStr, esc := 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
		case '>', '<', '=', '!':
			if depth == 0 {
				for _, op := range []string{">=", "<=", "==", "!=", ">", "<", "="} {
					if strings.HasPrefix(s[i:], op) {
						q, err := ParseQuery(s[:i])
						if err != nil {
							return Comparison{}, err
						}
						v, err := strconv.ParseFloat(strings.TrimSpace(s[i+len(op):]), 64)
						if err != nil {
							return Comparison{}, fmt.Errorf("bad threshold in %q", s)
						}
						if op == "=" {
							op = "=="
						}
						return Comparison{Query: q, Op: op, Value: v}, nil
					}
				}
				return Comparison{}, fmt.Errorf("bad operator in %q (want one of %s)", s, strings.Join(validOps, " "))
			}
		}
	}
	return Comparison{}, fmt.Errorf("no comparison operator in %q (want metric{...} OP number)", s)
}

// Compare applies the operator. validOps documents the set for help text.
func Compare(op string, value, threshold float64) bool {
	switch op {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	}
	return false
}

// ValidOps lists the comparison operators the subset accepts.
func ValidOps() []string { return validOps }

// ParseTime reads the time shapes the vault holds: RFC3339, a minute-truncated variant, or a bare
// date. Anything else fails rather than guessing an order.
func ParseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time %q (want RFC3339 or YYYY-MM-DD)", s)
}
