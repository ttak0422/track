package note

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

// Property value types. A value's type is detected from its text form (see ValueType), so the same
// rules apply to sidecar props, inline "key:: value" fields, and CLI input.
const (
	TypeString  = "string"
	TypeNumber  = "number"
	TypeBoolean = "boolean"
	TypeDate    = "date"
	TypeLink    = "link"
)

// Prop is one flattened property value with provenance: Line 0 means it came from the note's sidecar
// props; Line >= 1 is the 1-based body line of an inline "key:: value" field. A list value flattens
// to one Prop per item (same key, same line), preserving item order.
type Prop struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
	Line  int    `json:"line"`
}

// propKeyRe is the accepted property key form, shared by inline-field scanning and CLI validation.
var propKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// numberRe keeps number detection to plain decimals; strconv.ParseFloat alone would also accept
// "inf", "NaN", and hex floats, which nobody means in a note property.
var numberRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// dateRe is the ISO calendar-day form property dates use, matching how Created/Days are stamped.
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ValidPropKey reports whether key is an acceptable property key: a letter followed by letters,
// digits, "_" or "-".
func ValidPropKey(key string) bool {
	return propKeyRe.MatchString(key)
}

// ValueType classifies a scalar value text: "true"/"false" is a boolean, a plain decimal is a
// number, YYYY-MM-DD (a real calendar day) is a date, [[...]] is a link, anything else a string.
func ValueType(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "true" || s == "false":
		return TypeBoolean
	case numberRe.MatchString(s):
		return TypeNumber
	case dateRe.MatchString(s):
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return TypeDate
		}
		return TypeString
	case strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") && len(s) > 4:
		return TypeLink
	default:
		return TypeString
	}
}

// scalarProp types one scalar value text. A link's value is its resolution key: the [[...]] inner
// text with any |display suffix dropped, the same key the link resolver uses.
func scalarProp(key, raw string, line int) Prop {
	raw = strings.TrimSpace(raw)
	typ := ValueType(raw)
	if typ == TypeLink {
		inner := strings.TrimSuffix(strings.TrimPrefix(raw, "[["), "]]")
		if target, _, found := strings.Cut(inner, "|"); found {
			inner = target
		}
		return Prop{Key: key, Value: strings.TrimSpace(inner), Type: TypeLink, Line: line}
	}
	return Prop{Key: key, Value: raw, Type: typ, Line: line}
}

// splitList splits a value text on top-level commas, keeping commas inside [[...]] intact so
// "[[A, B]], [[C]]" is two links. A single-element result means the value is a scalar.
func splitList(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "[["):
			depth++
			i++
		case strings.HasPrefix(s[i:], "]]") && depth > 0:
			depth--
			i++
		case s[i] == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	items := out[:0]
	for _, item := range out {
		if t := strings.TrimSpace(item); t != "" {
			items = append(items, t)
		}
	}
	return items
}

// SidecarProps flattens a sidecar's props map into typed values, sorted by key for stable output.
// YAML scalars keep their native type; a string value is re-typed by its text (so a quoted date or
// [[link]] still classifies), and a list yields one Prop per item in order.
func SidecarProps(meta Metadata) []Prop {
	if len(meta.Props) == 0 {
		return nil
	}
	keys := make([]string, 0, len(meta.Props))
	for k := range meta.Props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Prop
	for _, k := range keys {
		out = append(out, flattenValue(k, meta.Props[k])...)
	}
	return out
}

func flattenValue(key string, v any) []Prop {
	switch val := v.(type) {
	case []any:
		var out []Prop
		for _, item := range val {
			out = append(out, flattenValue(key, item)...)
		}
		return out
	case bool:
		return []Prop{{Key: key, Value: strconv.FormatBool(val), Type: TypeBoolean}}
	case int:
		return []Prop{{Key: key, Value: strconv.Itoa(val), Type: TypeNumber}}
	case int64:
		return []Prop{{Key: key, Value: strconv.FormatInt(val, 10), Type: TypeNumber}}
	case float64:
		return []Prop{{Key: key, Value: strconv.FormatFloat(val, 'f', -1, 64), Type: TypeNumber}}
	case string:
		return []Prop{scalarProp(key, val, 0)}
	default:
		return []Prop{{Key: key, Value: fmt.Sprintf("%v", val), Type: TypeString}}
	}
}

// inlineLineRe matches a whole-line field: an optional list marker, then "key:: value" to the end of
// the line. inlineBracketRe matches the mid-sentence form "[key:: value]" anywhere in a line.
var (
	inlineLineRe = regexp.MustCompile(`^\s*(?:[-*+]\s+|\d+[.)]\s+)?([A-Za-z][A-Za-z0-9_-]*)::\s+(.+)$`)
	// The bracketed value is any run of [[...]] links and non-bracket text, so "[owner:: [[Ada]]]"
	// keeps the link's own closing brackets inside the field.
	inlineBracketRe = regexp.MustCompile(`\[([A-Za-z][A-Za-z0-9_-]*)::\s+((?:\[\[[^\[\]]+\]\]|[^\[\]])+)\]`)
	// fenceRe matches a code-fence line, capturing its run of marks and the info string after it. A
	// block is closed only by a bare run of the same mark at least as long (CommonMark), so the shorter
	// fences nested inside a ````-wrapped Markdown example stay code instead of ending the block early
	// and turning the example's fields into real data.
	fenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})\\s*(.*)$")
	// codeSpanRe finds `inline code`; a bracketed field inside one is prose about the syntax, not data.
	codeSpanRe = regexp.MustCompile("`[^`]*`")
)

// scanProse calls fn for every body line outside a fenced code block, with its 1-based line number.
// Fields only ever come from prose, so this fence walk is the one gate every field scan passes.
func scanProse(body string, fn func(line string, num int)) {
	fence := ""
	for i, line := range strings.Split(body, "\n") {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				fence = m[1]
			case m[1][0] == fence[0] && len(m[1]) >= len(fence) && m[2] == "":
				fence = ""
			}
			continue
		}
		if fence == "" {
			fn(line, i+1)
		}
	}
}

// InlineFields scans a note body for "key:: value" fields — a whole line (list items included) or a
// bracketed [key:: value] inside prose — and returns them typed with their 1-based line. Fenced code
// blocks are skipped, and a comma-separated value becomes one Prop per item.
func InlineFields(body string) []Prop {
	var out []Prop
	scanProse(body, func(line string, num int) {
		if m := inlineLineRe.FindStringSubmatch(line); m != nil {
			out = append(out, fieldProps(m[1], m[2], num)...)
			return
		}
		// Mask `inline code` first so a documented "[key:: value]" example never becomes data.
		for _, m := range inlineBracketRe.FindAllStringSubmatch(codeSpanRe.ReplaceAllString(line, "``"), -1) {
			out = append(out, fieldProps(m[1], m[2], num)...)
		}
	})
	return out
}

func fieldProps(key, value string, line int) []Prop {
	var out []Prop
	for _, item := range splitList(value) {
		out = append(out, scalarProp(key, item, line))
	}
	return out
}

// CollectProps returns every property of a note — sidecar props first, then inline fields in body
// order. It is the one flattening the indexer, the web note view, and doctor all use.
func CollectProps(meta Metadata, body string) []Prop {
	return append(SidecarProps(meta), InlineFields(body)...)
}

// ParsePropValue converts CLI/user value text into the YAML value stored in a sidecar's props map:
// booleans and numbers become native scalars, comma-separated text a list, everything else (dates
// and [[links]] included) stays a string and is typed on read.
func ParsePropValue(raw string) any {
	items := splitList(raw)
	if len(items) > 1 {
		list := make([]any, len(items))
		for i, item := range items {
			list[i] = parseScalar(item)
		}
		return list
	}
	if len(items) == 0 {
		return ""
	}
	return parseScalar(items[0])
}

func parseScalar(s string) any {
	switch ValueType(s) {
	case TypeBoolean:
		return s == "true"
	case TypeNumber:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return s
}

// Violation is one property that breaks the configured schema.
type Violation struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// CheckProps validates flattened properties against the configured schema. Keys absent from the
// schema are fine (the schema is opt-in per key); for declared keys, a non-"string" type must match
// the detected value type, and a non-empty enum must contain the value.
func CheckProps(props []Prop, schema map[string]config.PropSpec) []Violation {
	var out []Violation
	for _, p := range props {
		spec, ok := schema[p.Key]
		if !ok {
			continue
		}
		if spec.Type != "" && spec.Type != TypeString && spec.Type != p.Type {
			out = append(out, Violation{Key: p.Key, Value: p.Value, Line: p.Line,
				Message: fmt.Sprintf("value %q is not a %s", p.Value, spec.Type)})
			continue
		}
		if len(spec.Values) > 0 && !slices.Contains(spec.Values, p.Value) {
			out = append(out, Violation{Key: p.Key, Value: p.Value, Line: p.Line,
				Message: fmt.Sprintf("value %q is not one of: %s", p.Value, strings.Join(spec.Values, ", "))})
		}
	}
	return out
}
