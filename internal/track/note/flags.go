package note

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Flag is one author-assigned marker from the implementation-defined closed set
// (ADR 0074). The set is strictly closed: flags are provided by the implementation,
// not user-extensible, and a new flag means extending the registry, its validation,
// its rendering, and its search behavior together. Per-flag behavior is implemented
// explicitly per code path (a switch over the flag), not through a generic data table.
type Flag string

const (
	// FlagDeprecated marks a note superseded by another. It lowers the note's
	// search ranking (see deprecatedRankPenalty in the store) in addition to the
	// red stamp and list badge every flag carries.
	FlagDeprecated Flag = "DEPRECATED"
	// FlagConfidential marks a note whose content is not for casual readers. It
	// is display-only in this pass — the red stamp and list badge — and carries
	// no search behavior.
	FlagConfidential Flag = "CONFIDENTIAL"
)

// KnownFlags returns the closed set of flags in canonical (sorted) order.
func KnownFlags() []Flag {
	return []Flag{FlagDeprecated, FlagConfidential}
}

// NormalizeFlags trims and uppercases each value, rejects any value outside the
// closed set (the error names the offender), and returns the deduplicated set
// sorted into the registry's canonical order — the same order KnownFlags lists,
// so a note carrying both flags stores exactly the ADR's [DEPRECATED,
// CONFIDENTIAL]. Empty input returns nil, so an empty set stays omitted from the
// sidecar rather than appearing as "flags: []".
func NormalizeFlags(in []string) ([]Flag, error) {
	if len(in) == 0 {
		return nil, nil
	}
	known := KnownFlags()
	order := make(map[Flag]int, len(known))
	for i, k := range known {
		order[k] = i
	}
	var out []Flag
	seen := make(map[Flag]bool, len(in))
	for _, raw := range in {
		f := Flag(strings.ToUpper(strings.TrimSpace(raw)))
		if _, ok := order[f]; !ok {
			names := make([]string, len(known))
			for i, k := range known {
				names[i] = string(k)
			}
			return nil, fmt.Errorf("unknown flag %q (want one of: %s)", raw, strings.Join(names, ", "))
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Flag) int { return cmp.Compare(order[a], order[b]) })
	return out, nil
}

// FlagStrings renders a normalized flag set as its string forms for the sidecar
// and API payloads, returning nil for an empty set so the field stays omitted.
func FlagStrings(flags []Flag) []string {
	if len(flags) == 0 {
		return nil
	}
	out := make([]string, len(flags))
	for i, f := range flags {
		out[i] = string(f)
	}
	return out
}
