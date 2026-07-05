// Package rss converts an RSS 2.0 or Atom feed into Canonical Data Model event records — the engine
// behind the track-fetch-rss tool (see docs/spec/fetch.md). It depends only on the dataset contract,
// not on the track CLI or store.
package rss

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// Item is one feed entry, normalized across RSS and Atom.
type Item struct {
	Title string
	URL   string
	Time  time.Time
}

// Feed is a parsed feed: its title (a natural entity fallback) and entries in feed order.
type Feed struct {
	Title string
	Items []Item
}

// rss2 mirrors the RSS 2.0 shape (a <rss><channel> root).
type rss2 struct {
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
			DCDate  string `xml:"http://purl.org/dc/elements/1.1/ date"`
		} `xml:"item"`
	} `xml:"channel"`
}

// atom mirrors the Atom shape (a <feed> root in the Atom namespace).
type atom struct {
	Title   string `xml:"title"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
	} `xml:"entry"`
}

// Parse reads an RSS 2.0 or Atom document, dispatching on the root element. An entry without a title
// or a parsable date is dropped (feeds are messy; the skipped count is reported), never emitted
// half-formed — the dataset contract requires both.
func Parse(r io.Reader) (Feed, int, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Feed{}, 0, err
	}
	root, err := rootElement(raw)
	if err != nil {
		return Feed{}, 0, fmt.Errorf("parse feed: %w", err)
	}
	switch root {
	case "rss":
		var f rss2
		if err := xml.Unmarshal(raw, &f); err != nil {
			return Feed{}, 0, fmt.Errorf("parse rss: %w", err)
		}
		feed := Feed{Title: strings.TrimSpace(f.Channel.Title)}
		skipped := 0
		for _, it := range f.Channel.Items {
			date := it.PubDate
			if strings.TrimSpace(date) == "" {
				date = it.DCDate
			}
			feed.Items, skipped = appendItem(feed.Items, skipped, it.Title, it.Link, date)
		}
		return feed, skipped, nil
	case "feed":
		var f atom
		if err := xml.Unmarshal(raw, &f); err != nil {
			return Feed{}, 0, fmt.Errorf("parse atom: %w", err)
		}
		feed := Feed{Title: strings.TrimSpace(f.Title)}
		skipped := 0
		for _, e := range f.Entries {
			href := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					href = l.Href
					break
				}
			}
			date := e.Published
			if strings.TrimSpace(date) == "" {
				date = e.Updated
			}
			feed.Items, skipped = appendItem(feed.Items, skipped, e.Title, href, date)
		}
		return feed, skipped, nil
	}
	return Feed{}, 0, fmt.Errorf("unsupported feed format: root element <%s> (expected <rss> or Atom <feed>)", root)
}

// appendItem normalizes one entry onto the item list, counting (instead of emitting) entries that
// are missing a title or a parsable date.
func appendItem(items []Item, skipped int, title, url, date string) ([]Item, int) {
	title = strings.TrimSpace(title)
	t, err := parseTime(date)
	if title == "" || err != nil {
		return items, skipped + 1
	}
	return append(items, Item{Title: title, URL: strings.TrimSpace(url), Time: t}), skipped
}

// rootElement returns the local name of the document's first element.
func rootElement(raw []byte) (string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local, nil
		}
	}
}

// feedTimeFormats are the timestamp layouts seen in real feeds, tried in order: the RFC 822 family
// RSS mandates (with 4-digit years and named or numeric zones) and the RFC 3339 family Atom mandates.
var feedTimeFormats = []string{
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	time.RFC3339,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02",
}

// parseTime normalizes a feed timestamp, trying each known layout.
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range feedTimeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q", s)
}

// Events maps feed items onto canonical event records (docs/spec/fetch.md): time normalized to
// RFC 3339, ordered ascending, each validated against the event kind so a fetch tool can never emit
// a non-conformant line. entity, when non-empty, scopes every event (falling back is the caller's
// choice — e.g. the feed title).
func Events(items []Item, entity string) ([]dataset.Record, error) {
	sorted := make([]Item, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	records := make([]dataset.Record, 0, len(sorted))
	for _, it := range sorted {
		rec := dataset.Record{
			"version": dataset.SchemaVersion,
			"time":    it.Time.Format(time.RFC3339),
			"title":   it.Title,
		}
		if it.URL != "" {
			rec["url"] = it.URL
		}
		if entity != "" {
			rec["entity"] = entity
		}
		records = append(records, rec)
	}
	if err := dataset.ValidateRecords(dataset.KindEvent, records); err != nil {
		return nil, err
	}
	return records, nil
}
