// Package dataset defines track's Canonical Data Model and reads it from JSONL.
//
// track itself never talks to external services: market APIs, RSS, news, and social feeds are the job
// of separate track-fetch-* tools (see docs/adr/0021), which convert the outside world into these
// canonical records. Everything downstream of this package — query, metrics, and the View Spec
// renderers — operates only on the model defined here, so adding a new data source never touches track.
//
// JSONL is the first-class format: one record per line, one homogeneous file per kind
// (events.jsonl, prices.jsonl, metrics.jsonl, ...). Every record carries a schema Version so the
// format can evolve without silently misreading old data.
package dataset

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the current Canonical Data Model version. Records without a version are read as
// version 0 (pre-versioning) and may be rejected by stricter callers; writers should always stamp it.
const SchemaVersion = 1

// Kind names the canonical record types. A JSONL file holds a single kind.
type Kind string

const (
	KindEvent      Kind = "event"      // a point-in-time happening (news, post, milestone)
	KindPrice      Kind = "price"      // an OHLCV bar for an entity at a time
	KindMetric     Kind = "metric"     // a named numeric series sample (e.g. a Pressure Index)
	KindEntity     Kind = "entity"     // a thing series refer to (a ticker, index, sector)
	KindAnnotation Kind = "annotation" // a label attached to a time/target, for narrative overlays
)

// KnownKinds lists every kind the model defines, in a stable order.
var KnownKinds = []Kind{KindEvent, KindPrice, KindMetric, KindEntity, KindAnnotation}

// Valid reports whether k is one of the known kinds.
func (k Kind) Valid() bool {
	return slices.Contains(KnownKinds, k)
}

// The typed structs below document the canonical shape of each kind and back validation/tests. The
// render pipeline reads JSONL generically as Record so the View Spec can address any field by name;
// these types are the contract track-fetch-* writers target.

// Event is a point-in-time happening. Title is required; URL/Note carry provenance and detail.
type Event struct {
	Version int       `json:"version"`
	Time    time.Time `json:"time"`
	Title   string    `json:"title"`
	Entity  string    `json:"entity,omitempty"`
	URL     string    `json:"url,omitempty"`
	Note    string    `json:"note,omitempty"`
}

// Price is one OHLCV bar for an entity at a time.
type Price struct {
	Version int       `json:"version"`
	Entity  string    `json:"entity"`
	Time    time.Time `json:"time"`
	Open    float64   `json:"open"`
	High    float64   `json:"high"`
	Low     float64   `json:"low"`
	Close   float64   `json:"close"`
	Volume  float64   `json:"volume,omitempty"`
}

// Metric is a named numeric series sample, optionally scoped to an entity.
type Metric struct {
	Version int       `json:"version"`
	Name    string    `json:"name"`
	Entity  string    `json:"entity,omitempty"`
	Time    time.Time `json:"time"`
	Value   float64   `json:"value"`
}

// Entity is a thing that prices/metrics/events refer to.
type Entity struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"` // e.g. stock, index, fx, commodity, sector
}

// Annotation labels a time (and optional target series) for narrative overlays.
type Annotation struct {
	Version int       `json:"version"`
	Time    time.Time `json:"time"`
	Text    string    `json:"text"`
	Target  string    `json:"target,omitempty"`
}

// Field describes one field of a canonical kind for reference/help: its JSON name, a friendly type,
// and whether it is required (a field without `,omitempty`). It is derived from the typed structs so
// the contract, help text, and docs share one source.
type Field struct {
	Name     string
	Type     string
	Required bool
}

// kindStructs is the reflection source for KindFields: a zero value of each kind's typed struct. The
// structs are the canonical contract; deriving the field list from them keeps help/docs from drifting.
var kindStructs = map[Kind]any{
	KindEvent:      Event{},
	KindPrice:      Price{},
	KindMetric:     Metric{},
	KindEntity:     Entity{},
	KindAnnotation: Annotation{},
}

// KindFields returns the canonical field schema of a kind in declaration order, omitting the shared
// `version` field (every record carries it). Returns nil for an unknown kind.
func KindFields(k Kind) []Field {
	s, ok := kindStructs[k]
	if !ok {
		return nil
	}
	t := reflect.TypeOf(s)
	var out []Field
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || name == "version" {
			continue
		}
		out = append(out, Field{
			Name:     name,
			Type:     jsonType(f.Type),
			Required: !strings.Contains(opts, "omitempty"),
		})
	}
	return out
}

// jsonType maps a Go field type to a friendly JSON type name for help/docs.
func jsonType(t reflect.Type) string {
	switch {
	case t == reflect.TypeOf(time.Time{}):
		return "time(RFC3339)"
	case t.Kind() == reflect.String:
		return "string"
	case t.Kind() == reflect.Float64 || t.Kind() == reflect.Int:
		return "number"
	default:
		return t.Kind().String()
	}
}

// Doc returns a one-line description of a kind for help/reference.
func (k Kind) Doc() string {
	switch k {
	case KindEvent:
		return "a point-in-time happening (news, post, milestone)"
	case KindPrice:
		return "an OHLCV bar for an entity at a time"
	case KindMetric:
		return "a named numeric series sample (e.g. a Pressure Index)"
	case KindEntity:
		return "a thing series refer to (a ticker, index, sector)"
	case KindAnnotation:
		return "a label attached to a time/target, for narrative overlays"
	default:
		return ""
	}
}

// Record is a generic canonical record: a decoded JSONL object keyed by field name. The render
// pipeline reads records as Record so a View Spec can address any field by name without this package
// knowing the spec. Numbers are decoded as json.Number so Float can read them losslessly.
type Record map[string]any

// ReadJSONL streams a JSONL document into records, one per non-blank line. Lines are decoded with
// number preservation (json.Number) so numeric fields survive without float rounding surprises.
// A malformed line fails the whole read with its line number, since silently dropping data would
// corrupt a chart without warning.
func ReadJSONL(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	// JSONL records (a news event with a body, say) can exceed bufio's default 64K line cap.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []Record
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var rec Record
		if err := dec.Decode(&rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Float reads field as a float64, accepting json.Number, float64, int, and numeric strings. The
// second result is false when the field is absent or not numeric, so callers can skip rather than
// plot a zero.
func (r Record) Float(field string) (float64, bool) {
	v, ok := r[field]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// String reads field as a display string. Numbers render via their json.Number text so labels keep
// the source precision. The second result is false when the field is absent.
func (r Record) String(field string) (string, bool) {
	v, ok := r[field]
	if !ok || v == nil {
		return "", false
	}
	switch s := v.(type) {
	case string:
		return s, true
	case json.Number:
		return s.String(), true
	case bool:
		return strconv.FormatBool(s), true
	default:
		return fmt.Sprintf("%v", s), true
	}
}
