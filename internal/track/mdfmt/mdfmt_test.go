package mdfmt

import "testing"

// cases pairs an input with its canonical form, one group per rule, plus fence/edge protection cases.
var cases = []struct {
	name string
	in   string
	want string
}{
	// Rule 1: strip trailing whitespace.
	{"trailing whitespace", "a   \nb\t\nc \n", "a\nb\nc\n"},
	{"trailing crlf", "a\r\nb\r\n", "a\nb\n"},

	// Rule 2: collapse blank runs, drop leading/trailing blanks.
	{"collapse blank run", "a\n\n\n\nb\n", "a\n\nb\n"},
	{"drop leading blanks", "\n\n\na\n", "a\n"},
	{"drop trailing blanks", "a\n\n\n", "a\n"},

	// Rule 3: two blank lines before headings, one after.
	{"blank around heading", "a\n# H\nb\n", "a\n\n\n# H\n\nb\n"},
	{"heading at start", "# H\nbody\n", "# H\n\nbody\n"},
	{"consecutive headings", "# A\n## B\n", "# A\n\n\n## B\n"},
	{"heading already spaced", "a\n\n\n# H\n\nb\n", "a\n\n\n# H\n\nb\n"},
	{"extra blanks before heading", "a\n\n\n\n# H\n", "a\n\n\n# H\n"},

	// Rule 4: normalize list markers to "-".
	{"star bullets", "* one\n* two\n", "- one\n- two\n"},
	{"plus bullets", "+ one\n+ two\n", "- one\n- two\n"},
	{"indented star", "  * nested\n", "  - nested\n"},
	{"emphasis not a bullet", "*bold* text\n", "*bold* text\n"},
	{"star thematic break kept", "* * *\n", "* * *\n"},

	// Rule 5: exactly one final newline.
	{"add final newline", "a", "a\n"},
	{"trim extra final newlines", "a\n\n\n", "a\n"},

	// Fence protection: code content is verbatim.
	{"fence keeps content", "```\n*  keep  \n\n\nx\n```\n", "```\n*  keep  \n\n\nx\n```\n"},
	{"tilde fence", "~~~\n*  keep  \n~~~\n", "~~~\n*  keep  \n~~~\n"},
	{"blank inside fence kept", "text\n```go\nline1\n\nline2\n```\n", "text\n```go\nline1\n\nline2\n```\n"},
	{"heading-like inside fence", "```\n# not a heading\n```\n", "```\n# not a heading\n```\n"},

	// Inline code span content is untouched (mid-line).
	{"inline code preserved", "use `* x *` here\n", "use `* x *` here\n"},

	// Edge cases.
	{"empty", "", ""},
	{"blank only", "\n\n", ""},
	{"thematic break trailing strip", "--- \n", "---\n"},
}

func TestFormatGolden(t *testing.T) {
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("%s: Format(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestFormatIdempotent is the required property: Format(Format(x)) == Format(x), checked against every
// input and every expected output.
func TestFormatIdempotent(t *testing.T) {
	for _, c := range cases {
		for _, x := range []string{c.in, c.want} {
			once := Format(x)
			twice := Format(once)
			if once != twice {
				t.Errorf("%s: not idempotent for %q: once=%q twice=%q", c.name, x, once, twice)
			}
		}
	}
}
