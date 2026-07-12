# Home dashboard

The [[Web workspace]] can open a **home note** as its landing view instead of the search screen, and any
note can embed **dashboard widgets** — a recent-notes list, today's journal shortcut, and pinned links —
that render both in the live workspace and on this published site.

Back to [[track]].

## A home note

Point the workspace at a landing note in your config:

```yaml
# ~/.config/track/config.yml
web:
  home: Home
```

`home` takes a note title or a numeric id. When set, `track web` opens that note at `/` instead of the
search hero. Leave it unset to keep the search home. On the published static site the entry note chosen
with `track export-site --root` plays the same role.

## Dashboard widgets

Fence a block with `dashboard` — the same way a `mermaid` fence draws a diagram — and fill it with a
small YAML config:

````markdown
```dashboard
recent: 5          # a list of the 5 most-recently-updated notes
journal: true      # a shortcut to today's journal
pinned:            # notes you want one click away
  - Syntax
  - Web workspace
```
````

Every field is optional. The engine resolves the block to ordinary Markdown — bulleted `[[wiki links]]`
under a heading per widget — so it renders identically in the live workspace (resolved on each view, so
the recent list stays current) and in this static export (resolved at build time). Because the widgets
are just links, they get hover previews, backlinks, and navigation for free.

Here is a live one, resolved from a real `dashboard` block:

```dashboard
recent: 4
pinned:
  - Syntax
  - Web workspace
```

## Note icons

An icon can sit beside a note's title in lists, search results, and navigation. Map an emoji to a tag or
a note kind in your config:

```yaml
icons:
  tags:
    idea: 💡
    book: 📚
  kinds:
    journal: 📓
    note: 📝
```

The first of a note's tags that has a mapping wins; otherwise its kind's mapping applies. A single note
can override both from the command line (or the note's sidecar metadata):

```sh
track meta --title "Reading list" --icon 📚
```

Precedence is simple: a per-note `--icon` override beats a tag mapping, which beats a kind mapping. Set
an empty `--icon ""` to clear the override and fall back to the mapping. Icons are cosmetic — they never
change a note's title, id, or how `[[links]]` resolve.
