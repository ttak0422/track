package site

import (
	"reflect"
	"strings"
	"testing"
)

func TestRewriteAssetRefsLeavesCodeExamples(t *testing.T) {
	body := strings.Join([]string{
		"![real](assets/pic.png)",
		"",
		"Inline `![](assets/inline.mmd)` stays literal.",
		"",
		"```markdown",
		"![](assets/gantt.mmd)",
		"```",
	}, "\n")

	got := rewriteAssetRefs(body)

	// The real embed is rewritten to its published slug name.
	want := "assets/" + publishAssetName("pic.png")
	if !strings.Contains(got, want) {
		t.Fatalf("real embed not rewritten: %q", got)
	}
	// The fenced and inline examples are preserved exactly.
	if !strings.Contains(got, "![](assets/gantt.mmd)") {
		t.Fatalf("fenced example was rewritten: %q", got)
	}
	if !strings.Contains(got, "![](assets/inline.mmd)") {
		t.Fatalf("inline example was rewritten: %q", got)
	}
}

func TestCollectAssetsIgnoresCodeExamples(t *testing.T) {
	body := strings.Join([]string{
		"![real](assets/pic.png)",
		"`assets/inline.mmd`",
		"```",
		"assets/fenced.mmd",
		"```",
	}, "\n")

	got := collectAssets(body)
	want := []string{"pic.png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectAssets = %v, want %v", got, want)
	}
}

func TestMaskCodePreservesOffsetsAndNewlines(t *testing.T) {
	body := "a `b` c\n```\nd\n```\ne"
	masked := maskCode(body)
	if len(masked) != len(body) {
		t.Fatalf("mask changed length: %d != %d", len(masked), len(body))
	}
	for i := range body {
		if body[i] == '\n' && masked[i] != '\n' {
			t.Fatalf("newline at %d not preserved", i)
		}
	}
	// The inline span (`b`) and the fenced block (the ``` lines and d) are blanked, the rest kept.
	if strings.Contains(masked, "b") || strings.Contains(masked, "d") || strings.Contains(masked, "`") {
		t.Fatalf("code not fully masked: %q", masked)
	}
	if !strings.HasPrefix(masked, "a ") || !strings.HasSuffix(masked, "\ne") {
		t.Fatalf("non-code text not preserved: %q", masked)
	}
}

func TestMaskCodeUnterminatedBackticksAreLiteral(t *testing.T) {
	body := "see assets/keep.png ` not closed"
	masked := maskCode(body)
	// A lone backtick does not open a span, so the reference stays visible to the scanner.
	if !strings.Contains(masked, "assets/keep.png") {
		t.Fatalf("unterminated backtick masked real text: %q", masked)
	}
}
