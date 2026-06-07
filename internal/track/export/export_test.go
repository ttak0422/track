package export

import (
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/babel"
	"github.com/ttak0422/track/internal/track/note"
)

func exportMarkdown(t *testing.T, n *note.Note, opts Options) Result {
	t.Helper()
	res, err := Export(n, NewMarkdownRenderer(), opts)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return res
}

func TestExportFlattensWikiLinks(t *testing.T) {
	n := &note.Note{ID: 1, Body: "see [[Go]] and [[Go|ゴー]] and [[note#heading]] and [[C#]]\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "see Go and ゴー and note and C#\n"
	if got != want {
		t.Fatalf("wiki flatten mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportRemovesActionLinks(t *testing.T) {
	n := &note.Note{ID: 1, Body: "[今日](<journal?offset=0>) と [会議](<note?template=mtg&title=x>) と <journal?offset=-1> ここ\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "今日 と 会議 と  ここ\n"
	if got != want {
		t.Fatalf("action link mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportKeepsPlainMarkdownLink(t *testing.T) {
	n := &note.Note{ID: 1, Body: "see [docs](http://example.com) here\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "see [docs](http://example.com) here\n"
	if got != want {
		t.Fatalf("plain link should be preserved:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportKeepsHeadings(t *testing.T) {
	n := &note.Note{ID: 1, Body: "# Title\n\n## Section\n\ntext\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "# Title\n\n## Section\n\ntext\n"
	if got != want {
		t.Fatalf("headings should pass through:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportBabelCodeDefaultStripsHeaderArgs(t *testing.T) {
	n := &note.Note{ID: 1, Body: "```lua :name hi :results output\nprint(1)\n```\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "```lua\nprint(1)\n```\n"
	if got != want {
		t.Fatalf("babel code mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportBabelNoneRemovesBlock(t *testing.T) {
	n := &note.Note{ID: 1, Body: "before\n\n```lua :exports none\nprint(1)\n```\n\nafter\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	if strings.Contains(got, "print(1)") || strings.Contains(got, "```") {
		t.Fatalf(":exports none should drop the block, got %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("surrounding text should remain, got %q", got)
	}
}

func TestExportBabelResultsFromMetadata(t *testing.T) {
	n := &note.Note{
		ID:   1,
		Body: "```lua :name hi :exports results\nprint(1)\n```\n",
		Meta: note.Metadata{
			Version: note.MetadataVersionV2,
			Blocks: map[string]babel.BlockMeta{
				"hi": {Language: "lua", LastRun: &babel.RunResult{Stdout: "1"}},
			},
		},
	}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "```\n1\n```\n"
	if got != want {
		t.Fatalf("results-only mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportBabelBothEmitsSourceAndResults(t *testing.T) {
	n := &note.Note{
		ID:   1,
		Body: "```lua :name hi :exports both\nprint(1)\n```\n",
		Meta: note.Metadata{
			Version: note.MetadataVersionV2,
			Blocks: map[string]babel.BlockMeta{
				"hi": {Language: "lua", LastRun: &babel.RunResult{Stdout: "1"}},
			},
		},
	}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "```lua\nprint(1)\n```\n\n```\n1\n```\n"
	if got != want {
		t.Fatalf("both mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportBabelResultsMissingWarns(t *testing.T) {
	n := &note.Note{ID: 1, Body: "```lua :name hi :exports both\nprint(1)\n```\n"}
	res := exportMarkdown(t, n, Options{})
	if len(res.Warnings) != 1 {
		t.Fatalf("expected one warning for missing result, got %+v", res.Warnings)
	}
	// "both" without a stored result still emits the source.
	if !strings.Contains(res.Markdown, "print(1)") {
		t.Fatalf("source should still emit, got %q", res.Markdown)
	}
}

func TestExportPlainFencePassesThrough(t *testing.T) {
	n := &note.Note{ID: 1, Body: "```\n[[Go]] stays literal\n```\n"}
	got := exportMarkdown(t, n, Options{}).Markdown
	want := "```\n[[Go]] stays literal\n```\n"
	if got != want {
		t.Fatalf("plain fence should be verbatim:\n got: %q\nwant: %q", got, want)
	}
}

func TestExportDefaultExportsOption(t *testing.T) {
	n := &note.Note{ID: 1, Body: "```lua\nprint(1)\n```\n"}
	got := exportMarkdown(t, n, Options{DefaultExports: "none"}).Markdown
	if strings.Contains(got, "print(1)") {
		t.Fatalf("--exports-default none should drop the block, got %q", got)
	}
}

func TestExportFrontmatterDisabledByDefault(t *testing.T) {
	n := &note.Note{ID: 1, Body: "# Title\n", Meta: note.Metadata{Title: "Title", Created: "2026-06-07"}}
	got := exportMarkdown(t, n, Options{}).Markdown
	if strings.Contains(got, "---") {
		t.Fatalf("frontmatter should be off by default, got %q", got)
	}
}

func TestExportFrontmatterEnabled(t *testing.T) {
	n := &note.Note{
		ID:   1,
		Body: "# Title\n",
		Meta: note.Metadata{Title: "Title", Created: "2026-06-07", Tags: []string{"a", "b"}},
	}
	got := exportMarkdown(t, n, Options{Frontmatter: true}).Markdown
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("frontmatter should lead the output, got %q", got)
	}
	if !strings.Contains(got, "title: Title") || !strings.Contains(got, "created: \"2026-06-07\"") {
		t.Fatalf("frontmatter fields missing, got %q", got)
	}
	if !strings.Contains(got, "# Title") {
		t.Fatalf("body should follow frontmatter, got %q", got)
	}
}
