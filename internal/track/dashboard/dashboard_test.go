package dashboard

import (
	"strings"
	"testing"
)

func TestResolveRecentJournalPinned(t *testing.T) {
	body := "# Home\n\n```dashboard\nrecent: 2\njournal: true\npinned:\n  - Guide\n  - Syntax\n```\n\nfooter\n"
	got := Resolve(body, Data{
		RecentTitles: []string{"Alpha", "Beta", "Gamma"},
		JournalTitle: "20260711",
	})
	for _, want := range []string{
		"# Home",
		"**Recent notes**",
		"- [[Alpha]]",
		"- [[Beta]]",
		"**Today's journal**",
		"- [[20260711]]",
		"**Pinned**",
		"- [[Guide]]",
		"- [[Syntax]]",
		"footer",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("resolved output missing %q\n---\n%s", want, got)
		}
	}
	// recent: 2 truncates a longer list.
	if strings.Contains(got, "Gamma") {
		t.Errorf("recent widget should have truncated to 2 items, got Gamma:\n%s", got)
	}
	// The fence itself must be gone (resolved to markdown, not left as a code block).
	if strings.Contains(got, "```dashboard") {
		t.Errorf("dashboard fence was not consumed:\n%s", got)
	}
}

func TestResolveNoFenceUntouched(t *testing.T) {
	body := "# Plain note\n\nnothing to see\n"
	if got := Resolve(body, Data{RecentTitles: []string{"X"}}); got != body {
		t.Errorf("body without a dashboard fence should be unchanged\nwant %q\ngot  %q", body, got)
	}
}

func TestResolveEmptyJournalShowsEmptyState(t *testing.T) {
	body := "```dashboard\njournal: true\n```\n"
	got := Resolve(body, Data{})
	if !strings.Contains(got, "No journal yet.") {
		t.Errorf("journal widget with no journal should show its empty state:\n%s", got)
	}
}

func TestResolveBadYAMLKeepsSource(t *testing.T) {
	body := "```dashboard\nrecent: [not, an, int\n```\n"
	got := Resolve(body, Data{})
	if !strings.Contains(got, "Dashboard error:") {
		t.Errorf("a malformed block should surface an inline error:\n%s", got)
	}
}
