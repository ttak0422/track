package export

import (
	"testing"

	"github.com/ttak0422/track/internal/track/note"
)

func renderWeb(t *testing.T, body string) string {
	t.Helper()
	res, err := Export(&note.Note{ID: 1, Body: body}, NewWebRenderer(), Options{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return res.Markdown
}

func TestWebRendererKeepsWikiLinks(t *testing.T) {
	got := renderWeb(t, "see [[Go]] and [[Go|ゴー]] and [[note#heading]]\n")
	want := "see [[Go]] and [[Go|ゴー]] and [[note#heading]]\n"
	if got != want {
		t.Fatalf("wiki links should pass through verbatim:\n got: %q\nwant: %q", got, want)
	}
}

func TestWebRendererFlattensActionLinks(t *testing.T) {
	got := renderWeb(t, "[今日](<journal?offset=0>) と [会議](<note?template=mtg&title=x>) と <journal?offset=-1> ここ\n")
	want := "今日 と 会議 と  ここ\n"
	if got != want {
		t.Fatalf("action links should flatten to their label:\n got: %q\nwant: %q", got, want)
	}
}

func TestWebRendererKeepsPlainMarkdownLink(t *testing.T) {
	got := renderWeb(t, "see [docs](http://example.com) here\n")
	want := "see [docs](http://example.com) here\n"
	if got != want {
		t.Fatalf("plain link should be preserved:\n got: %q\nwant: %q", got, want)
	}
}

func TestWebRendererKeepsCodeBlock(t *testing.T) {
	got := renderWeb(t, "```go\nfmt.Println(1)\n```\n")
	want := "```go\nfmt.Println(1)\n```\n"
	if got != want {
		t.Fatalf("code block should pass through:\n got: %q\nwant: %q", got, want)
	}
}

func TestWebRendererKeepsFenceHeaderArguments(t *testing.T) {
	// Header arguments carry rendering options resolved after sanitization (e.g. a track-query
	// fence's :layout), so the web renderer must round-trip the full info string.
	got := renderWeb(t, "```track-query :layout board :by state\nTABLE title, state\n```\n")
	want := "```track-query :layout board :by state\nTABLE title, state\n```\n"
	if got != want {
		t.Fatalf("fence header arguments should pass through:\n got: %q\nwant: %q", got, want)
	}
}

func TestWebRendererDropsFrontmatter(t *testing.T) {
	n := &note.Note{ID: 1, Body: "text\n", Meta: note.Metadata{Title: "T", Tags: []string{"a"}}}
	res, err := Export(n, NewWebRenderer(), Options{Frontmatter: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.Markdown != "text\n" {
		t.Fatalf("frontmatter should be dropped for web: %q", res.Markdown)
	}
}
