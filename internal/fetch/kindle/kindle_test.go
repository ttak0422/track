package kindle

import (
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// The real exports come with a UTF-8 BOM and CRLF line endings; the fixture below exercises both
// plus a trailing separator on the last block (Amazon's format).
const enClippings = "\ufeffThe Pragmatic Programmer (David Thomas)\r\n" +
	"- Your Highlight at location 234-235 | Added on Tuesday, January 13, 2015 10:33:28 AM\r\n" +
	"\r\n" +
	"2 days of work can save 2 hours of thought.\r\n" +
	"==========\r\n" +
	"The Pragmatic Programmer (David Thomas)\r\n" +
	"- Your Highlight on page 42 | location 1240-1241 | Added on Friday, January 16, 2015 9:12:01 PM\r\n" +
	"\r\n" +
	"Don't Program by Coincidence.\r\n" +
	"==========\r\n" +
	"Clean Code (Robert C. Martin)\r\n" +
	"- Your Note at location 789 | Added on Sunday, March 1, 2015 7:00:00 AM\r\n" +
	"\r\n" +
	"Note: naming is the cheapest refactor.\r\n" +
	"==========\r\n"

const jaClippings = "\ufeff達人プログラマー（デビッド・トーマス）\r\n" +
	"- ハイライト 位置 234-235 に追加 | 追加日: 2015年1月13日火曜日 10:33:28\r\n" +
	"\r\n" +
	"2日間の作業で2時間の思考を省ける。\r\n" +
	"==========\r\n" +
	"達人プログラマー（デビッド・トーマス）\r\n" +
	"- メモ 位置 789 に追加 | 追加日: 2015年3月1日(日) 7:00:00\r\n" +
	"\r\n" +
	"メモ：名前付けは最も安いリファクタリング。\r\n" +
	"==========\r\n"

func TestParseEnglish(t *testing.T) {
	clips, skipped, dupes, err := Parse(strings.NewReader(enClippings))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || dupes != 0 {
		t.Fatalf("skipped = %d, dupes = %d, want 0/0", skipped, dupes)
	}
	if len(clips) != 3 {
		t.Fatalf("clips = %d, want 3: %+v", len(clips), clips)
	}
	c := clips[0]
	if c.Book != "The Pragmatic Programmer" {
		t.Fatalf("book = %q (author should be stripped)", c.Book)
	}
	if c.Type != "highlight" || c.Location != "234-235" {
		t.Fatalf("type/location = %q/%q", c.Type, c.Location)
	}
	if c.Text != "2 days of work can save 2 hours of thought." {
		t.Fatalf("text = %q", c.Text)
	}
	// The file has no zone; instants are UTC and the time is the added-on moment.
	if want := time.Date(2015, 1, 13, 10, 33, 28, 0, time.UTC); !c.Time.Equal(want) {
		t.Fatalf("time = %v, want %v", c.Time, want)
	}
	if !strings.HasPrefix(c.Anchor, "h") || len(c.Anchor) != 12 {
		t.Fatalf("anchor = %q, want h + 11 hex chars", c.Anchor)
	}
	// Page-only prefix must not steal the location: "on page 42 | location 1240-1241".
	if clips[1].Location != "1240-1241" {
		t.Fatalf("page number leaked into location: %q", clips[1].Location)
	}
	// Notes normalize to their own type.
	if clips[2].Type != "note" {
		t.Fatalf("note type = %q", clips[2].Type)
	}
}

func TestParseJapanese(t *testing.T) {
	clips, skipped, dupes, err := Parse(strings.NewReader(jaClippings))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || dupes != 0 || len(clips) != 2 {
		t.Fatalf("clips/skipped/dupes = %d/%d/%d, want 2/0/0", len(clips), skipped, dupes)
	}
	if clips[0].Book != "達人プログラマー" || clips[0].Type != "highlight" {
		t.Fatalf("ja clip 0 = %+v", clips[0])
	}
	if want := time.Date(2015, 1, 13, 10, 33, 28, 0, time.UTC); !clips[0].Time.Equal(want) {
		t.Fatalf("ja time = %v, want %v", clips[0].Time, want)
	}
	if clips[1].Type != "note" {
		t.Fatalf("ja note type = %q", clips[1].Type)
	}
	// Paren weekday form "(日)" parses too.
	if want := time.Date(2015, 3, 1, 7, 0, 0, 0, time.UTC); !clips[1].Time.Equal(want) {
		t.Fatalf("ja paren weekday time = %v, want %v", clips[1].Time, want)
	}
}

func TestParseSkipsMalformedBlocks(t *testing.T) {
	doc := strings.Join([]string{
		"Book (Author)",
		"- Your Highlight at location 10 | Added on Tuesday, January 13, 2015 10:33:28 AM",
		"",
		"kept",
		"==========",
		"Book (Author)", // missing annotation line
		"==========",
		"Book (Author)",
		"- Your Highlight at location 20 | Added on some unknown date",
		"",
		"dropped (no date)",
		"==========",
		"Book (Author)", // empty text
		"- Your Highlight at location 30 | Added on Tuesday, January 13, 2015 10:33:28 AM",
		"",
		"   ",
		"==========",
		"", // trailing separator
	}, "\n")
	clips, skipped, _, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(clips) != 1 || skipped != 3 {
		t.Fatalf("clips = %d, skipped = %d, want 1/3: %+v", len(clips), skipped, clips)
	}
	if clips[0].Text != "kept" {
		t.Fatalf("kept clip text = %q", clips[0].Text)
	}
}

func TestParseDedupesByAnchor(t *testing.T) {
	doc := strings.Join([]string{
		"Book (Author)",
		"- Your Highlight at location 10 | Added on Tuesday, January 13, 2015 10:33:28 AM",
		"",
		"same text",
		"==========",
		"Book (Author)",
		"- Your Highlight at location 10 | Added on Tuesday, January 13, 2015 10:33:28 AM",
		"",
		"same text",
		"==========",
		"Book (Author)",
		"- Your Highlight at location 10 | Added on Tuesday, January 13, 2015 10:33:28 AM",
		"",
		"different text",
		"==========",
	}, "\n")
	clips, skipped, dupes, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || dupes != 1 || len(clips) != 2 {
		t.Fatalf("clips/skipped/dupes = %d/%d/%d, want 2/0/1", len(clips), skipped, dupes)
	}
	// A single character of text changes the anchor.
	if clips[0].Anchor == clips[1].Anchor {
		t.Fatalf("anchors must differ across texts: %q", clips[0].Anchor)
	}
}

func TestAnchorIsDeterministic(t *testing.T) {
	a := anchor("B", "highlight", "1-2", "t")
	b := anchor("B", "highlight", "1-2", "t")
	if a != b {
		t.Fatalf("same input gave different anchors %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "h") || len(a) != 12 {
		t.Fatalf("anchor shape = %q, want ^h + 11 hex chars", a)
	}
	for _, r := range a[1:] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("anchor %q has non-hex char %q", a, r)
		}
	}
	// Every component participates: a change anywhere moves the anchor.
	if anchor("C", "highlight", "1-2", "t") == a || anchor("B", "note", "1-2", "t") == a ||
		anchor("B", "highlight", "9-9", "t") == a || anchor("B", "highlight", "1-2", "u") == a {
		t.Fatal("anchor must depend on every key component")
	}
}

func TestEventsAreOrderedValidCanonicalRecords(t *testing.T) {
	clips, _, _, err := Parse(strings.NewReader(enClippings))
	if err != nil {
		t.Fatal(err)
	}
	records, err := Events(clips)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %+v", records)
	}
	// Ascending by time: Jan 13 (10:33) < Jan 13 (21:12, note the PM) < Mar 1.
	if got := records[0]["title"]; got != "2 days of work can save 2 hours of thought." {
		t.Fatalf("records[0].title = %q", got)
	}
	if got := records[1]["time"].(string); !strings.HasPrefix(got, "2015-01-16T21:12:01Z") {
		t.Fatalf("PM time not normalized to 24h UTC: %v", got)
	}
	if records[1]["time"].(string) >= records[2]["time"].(string) {
		t.Fatalf("records not ascending: %v >= %v", records[1]["time"], records[2]["time"])
	}
	first := records[0]
	if first["version"] != dataset.SchemaVersion || first["entity"] != "The Pragmatic Programmer" {
		t.Fatalf("canonical fields missing: %+v", first)
	}
	if _, ok := first["location"]; !ok {
		t.Fatalf("location extra field missing: %+v", first)
	}
	if err := dataset.ValidateRecords(dataset.KindEvent, records); err != nil {
		t.Fatalf("records must validate: %v", err)
	}
}

func TestNoteBody(t *testing.T) {
	clips, _, _, err := Parse(strings.NewReader(enClippings))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	body := NoteBody(clips, "The Pragmatic Programmer", now)

	for _, want := range []string{
		"up:: [[The Pragmatic Programmer]]",
		"Clipped 2026-09-06",
		"- 2 days of work can save 2 hours of thought. ^" + clips[0].Anchor,
		"- Don't Program by Coincidence. ^" + clips[1].Anchor,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("note body missing %q:\n%s", want, body)
		}
	}
	// Regeneration is idempotent: same clips, same anchors.
	if again := NoteBody(clips, "The Pragmatic Programmer", now); again != body {
		t.Fatal("regenerated note body differs")
	}
	// The anchors are exactly the block-id shape the link package accepts (^letter/digit, then
	// letters/digits/-/_).
	for _, c := range clips {
		if !strings.Contains(body, "^"+c.Anchor) {
			t.Fatalf("note body missing anchor ^%s for %q", c.Anchor, c.Text)
		}
	}
}

func TestNoteBodyMultiParagraphStaysOneItem(t *testing.T) {
	clips, _, _, err := Parse(strings.NewReader(
		"Book (Author)\n- Your Highlight at location 5 | Added on Tuesday, January 13, 2015 10:33:28 AM\n\npara one\n\npara two\n==========\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	body := NoteBody(clips, "Book", time.Unix(0, 0).UTC())
	// The paragraph break becomes an indented continuation, so the item (and its ^anchor) stays
	// intact; the anchor sits on the item line.
	if !strings.Contains(body, "- para one ^"+clips[0].Anchor+"\n  para two") {
		t.Fatalf("multi-paragraph item malformed:\n%s", body)
	}
}
