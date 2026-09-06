package elem

import (
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

func TestClampRedactsSecrets(t *testing.T) {
	raw := []byte(`{
		"page": {"sanitizedUrl": "https://example.com/done?access_token=abc123#frag", "capturedAt": "2026-05-04T09:30:00Z"},
		"target": {
			"tagName": "button",
			"selector": "button#buy",
			"textSnippet": "Buy now",
			"attributes": {
				"id": "buy",
				"href": "https://example.com/oauth?code=secret&state=xyz",
				"onclick": "alert(1)",
				"data-api-key": "tok123",
				"aria-label": "buy button"
			},
			"accessibility": {"accessibleName": "Buy now"},
			"computedStyles": {"display": "block", "color": "rgb(0,0,0)"}
		}
	}`)
	el, err := Clamp(raw, DefaultBudget)
	if err != nil {
		t.Fatal(err)
	}
	if el.URL != "https://example.com/done" {
		t.Errorf("URL = %q, want query/fragment stripped", el.URL)
	}
	md := el.Markdown

	for _, want := range []string{
		"Buy now",
		"**Selector:**",
		"**Attributes:**",
		"[Source](https://example.com/done)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	// unsafe attribute dropped, secret attribute redacted, aria kept
	if strings.Contains(md, "onclick") {
		t.Errorf("onclick (unsafe attribute) should be dropped:\n%s", md)
	}
	if !strings.Contains(md, "aria-label") {
		t.Errorf("aria-label should be kept:\n%s", md)
	}
	if !strings.Contains(md, "[redacted]") {
		t.Errorf("secret data-api-key should be redacted:\n%s", md)
	}
	// computed style noise dropped: display:inline would be, but block is real
	if !strings.Contains(md, "display: block") {
		t.Errorf("display: block should be kept:\n%s", md)
	}
}

func TestClampBudgets(t *testing.T) {
	long := strings.Repeat("x", 500)
	raw := []byte(`{
		"page": {"sanitizedUrl": "https://example.com/a"},
		"target": {"tagName": "div", "textSnippet": "` + long + `", "htmlSnippet": "` + long + `"},
		"nearbyText": ["` + long + `", "two"]
	}`)
	el, err := Clamp(raw, DefaultBudget)
	if err != nil {
		t.Fatal(err)
	}
	// oversized text truncated to budget with marker
	if !strings.Contains(el.Markdown, "(truncated)") {
		t.Errorf("oversized text should be truncated:\n%s", el.Markdown)
	}
	// title derived from snippet stays a short summary, not a bounded-trim quote
	if len(el.Title) > 80 {
		t.Errorf("title too long: %d (%q)", len(el.Title), el.Title)
	}
}

func TestRecord(t *testing.T) {
	fetched := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	el := Element{
		Title:    `button "Buy now"`,
		URL:      "https://example.com/p",
		Captured: time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC),
		TagName:  "button",
		Selector: "button#buy",
		Markdown: "**Text:** Buy now",
	}
	rec, err := Record(el, fetched)
	if err != nil {
		t.Fatal(err)
	}
	if rec["time"] != "2026-05-04T09:30:00Z" || rec["title"] != `button "Buy now"` || rec["url"] != "https://example.com/p" {
		t.Fatalf("record = %v", rec)
	}
	if rec["markdown"] != "**Text:** Buy now" || rec["selector"] != "button#buy" || rec["version"] != dataset.SchemaVersion {
		t.Fatalf("record = %v", rec)
	}

	// No captured timestamp → fetch time; empty title → tag name.
	el2 := Element{TagName: "div", Markdown: "x"}
	rec, err = Record(el2, fetched)
	if err != nil {
		t.Fatal(err)
	}
	if rec["time"] != "2026-07-01T12:00:00Z" || rec["title"] != "div" {
		t.Fatalf("record = %v", rec)
	}

	// No tag, no URL cannot make a valid event record.
	if _, err := Record(Element{Markdown: "x"}, fetched); err == nil {
		t.Fatal("expected validation error for a titleless, urlless, tagless clip")
	}
}

func TestMalformedPayload(t *testing.T) {
	if _, err := Clamp([]byte(`{not json`), DefaultBudget); err == nil {
		t.Fatal("expected parse error")
	}
}
