package provenance

import "testing"

func TestSelect(t *testing.T) {
	for _, tt := range []struct {
		name     string
		body     string
		position Position
		want     Selection
	}{
		{"whole", "intro\n\ntext ^id\n", Position{}, Selection{Body: "intro\n\ntext ^id\n", StartLine: 1, EndLine: 3}},
		{"empty", "", Position{}, Selection{}},
		{"blank", "\n", Position{StartLine: 1, EndLine: 1}, Selection{Body: "\n", StartLine: 1, EndLine: 1}},
		{"trailing blank", "one\n\n", Position{StartLine: 2, EndLine: 2}, Selection{Body: "\n", StartLine: 2, EndLine: 2}},
		{"line range", "one\ntwo\nthree", Position{StartLine: 2, EndLine: 3}, Selection{Body: "two\nthree", StartLine: 2, EndLine: 3}},
		{"CRLF lines", "one\r\ntwo\r\n", Position{StartLine: 2, EndLine: 2}, Selection{Body: "two\r\n", StartLine: 2, EndLine: 2}},
		{"heading section", "intro\n## Evidence\ntext\n### Detail\nmore\n\n## Next\nend", Position{Heading: "Evidence"}, Selection{Body: "## Evidence\ntext\n### Detail\nmore\n\n", StartLine: 2, EndLine: 6}},
		{"heading level", "# Same\nfirst\n## Same\nsecond\n", Position{Heading: "Same", Level: 2}, Selection{Body: "## Same\nsecond\n", StartLine: 3, EndLine: 4}},
		{"heading CRLF", "intro\r\n## Evidence ##\r\ntext\r\n", Position{Heading: "Evidence"}, Selection{Body: "## Evidence ##\r\ntext\r\n", StartLine: 2, EndLine: 3}},
		{"fenced headings", "```md\n# Evidence\n```\n# Evidence\n```\n# False end\n```\ntext\n# End\n", Position{Heading: "Evidence"}, Selection{Body: "# Evidence\n```\n# False end\n```\ntext\n", StartLine: 4, EndLine: 8}},
		{"paragraph block", "intro\n\nfirst\nsecond ^proof\nthird\n\nend", Position{Block: "proof"}, Selection{Body: "first\nsecond ^proof\nthird\n", StartLine: 3, EndLine: 5}},
		{"list block", "- first\n- chosen ^proof\n  continuation\n- last\n", Position{Block: "proof"}, Selection{Body: "- chosen ^proof\n  continuation\n", StartLine: 2, EndLine: 3}},
		{"CRLF block", "first\r\nsecond ^proof\r\n\r\n", Position{Block: "proof"}, Selection{Body: "first\r\nsecond ^proof\r\n", StartLine: 1, EndLine: 2}},
		{"fenced block", "```\nfalse ^proof\n```\n\ntrue ^proof\n", Position{Block: "proof"}, Selection{Body: "true ^proof\n", StartLine: 5, EndLine: 5}},
		{"physical page", "printed ix\n\fprinted 1\ncontent\n\fprinted 2", Position{Page: 2}, Selection{Body: "printed 1\ncontent\n", StartLine: 2, EndLine: 3, Page: 2}},
		{"page within line", "first\fsecond\fthird", Position{Page: 2}, Selection{Body: "second", StartLine: 1, EndLine: 1, Page: 2}},
		{"single terminated page", "one\f", Position{Page: 1}, Selection{Body: "one", StartLine: 1, EndLine: 1, Page: 1}},
		{"note newline after terminator", "one\f\n", Position{Page: 1}, Selection{Body: "one", StartLine: 1, EndLine: 1, Page: 1}},
		{"blank last physical page", "one\f\f", Position{Page: 2}, Selection{Page: 2}},
		{"first page", "first\fsecond", Position{Page: 1}, Selection{Body: "first", StartLine: 1, EndLine: 1, Page: 1}},
		{"empty page", "first\f\fthird", Position{Page: 2}, Selection{Page: 2}},
		{"full keeps page breaks", "one\f\ntwo\f", Position{}, Selection{Body: "one\f\ntwo\f", StartLine: 1, EndLine: 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(tt.body, tt.position)
			if err != nil || got != tt.want {
				t.Fatalf("Select() = %#v, %v; want %#v", got, err, tt.want)
			}
		})
	}
}

func TestSelectRejectsInvalidPosition(t *testing.T) {
	for _, tt := range []struct {
		name     string
		body     string
		position Position
	}{
		{"missing heading", "# Found\n", Position{Heading: "Missing"}},
		{"wrong level has no fallback", "## Found\n", Position{Heading: "Found", Level: 1}},
		{"duplicate heading", "# Same\n## Same\n", Position{Heading: "Same"}},
		{"duplicate heading level", "## Same\n## Same\n", Position{Heading: "Same", Level: 2}},
		{"fenced heading absent", "```\n# Hidden\n```", Position{Heading: "Hidden"}},
		{"missing block", "text ^found", Position{Block: "missing"}},
		{"duplicate block", "one ^same\n\ntwo ^same", Position{Block: "same"}},
		{"detached marker", "text\n\n^id", Position{Block: "id"}},
		{"level without heading", "# Text", Position{Level: 1}},
		{"negative level", "# Text", Position{Heading: "Text", Level: -1}},
		{"excessive level", "# Text", Position{Heading: "Text", Level: 7}},
		{"conflicting anchors", "# Text\n\nbody ^id", Position{Heading: "Text", Block: "id"}},
		{"conflicting page and line", "one\ftwo", Position{Page: 1, StartLine: 1, EndLine: 1}},
		{"negative page", "one\ftwo", Position{Page: -1}},
		{"unpaginated page one", "text", Position{Page: 1}},
		{"no phantom final page", "one\f\n", Position{Page: 2}},
		{"missing page", "one\ftwo", Position{Page: 3}},
		{"printed labels not boundaries", "Page 1\ntext\nPage 2\ntext", Position{Page: 2}},
		{"empty body line", "", Position{StartLine: 1, EndLine: 1}},
		{"no phantom trailing line", "one\n", Position{StartLine: 2, EndLine: 2}},
		{"negative start", "one", Position{StartLine: -1, EndLine: 1}},
		{"negative end", "one", Position{StartLine: 1, EndLine: -1}},
		{"start required", "one", Position{EndLine: 1}},
		{"end required", "one", Position{StartLine: 1}},
		{"reversed range", "one\ntwo", Position{StartLine: 2, EndLine: 1}},
		{"bounds never clipped", "one", Position{StartLine: 1, EndLine: 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(tt.body, tt.position)
			if err == nil || got != (Selection{}) {
				t.Fatalf("Select() = %#v, %v; want empty result and error", got, err)
			}
		})
	}
}
