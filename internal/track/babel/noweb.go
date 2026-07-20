package babel

import (
	"fmt"
	"regexp"
	"strings"
)

// Noweb reference expansion (docs/spec/babel.md): "<<name>>" inside a block body is replaced by the
// body of the named block, recursively. Expansion is opt-in per block through :noweb, matching Org:
// "yes" expands everywhere, "eval" only before execution, "tangle" only before tangling, anything
// else (including the default "no") never expands.

var (
	// nowebLineRef matches a reference standing alone on a line; its leading whitespace is re-applied
	// to every expanded line, so an indented reference keeps the surrounding code's indentation.
	nowebLineRef = regexp.MustCompile(`^([ \t]*)<<([^<>\s]+)>>[ \t]*$`)
	// nowebInlineRef matches a reference embedded in a longer line; it is spliced in verbatim.
	nowebInlineRef = regexp.MustCompile(`<<([^<>\s]+)>>`)
)

// NowebExpands reports whether a block's :noweb header asks for expansion in the given phase
// ("eval" or "tangle").
func NowebExpands(b Block, phase string) bool {
	v := firstValue(b.HeaderArgs, "noweb")
	return v == "yes" || v == phase
}

// ExpandNoweb expands every <<name>> reference in body against the note's named blocks, recursively.
// An unresolved reference or a reference cycle is an error naming the offending chain.
func ExpandNoweb(body string, blocks []Block) (string, error) {
	byName := make(map[string]Block)
	for _, b := range blocks {
		if b.Name != "" {
			byName[b.Name] = b
		}
	}
	return expandNoweb(body, byName, nil)
}

func expandNoweb(body string, byName map[string]Block, stack []string) (string, error) {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if m := nowebLineRef.FindStringSubmatch(line); m != nil {
			exp, err := expandRef(m[2], byName, stack)
			if err != nil {
				return "", err
			}
			for _, el := range strings.Split(exp, "\n") {
				out = append(out, m[1]+el)
			}
			continue
		}
		var refErr error
		line = nowebInlineRef.ReplaceAllStringFunc(line, func(match string) string {
			name := nowebInlineRef.FindStringSubmatch(match)[1]
			exp, err := expandRef(name, byName, stack)
			if err != nil {
				refErr = err
				return match
			}
			return exp
		})
		if refErr != nil {
			return "", refErr
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), nil
}

// expandRef resolves one reference and expands its body, tracking the reference chain for cycles.
func expandRef(name string, byName map[string]Block, stack []string) (string, error) {
	for _, s := range stack {
		if s == name {
			return "", fmt.Errorf("noweb cycle: %s", strings.Join(append(stack, name), " -> "))
		}
	}
	b, ok := byName[name]
	if !ok {
		return "", fmt.Errorf("noweb reference <<%s>> does not match any named block", name)
	}
	return expandNoweb(b.Body, byName, append(stack, name))
}
