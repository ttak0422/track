package rss

import (
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/dataset"
)

const rssDoc = `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Example News</title>
    <item><title>Second</title><link>https://example.com/2</link><pubDate>Tue, 02 Jun 2026 09:30:00 +0000</pubDate></item>
    <item><title>First</title><link>https://example.com/1</link><pubDate>Mon, 01 Jun 2026 12:00:00 +0900</pubDate></item>
    <item><title>No date</title><link>https://example.com/3</link></item>
    <item><pubDate>Wed, 03 Jun 2026 00:00:00 +0000</pubDate></item>
  </channel>
</rss>`

const atomDoc = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Atom</title>
  <entry>
    <title>Entry</title>
    <link rel="alternate" href="https://example.com/e"/>
    <published>2026-06-05T10:00:00Z</published>
  </entry>
  <entry>
    <title>Updated only</title>
    <link href="https://example.com/u"/>
    <updated>2026-06-06T10:00:00Z</updated>
  </entry>
</feed>`

func TestParseRSS(t *testing.T) {
	feed, skipped, err := Parse(strings.NewReader(rssDoc))
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "Example News" {
		t.Fatalf("title = %q", feed.Title)
	}
	if len(feed.Items) != 2 || skipped != 2 {
		t.Fatalf("items = %d, skipped = %d, want 2/2: %+v", len(feed.Items), skipped, feed.Items)
	}
}

func TestParseAtom(t *testing.T) {
	feed, skipped, err := Parse(strings.NewReader(atomDoc))
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "Example Atom" || len(feed.Items) != 2 || skipped != 0 {
		t.Fatalf("feed = %+v, skipped = %d", feed, skipped)
	}
	if feed.Items[0].URL != "https://example.com/e" {
		t.Fatalf("alternate link not picked: %+v", feed.Items[0])
	}
	// published wins over updated when both could apply; updated is the fallback.
	if feed.Items[1].Time.IsZero() {
		t.Fatalf("updated fallback not parsed: %+v", feed.Items[1])
	}
}

func TestParseRejectsUnknownRoot(t *testing.T) {
	if _, _, err := Parse(strings.NewReader(`<html></html>`)); err == nil || !strings.Contains(err.Error(), "unsupported feed format") {
		t.Fatalf("want unsupported-format error, got %v", err)
	}
}

func TestEventsAreOrderedValidCanonicalRecords(t *testing.T) {
	feed, _, err := Parse(strings.NewReader(rssDoc))
	if err != nil {
		t.Fatal(err)
	}
	records, err := Events(feed.Items, "Example News")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %+v", records)
	}
	// Ascending by time: "First" (Jun 1) precedes "Second" (Jun 2), regardless of feed order.
	if records[0]["title"] != "First" || records[1]["title"] != "Second" {
		t.Fatalf("records not time-ordered: %+v", records)
	}
	first := records[0]
	if first["version"] != dataset.SchemaVersion || first["entity"] != "Example News" {
		t.Fatalf("canonical fields missing: %+v", first)
	}
	if ts, _ := first["time"].(string); !strings.HasPrefix(ts, "2026-06-01T12:00:00") {
		t.Fatalf("time not RFC3339-normalized: %v", first["time"])
	}
	if err := dataset.ValidateRecords(dataset.KindEvent, records); err != nil {
		t.Fatalf("records must validate: %v", err)
	}
}
