// Package dashboard resolves fenced ```dashboard widget blocks into plain Markdown, so a note (typically
// the configured home note) can carry a small workspace landing view — recent notes, today's journal, and
// pinned links — that renders identically in the live web workspace and the static-site export.
//
// The resolution deliberately produces ordinary Markdown ([[wiki links]] in bulleted lists) rather than a
// bespoke widget component: the same [[link]] and list rendering already works in both deployments, so no
// new frontend renderer or API is needed. The engine stays the single source of truth for the widget data
// — the live server resolves a block on /api/render, the static export resolves it at build time — while
// the frontend just draws Markdown.
package dashboard

import (
	"fmt"
	"strings"

	"github.com/ttak0422/track/internal/track/babel"
	"gopkg.in/yaml.v3"
)

// Lang is the fence language that marks a dashboard block (```dashboard ... ```), mirroring how
// ```viewspec marks an embedded chart.
const Lang = "dashboard"

// Data supplies the vault-derived values a dashboard block needs. The caller (the web server or the site
// export) gathers these once; the resolver slices them per block. Pinned links come from the block body
// itself, so they need no Data.
type Data struct {
	// RecentTitles lists note titles most-recently-updated first. The recent widget takes its first N.
	RecentTitles []string
	// JournalTitle is today's journal note title (its date-addressed name), for the journal shortcut.
	// Empty disables the shortcut even when a block requests it.
	JournalTitle string
}

// config is one dashboard block's YAML body. Every field is optional; an empty block renders nothing.
type config struct {
	Title   string   `yaml:"title"`   // optional heading above the widgets
	Recent  int      `yaml:"recent"`  // recent-notes widget: show this many (<=0 omits it)
	Journal bool     `yaml:"journal"` // today's-journal shortcut widget
	Pinned  []string `yaml:"pinned"`  // pinned note titles, linked in order
}

// Resolve replaces every fenced ```dashboard block in body with the Markdown its widgets render to. A
// block whose body is not valid YAML is replaced by an inline error plus its source (matching how the
// viewspec export surfaces a bad block), so the page still renders. Bodies with no dashboard fence — the
// common case — are returned untouched.
func Resolve(body string, data Data) string {
	lines := strings.Split(body, "\n")
	var out []string
	next := 0
	for _, b := range babel.ParseBlocks(body) {
		if !strings.EqualFold(b.Language, Lang) {
			continue
		}
		out = append(out, lines[next:b.StartLine]...)
		var cfg config
		if err := yaml.Unmarshal([]byte(b.Body), &cfg); err != nil {
			out = append(out, "> Dashboard error: "+err.Error(), "", "```"+Lang, b.Body, "```")
		} else {
			out = append(out, render(cfg, data))
		}
		next = b.EndLine + 1
	}
	if next == 0 {
		return body // no dashboard fences: untouched
	}
	out = append(out, lines[next:]...)
	return strings.Join(out, "\n")
}

// render turns one block's config into Markdown: a titled section per requested widget, each a heading
// followed by a bulleted list of [[wiki links]]. Widgets are emitted in a fixed order (recent, journal,
// pinned) so a block reads the same regardless of YAML key order.
func render(cfg config, data Data) string {
	var b strings.Builder
	if cfg.Title != "" {
		fmt.Fprintf(&b, "## %s\n\n", cfg.Title)
	}
	if cfg.Recent > 0 {
		titles := data.RecentTitles
		if len(titles) > cfg.Recent {
			titles = titles[:cfg.Recent]
		}
		writeWidget(&b, "Recent notes", titles, "No recent notes.")
	}
	if cfg.Journal {
		var items []string
		if data.JournalTitle != "" {
			items = []string{data.JournalTitle}
		}
		writeWidget(&b, "Today's journal", items, "No journal yet.")
	}
	if len(cfg.Pinned) > 0 {
		writeWidget(&b, "Pinned", cfg.Pinned, "")
	}
	return strings.TrimRight(b.String(), "\n")
}

// writeWidget appends one widget: a bold label, then a [[link]] per title, or an empty-state line when
// there are none (skipped when empty is ""). Blank-line separation keeps each widget its own Markdown
// block so headings and lists render cleanly.
func writeWidget(b *strings.Builder, label string, titles []string, empty string) {
	fmt.Fprintf(b, "**%s**\n\n", label)
	if len(titles) == 0 {
		if empty != "" {
			fmt.Fprintf(b, "%s\n\n", empty)
		}
		return
	}
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		fmt.Fprintf(b, "- [[%s]]\n", t)
	}
	b.WriteString("\n")
}
