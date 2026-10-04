package dataset

import (
	"fmt"
	"math"
	"strings"
)

// Validate checks a record conforms to its kind's canonical schema: every required field is present
// and non-empty, every present numeric field parses as a number, and the record's version (when it
// carries one) is not newer than SchemaVersion. Extra fields beyond the schema are allowed, so a View
// Spec can still address custom fields on a conformant record.
//
// kind is treated as a real schema, not a loose label: the typed structs in this package are the
// contract track-fetch-* writers target, and validation enforces it at the render boundary.
func Validate(kind Kind, rec Record) error {
	if !kind.Valid() {
		return fmt.Errorf("unknown kind %q", kind)
	}
	if v, ok := rec.Float("version"); ok && v > float64(SchemaVersion) {
		return fmt.Errorf("record version %v is newer than supported %d", v, SchemaVersion)
	}
	if kind == KindMetric {
		if err := validateMetricValue(rec); err != nil {
			return err
		}
	}
	for _, f := range KindFields(kind) {
		raw, present := rec[f.Name]
		if kind == KindMetric && f.Name == "value" && present && raw == nil {
			continue // validateMetricValue requires an explanation for this explicit gap
		}
		if !present || isBlank(raw) {
			if f.Required {
				return fmt.Errorf("missing required field %q", f.Name)
			}
			continue
		}
		if f.Type == "number" || f.Type == "number|null" {
			if _, ok := rec.Float(f.Name); !ok {
				return fmt.Errorf("field %q must be a number", f.Name)
			}
		}
	}
	return nil
}

// validateMetricValue distinguishes a deliberate missing observation from malformed input. The
// required value key stays mandatory; only an explicit null with a meaningful reason is a gap.
func validateMetricValue(rec Record) error {
	raw, present := rec["value"]
	reason, hasReason := rec["missing_reason"]
	text, isString := reason.(string)
	if hasReason && !isString {
		return fmt.Errorf("field %q must be a string", "missing_reason")
	}
	if present && raw == nil {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("null metric value requires a non-empty %q", "missing_reason")
		}
		return nil
	}
	if strings.TrimSpace(text) != "" {
		return fmt.Errorf("field %q requires an explicit null metric value", "missing_reason")
	}
	if value, ok := rec.Float("value"); ok && (math.IsNaN(value) || math.IsInf(value, 0)) {
		return fmt.Errorf("field %q must be a finite number or an explicit null with missing_reason", "value")
	}
	return nil
}

// ValidateRecords validates every record against the kind, reporting the 1-based index of the first
// that fails so a bad line in a feed is easy to locate.
func ValidateRecords(kind Kind, records []Record) error {
	for i, rec := range records {
		if err := Validate(kind, rec); err != nil {
			return fmt.Errorf("record %d: %w", i+1, err)
		}
	}
	return nil
}

// isBlank reports whether a field value counts as empty for a required-field check: absent (nil) or a
// whitespace-only string. A zero number is a real value, not blank.
func isBlank(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}
