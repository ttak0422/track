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
search hero. Leave it unset to keep the search home. This is the *workspace's* home: it lives in your
machine's config, so it follows you, not the notes. A published site has a home of its own, in a config
that travels with the content — see "The published site's config" below.

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

An icon can sit beside a note's title in search results — in the live workspace and on a published site
alike; that is the one surface that draws it today. It is metadata, so it lives where all of a note's
metadata lives: in a sidecar file next to the body, never in the body itself. A **vault** has one per
note; a **published site built from a plain Markdown directory** — like this help site — has one per
page.

### In a vault

Map an emoji to a tag or a note kind in your config:

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

### On a published directory site

`track export-site --src <dir>` publishes plain Markdown files that belong to no vault. They still keep
the body and its metadata apart: a page gets a **page sidecar** at `.track/<name>.yml`, beside — not
inside — `<name>.md`. This page's own sidecar is `docs/help/.track/dashboard.yml`:

```yaml
# docs/help/.track/dashboard.yml
icon: 🏠
tags: [guide]
```

It carries the same keys a vault note's metadata does — `title`, `tags`, `description`, `image`, `icon`,
`props` — and it is keyed by file name because a published directory has no note ids. Every key is
optional, and so is the whole file: a page without one is a plain Markdown file, exactly as before (the
[[Syntax]] page has no sidecar, and takes its icon from the kind map below). A sidecar naming no page, a
key that is not one of the six, or the same page spelled both `.yml` and `.yaml` is a **build error**, not
a shrug — a typo in a file you only exercise at publish time would otherwise publish a page missing the
metadata you wrote.

A `title` here wins over the page's first `# H1`, and becomes the key `[[links]]` resolve by. A page with
no `icon` falls through to the site's tag and kind maps, below — the same precedence as in a vault, with
the page sidecar playing the part the note sidecar plays there.

Note what does *not* set a page's icon: an inline `key:: value` field (see [[Properties]]). Those are for
data that belongs in your prose; an icon is not prose.

## The published site's config

A directory of Markdown files publishes with no config at all by default: no vault, and never your
machine's `~/.config/track/config.yml` — the same directory has to publish the same way on your laptop and
in CI. What the *site* is, though, belongs with the content, so `export-site --src <dir>` picks up an
optional `site.yml` — or `site.yaml`, either spelling — sitting in that directory (this help site has one,
at `docs/help/site.yml`). No file means exactly the plain export above: the `index` convention and no icon
maps. The file is opt-in, and it is the *site's*: what a single *page* says about itself lives in that
page's own sidecar, under `.track/`, so the two never collide.

```yaml
# docs/help/site.yml
home: index          # the entry page: a file base name or a page title
icons:
  tags:
    reference: 📖
    guide: 🧭
  kinds:
    note: 📄
```

- **`home`** is the site's landing page — the published counterpart of the workspace's `web.home`. It
  names a page the way a `[[wiki link]]` does, by file base name first and page title second. Unset — a
  `site.yml` that only maps icons, or no `site.yml` at all — a page named `index` is the fallback. If
  neither names a real page, the build fails loudly
  rather than quietly publishing a different front door. There is no flag for it: a site's front door is
  the same wherever it is deployed, so it belongs with the content.
- **`icons`** is the same map, with the same meaning and precedence, as the ambient config's `icons:` —
  a page sidecar's `icon`, then the first of the page's `tags` with a mapping, then its kind (a published
  page is always kind `note`). All three paths are live on this site: most pages set their own `icon`, the
  [[CLI]] page has none and takes 📖 from the `reference` tag, and [[Syntax]] has no sidecar at all and
  takes 📄 from the kind map.

Unknown keys are a **build error** naming the file and the key — as is a second `---` document, which a
single decode would never read — not a silent drop: a mistyped key in a config you only exercise at
publish time would otherwise ship the wrong site without a word.

What is *not* site config: `--base-url`, `--out`, and `--frontend`. They describe one *build* of the
content, not the site — where the output is written, which frontend build goes into it, which origin it is
served from — and this one `docs/help` directory is published twice, to GitHub Pages and, for every pull
request, to a preview URL. So they stay build flags.
