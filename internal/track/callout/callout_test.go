package callout

import (
	"testing"
)

func TestCalloutsRecognizesEachType(t *testing.T) {
	text := "> [!NOTE]\n> note body\n\n> [!TIP]\n> tip body\n\n> [!IMPORTANT]\n> x\n\n> [!WARNING]\n> x\n\n> [!CAUTION]\n> x\n"
	cs := Callouts(text)
	if len(cs) != 5 {
		t.Fatalf("got %d callouts, want 5: %+v", len(cs), cs)
	}
	want := []Type{Note, Tip, Important, Warning, Caution}
	for i, c := range cs {
		if c.Type != want[i] {
			t.Errorf("callout %d type = %q, want %q", i, c.Type, want[i])
		}
	}
}

func TestCalloutsCaseInsensitiveMarker(t *testing.T) {
	cs := Callouts("> [!note]\n> body\n")
	if len(cs) != 1 || cs[0].Type != Note {
		t.Fatalf("callouts = %+v", cs)
	}
}

func TestCalloutsSkipsPlainQuotes(t *testing.T) {
	text := "> An ordinary quote\n> second line\n\n> [!NOTE]\n> real callout\n"
	cs := Callouts(text)
	if len(cs) != 1 {
		t.Fatalf("got %d callouts, want 1: %+v", len(cs), cs)
	}
	if cs[0].Type != Note {
		t.Errorf("type = %q", cs[0].Type)
	}
}

func TestCalloutsSkipsFences(t *testing.T) {
	text := "```markdown\n> [!NOTE]\n> this is code, not a callout\n```\n"
	cs := Callouts(text)
	if len(cs) != 0 {
		t.Fatalf("got %d callouts inside a fence, want 0: %+v", len(cs), cs)
	}
}

func TestCalloutsLineRange(t *testing.T) {
	text := "preamble\n> [!WARNING]\n> careful\n>\n> really\n\ntrailing\n"
	cs := Callouts(text)
	if len(cs) != 1 {
		t.Fatalf("got %d callouts, want 1: %+v", len(cs), cs)
	}
	c := cs[0]
	if c.StartLine != 1 || c.EndLine != 4 {
		t.Errorf("range = [%d,%d], want [1,4]", c.StartLine, c.EndLine)
	}
	if c.Type != Warning {
		t.Errorf("type = %q", c.Type)
	}
}

func TestCalloutsUnknownTypeIsNotCallout(t *testing.T) {
	text := "> [!FOO]\n> not a callout"
	if cs := Callouts(text); len(cs) != 0 {
		t.Fatalf("got %+v, want none", cs)
	}
}

func TestCalloutsMarkerInlineWithText(t *testing.T) {
	text := "> [!NOTE] A note on the same line\n> body"
	cs := Callouts(text)
	if len(cs) != 1 || cs[0].Type != Note {
		t.Fatalf("callouts = %+v", cs)
	}
}

func TestTypeFromMarker(t *testing.T) {
	cases := map[string]Type{
		"note":      Note,
		"NOTE":      Note,
		"tip":       Tip,
		"important": Important,
		"warning":   Warning,
		"caution":   Caution,
		"foo":       "",
		"":          "",
	}
	for in, want := range cases {
		if got := TypeFromMarker(in); got != want {
			t.Errorf("TypeFromMarker(%q) = %q, want %q", in, got, want)
		}
	}
}
