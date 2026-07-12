package link

import (
	"strings"
	"testing"
)

func TestSplitAnchor(t *testing.T) {
	key, heading, level := SplitAnchor("Note##設計")
	if key != "Note" || heading != "設計" || level != 2 {
		t.Fatalf("got %q %q %d", key, heading, level)
	}
	key, heading, level = SplitAnchor("Plain")
	if key != "Plain" || heading != "" || level != 0 {
		t.Fatalf("got %q %q %d", key, heading, level)
	}
	// A trailing "#" with no heading text stays part of the key.
	key, heading, _ = SplitAnchor("C#")
	if key != "C#" || heading != "" {
		t.Fatalf("got %q %q", key, heading)
	}
}

func TestResolveAnchor(t *testing.T) {
	body := "# Top\n\n## Tasks\n- a\n\n## Done\n\n### Tasks\ntext\n"

	// Exact level+text match wins.
	h, err := ResolveAnchor(body, "Tasks", 2)
	if err != nil || h.Line != 2 {
		t.Fatalf("exact: h=%+v err=%v", h, err)
	}
	// No exact match at that level falls back to a unique text match.
	h, err = ResolveAnchor(body, "Done", 1)
	if err != nil || h.Line != 5 || h.Level != 2 {
		t.Fatalf("fallback: h=%+v err=%v", h, err)
	}
	// Two same-level candidates refuse.
	dup := "## X\n\n## X\n"
	if _, err := ResolveAnchor(dup, "X", 2); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	// Text-only fallback with several candidates at different levels refuses too.
	if _, err := ResolveAnchor(body, "Tasks", 4); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	if _, err := ResolveAnchor(body, "Missing", 1); err == nil {
		t.Fatal("expected not-found error")
	}
	// Headings inside fenced code blocks are not anchor candidates.
	fenced := "```\n## X\n```\n## X\n"
	h, err = ResolveAnchor(fenced, "X", 2)
	if err != nil || h.Line != 3 {
		t.Fatalf("fenced: h=%+v err=%v", h, err)
	}
}

func TestCutSectionMovesNestedHeadings(t *testing.T) {
	body := "# Top\n\n## Move\nline [[Other]]\n\n### Child\nnested\n\n## Keep\ntail\n"
	h, err := ResolveAnchor(body, "Move", 2)
	if err != nil {
		t.Fatal(err)
	}
	rest, section := CutSection(body, h)
	wantSection := []string{"## Move", "line [[Other]]", "", "### Child", "nested"}
	if strings.Join(section, "\n") != strings.Join(wantSection, "\n") {
		t.Fatalf("section:\n%q", section)
	}
	if rest != "# Top\n\n## Keep\ntail\n" {
		t.Fatalf("rest:\n%q", rest)
	}
}

func TestCutSectionAtEndOfFile(t *testing.T) {
	body := "## A\na\n\n## B\nb\n"
	h, _ := ResolveAnchor(body, "B", 2)
	rest, section := CutSection(body, h)
	if strings.Join(section, "\n") != "## B\nb" {
		t.Fatalf("section: %q", section)
	}
	if rest != "## A\na\n" {
		t.Fatalf("rest: %q", rest)
	}
}

func TestCutListItem(t *testing.T) {
	body := "# T\n\n- keep\n- move [[Ref]]\n  - child\n\n  more child\n- after\n"

	rest, item, err := CutListItem(body, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"- move [[Ref]]", "  - child", "", "  more child"}
	if strings.Join(item, "\n") != strings.Join(want, "\n") {
		t.Fatalf("item: %q", item)
	}
	if rest != "# T\n\n- keep\n- after\n" {
		t.Fatalf("rest: %q", rest)
	}

	// A trailing blank line stays with the source when nothing deeper follows.
	body2 := "- solo\n\n# Next\n"
	rest, item, err = CutListItem(body2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(item) != 1 || item[0] != "- solo" {
		t.Fatalf("item: %q", item)
	}
	if rest != "\n# Next\n" {
		t.Fatalf("rest: %q", rest)
	}

	if _, _, err := CutListItem(body, 1); err == nil {
		t.Fatal("expected error for a non-list line")
	}
	if _, _, err := CutListItem(body, 99); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestAppendUnderSection(t *testing.T) {
	body := "# Inbox\n\n## Tasks\n- [ ] old\n\n## Notes\ntext\n"
	h, _ := ResolveAnchor(body, "Tasks", 2)

	// List items pack: no blank line between old and new entries.
	got, at := AppendUnder(body, &h, []string{"- [ ] new"})
	if got != "# Inbox\n\n## Tasks\n- [ ] old\n- [ ] new\n\n## Notes\ntext\n" {
		t.Fatalf("got:\n%q", got)
	}
	if at != 5 {
		t.Fatalf("at=%d", at)
	}

	// A paragraph gets a blank separator, plus one before the following heading when needed.
	got, at = AppendUnder("## A\ntext\n## B\n", &Heading{Level: 2, Text: "A", Line: 0}, []string{"para"})
	if got != "## A\ntext\n\npara\n\n## B\n" {
		t.Fatalf("got:\n%q", got)
	}
	if at != 4 {
		t.Fatalf("at=%d", at)
	}
}

func TestAppendUnderEndOfNote(t *testing.T) {
	got, at := AppendUnder("# T\nbody\n", nil, []string{"", "new", ""})
	if got != "# T\nbody\n\nnew\n" || at != 4 {
		t.Fatalf("got %q at=%d", got, at)
	}
	// A blank block is a no-op signalled by line 0.
	if _, at := AppendUnder("# T\n", nil, []string{"", ""}); at != 0 {
		t.Fatalf("blank block: at=%d", at)
	}
	// Appending to an empty body just writes the block.
	got, at = AppendUnder("", nil, []string{"solo"})
	if got != "solo\n" || at != 1 {
		t.Fatalf("empty body: %q at=%d", got, at)
	}
}

func TestAppendUnderEmptySection(t *testing.T) {
	body := "## Inbox\n\n## Next\nx\n"
	h, _ := ResolveAnchor(body, "Inbox", 2)
	got, at := AppendUnder(body, &h, []string{"- entry"})
	if got != "## Inbox\n\n- entry\n\n## Next\nx\n" || at != 3 {
		t.Fatalf("got %q at=%d", got, at)
	}
}
