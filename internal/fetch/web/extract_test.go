package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// semanticMarkdown is the expected conversion of testdata/semantic.html: the h1 (repeating the
// title) dropped, chrome pruned, and every supported construct rendered.
const semanticMarkdown = `Container gardening rewards small, steady adjustments. A pot dries faster than a bed, so the watering rhythm matters more than the total volume. This first season was mostly about learning that rhythm.

## What worked

Three habits made the biggest difference, and none of them cost anything. The **morning check** caught problems a day earlier than an evening one.

- Water at the soil line, not over the leaves
- Rotate each pot a quarter turn *every week*
- Feed lightly but often

> A plant tells you what it needs; the trick is checking often enough to hear it.

## Tracking growth

Heights went into a plain text log, one line per reading, parsed later with a short script. The full log format is described in [a companion note](https://example.com/notes/log-format).

` + "```python" + `
def week_of(reading):
    return reading.date.isocalendar().week
` + "```" + `

![Six pots on a balcony rail](https://example.com/images/pots.jpg)

*The balcony arrangement in early summer*

| Week | Height (cm) |
| --- | --- |
| 1 | 4 |
| 4 | 19 |`

func extractFixture(t *testing.T, name, base string) Page {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var pageURL *url.URL
	if base != "" {
		pageURL, err = url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := Extract(f, pageURL)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		base      string
		title     string
		published string // RFC 3339, "" for none declared
		image     string
		markdown  string   // exact match when non-empty
		contains  []string // substring checks on the markdown
		excludes  []string // chrome that must have been pruned
	}{
		{
			name:      "semantic article with og metadata",
			fixture:   "semantic.html",
			base:      "https://example.com/posts/gardening",
			title:     "Field Notes on Container Gardening",
			published: "2026-05-04T09:30:00Z",
			image:     "https://example.com/images/lead.jpg",
			markdown:  semanticMarkdown,
			excludes:  []string{"Archive", "Related reading", "Placeholder Site", "window.analytics"},
		},
		{
			name:      "div soup scored by paragraph mass",
			fixture:   "soup.html",
			base:      "https://example.com/posts/kettle",
			title:     "Choosing a Kettle That Lasts",
			published: "2026-02-11T00:00:00Z", // from <time datetime>
			image:     "https://example.com/posts/images/kettle.jpg",
			contains: []string{
				"durability the only specification",
				// relative link and image resolved against the page URL
				"[replacement hinges](https://example.com/posts/parts/hinge)",
				"![A stainless steel kettle](https://example.com/posts/images/kettle.jpg)",
			},
			excludes: []string{"Newsletter", "Privacy", "/tags/kitchen"},
		},
		{
			name:     "minimal page without metadata",
			fixture:  "minimal.html",
			base:     "",
			title:    "Bare Page",
			markdown: "One short paragraph, nothing else worth keeping on this page.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := extractFixture(t, tt.fixture, tt.base)
			if p.Title != tt.title {
				t.Errorf("title = %q, want %q", p.Title, tt.title)
			}
			if got := formatPublished(p.Published); got != tt.published {
				t.Errorf("published = %q, want %q", got, tt.published)
			}
			if p.Image != tt.image {
				t.Errorf("image = %q, want %q", p.Image, tt.image)
			}
			if tt.markdown != "" && p.Markdown != tt.markdown {
				t.Errorf("markdown:\n%s\nwant:\n%s", p.Markdown, tt.markdown)
			}
			for _, want := range tt.contains {
				if !strings.Contains(p.Markdown, want) {
					t.Errorf("markdown missing %q:\n%s", want, p.Markdown)
				}
			}
			for _, junk := range tt.excludes {
				if strings.Contains(p.Markdown, junk) {
					t.Errorf("markdown kept pruned content %q:\n%s", junk, p.Markdown)
				}
			}
		})
	}
}

func formatPublished(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func TestExtractStrings(t *testing.T) {
	base, _ := url.Parse("https://example.com/a/b")
	tests := []struct {
		name string
		html string
		want Page
	}{
		{
			name: "title falls back to first heading",
			html: `<body><article><h1>Heading Title</h1><p>Some paragraph text that is long enough to count as content here.</p></article></body>`,
			want: Page{Title: "Heading Title", Markdown: "Some paragraph text that is long enough to count as content here."},
		},
		{
			name: "lead image falls back to first content image",
			html: `<body><article><p>Enough paragraph text to make this article the content candidate for sure.</p><img src="/pic.png" alt="x"></article></body>`,
			want: Page{Markdown: "Enough paragraph text to make this article the content candidate for sure.\n\n![x](https://example.com/pic.png)", Image: "https://example.com/pic.png"},
		},
		{
			name: "unsafe schemes are dropped",
			html: `<body><article><p>Prose long enough to be selected as the main readable content block.</p><p><a href="javascript:alert(1)">bad link</a> and <img src="data:image/png;base64,xx" alt="bad"></p></article></body>`,
			want: Page{Markdown: "Prose long enough to be selected as the main readable content block.\n\nbad link and"},
		},
		{
			name: "ordered and nested lists",
			html: `<body><article><p>Padding paragraph so the article container passes the minimum text threshold.</p><ol><li>first</li><li>second<ul><li>inner</li></ul></li></ol></article></body>`,
			want: Page{Markdown: "Padding paragraph so the article container passes the minimum text threshold.\n\n1. first\n2. second\n  - inner"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Extract(strings.NewReader(tt.html), base)
			if err != nil {
				t.Fatal(err)
			}
			if p.Markdown != tt.want.Markdown {
				t.Errorf("markdown:\n%q\nwant:\n%q", p.Markdown, tt.want.Markdown)
			}
			if tt.want.Title != "" && p.Title != tt.want.Title {
				t.Errorf("title = %q, want %q", p.Title, tt.want.Title)
			}
			if tt.want.Image != "" && p.Image != tt.want.Image {
				t.Errorf("image = %q, want %q", p.Image, tt.want.Image)
			}
		})
	}
}

func TestRecord(t *testing.T) {
	fetched := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	published := time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC)

	rec, err := Record(Page{Title: "A Page", Published: published, Image: "https://example.com/i.jpg", Markdown: "Body."},
		"https://example.com/a", fetched)
	if err != nil {
		t.Fatal(err)
	}
	if rec["time"] != "2026-05-04T09:30:00Z" || rec["title"] != "A Page" || rec["url"] != "https://example.com/a" {
		t.Fatalf("record = %v", rec)
	}
	if rec["markdown"] != "Body." || rec["image"] != "https://example.com/i.jpg" || rec["version"] != dataset.SchemaVersion {
		t.Fatalf("record = %v", rec)
	}

	// No published time → fetch time; no title → source URL; empty extras omitted.
	rec, err = Record(Page{}, "https://example.com/b", fetched)
	if err != nil {
		t.Fatal(err)
	}
	if rec["time"] != "2026-07-01T12:00:00Z" || rec["title"] != "https://example.com/b" {
		t.Fatalf("record = %v", rec)
	}
	if _, ok := rec["markdown"]; ok {
		t.Fatalf("empty markdown should be omitted: %v", rec)
	}

	// No title and no URL cannot make a valid event record.
	if _, err := Record(Page{Markdown: "x"}, "", fetched); err == nil {
		t.Fatal("expected validation error for a titleless, urlless clip")
	}
}

func TestNoteBody(t *testing.T) {
	fetched := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	got := NoteBody(Page{Image: "https://example.com/i.jpg", Markdown: "Body."}, "https://example.com/a", fetched)
	want := "[Source](https://example.com/a) — clipped 2026-07-01\n\n![](https://example.com/i.jpg)\n\nBody.\n"
	if got != want {
		t.Errorf("note body = %q, want %q", got, want)
	}
	if got := NoteBody(Page{Markdown: "Body."}, "", fetched); got != "Clipped 2026-07-01\n\nBody.\n" {
		t.Errorf("note body without source = %q", got)
	}
}
