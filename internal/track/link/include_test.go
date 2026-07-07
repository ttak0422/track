package link

import (
	"reflect"
	"strings"
	"testing"
)

func TestIncludesParsesDirectives(t *testing.T) {
	text := strings.Join([]string{
		"prose with an inline ![[NotAnInclude]] embed", // not block-level
		"![[Note]]", // whole note
		"  ![[Note##設計|抜粋]] :only-contents :lines 4-5,8", // indented, anchored, aliased, options
		"```",
		"![[Fenced]]",
		"```",
		"![[Typo]] :nope :lines x-y",
	}, "\n")

	incs := Includes(text)
	if len(incs) != 3 {
		t.Fatalf("want 3 includes, got %d: %+v", len(incs), incs)
	}

	if incs[0].Line != 1 || incs[0].Text != "Note" || incs[0].Heading != "" {
		t.Errorf("whole-note include parsed wrong: %+v", incs[0])
	}

	sec := incs[1]
	if sec.Line != 2 || sec.Text != "Note" || sec.Heading != "設計" || sec.HeadingLevel != 2 {
		t.Errorf("anchored include parsed wrong: %+v", sec)
	}
	if sec.Display != "抜粋" || !sec.OnlyContents {
		t.Errorf("alias/only-contents parsed wrong: %+v", sec)
	}
	if want := []LineRange{{4, 5}, {8, 8}}; !reflect.DeepEqual(sec.Lines, want) {
		t.Errorf("lines ranges = %+v, want %+v", sec.Lines, want)
	}
	if len(sec.BadOptions) != 0 {
		t.Errorf("unexpected bad options: %v", sec.BadOptions)
	}

	bad := incs[2]
	if want := []string{":nope", ":lines x-y"}; !reflect.DeepEqual(bad.BadOptions, want) {
		t.Errorf("bad options = %v, want %v", bad.BadOptions, want)
	}
	if len(bad.Lines) != 0 {
		t.Errorf("malformed :lines must not yield ranges: %+v", bad.Lines)
	}
}

func TestIncludeOffsetsMatchRefs(t *testing.T) {
	text := "  ![[Note##設計]] :only-contents"
	inc := Includes(text)[0]
	ref := Refs(text)[0]
	if inc.OpenByte != ref.OpenByte || inc.StartByte != ref.StartByte ||
		inc.EndByte != ref.EndByte || inc.CloseByte != ref.CloseByte {
		t.Errorf("include offsets %+v disagree with Refs %+v", inc.Ref, ref)
	}
}

const includeBody = `intro line

## 設計
first
second

` + "```" + `
## 設計 in fence
` + "```" + `
third

## 実装
impl line`

func TestExtractWholeNote(t *testing.T) {
	got, ok := Extract(includeBody, Include{})
	if !ok {
		t.Fatal("whole-note extract must succeed")
	}
	if got[0] != "intro line" || got[len(got)-1] != "impl line" {
		t.Errorf("whole note trimmed wrong: %q", got)
	}
}

func TestExtractSectionStopsAtNextHeading(t *testing.T) {
	inc := Includes("![[X##設計]]")[0]
	got, ok := Extract(includeBody, inc)
	if !ok {
		t.Fatal("section extract must succeed")
	}
	want := []string{"## 設計", "first", "second", "", "```", "## 設計 in fence", "```", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("section = %q, want %q", got, want)
	}
}

func TestExtractOnlyContentsAndLines(t *testing.T) {
	inc := Includes("![[X##設計]] :only-contents :lines 1-2,99")[0]
	got, ok := Extract(includeBody, inc)
	if !ok {
		t.Fatal("extract must succeed")
	}
	// Region after dropping the heading: first, second, ... — :lines 1-2 picks the first two,
	// the out-of-range 99 is clipped away.
	if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractMissingHeadingFails(t *testing.T) {
	inc := Includes("![[X###どこにもない]]")[0]
	if _, ok := Extract(includeBody, inc); ok {
		t.Error("missing heading must not fall back to the whole note")
	}
}
