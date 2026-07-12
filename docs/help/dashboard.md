# Home dashboard

The [[Web workspace]] can open a **home note** as its landing view instead of the search screen, and any
note can embed **dashboard widgets** — a recent-notes list, today's journal shortcut, and pinned links —
that render both in the live workspace and on this published site.

icon:: 🏠

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

An icon can sit beside a note's title in lists, search results, and navigation. Where it comes from
depends on what is being shown: a **vault** has a config and per-note metadata, while a **published site
built from a plain Markdown directory** — like this help site — has neither, so each page states its own.

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

`track export-site --src <dir>` publishes plain Markdown files that belong to no vault: there is no
sidecar to override a page's icon, and inline `key:: value` fields (see [[Properties]]) are a page's only
metadata. So a page sets its own icon — and its own tags — with inline fields:

```markdown
# Home dashboard

icon:: 🏠
tags:: guide

The Web workspace can open a home note as its landing view.
```

The first `icon::` field on a page wins, and an empty value means no icon — the same as having no field
at all. It stays an ordinary property, so it also shows up in the page's property strip. Every page of
this help site carries one: search for a page here and its icon is beside the title in the results.

A page with no `icon::` field can still get one from the site's own tag and kind maps, below — the same
precedence as in a vault, with the page's field playing the part the sidecar override plays there.

## The published site's config

A directory of Markdown files publishes with no config at all by default: no vault, no sidecars, and
never your machine's `~/.config/track/config.yml` — the same directory has to publish the same way on
your laptop and in CI. What the *site* is, though, belongs with the content, so `export-site --src <dir>`
picks up an optional `site.yml` — or `site.yaml`, either spelling — sitting in that directory (this help
site has one, at `docs/help/site.yml`). No file means exactly the plain export above: the `index`
convention and no icon maps. The file is opt-in; what a *page* says about itself with its own `icon::` and
`tags::` fields publishes either way.

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
  names a page the way a `[[wiki link]]` does, by file base name first and page title second. The
  `--root` flag still wins when you pass it, so a one-off build can land somewhere else; with neither, a
  page named `index` is the fallback. If none of them names a real page, the build fails loudly rather
  than quietly publishing a different front door.
- **`icons`** is the same map, with the same meaning and precedence, as the ambient config's `icons:` —
  a page's `icon::` field, then the first of its `tags::` with a mapping, then its kind (a published page
  is always kind `note`). Every page of this help site states its own `icon::`, so the maps here are the
  fallback for pages that do not.

Unknown keys are a **build error** naming the file and the key — as is a second `---` document, which a
single decode would never read — not a silent drop: a mistyped key in a config you only exercise at
publish time would otherwise ship the wrong site without a word.

What is *not* site config: `--base-url`, `--out`, and `--frontend`. They describe one *build* of the
content, not the site — where the output is written, which frontend build goes into it, which origin it is
served from — and this one `docs/help` directory is published twice, to GitHub Pages and, for every pull
request, to a preview URL. So they stay build flags.
