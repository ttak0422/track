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
alike; that is the one surface that draws it today. It is note-level metadata, so it never lives in the
body: a **vault** note keeps it in its sidecar, and a **published site built from a plain Markdown
directory** — like this help site — keeps every page's in its `site.yml`. Both resolve it by the same
rule.

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

`track export-site --src <dir>` publishes plain Markdown files that belong to no vault — no note ids and
no note sidecars, just files in a repository. Their bodies stay pure Markdown all the same, so a page's
icon is not written inside it. It is declared once, in the site's own `site.yml`, under `icons.pages` —
a map from a page's **file base name** to its icon:

```yaml
# docs/help/site.yml
icons:
  pages:
    dashboard: 🏠      # this page
    index: 🧭
  kinds:
    note: 📄
```

`icons.pages` is the *page's own icon*: it takes the slot a vault note's sidecar `icon` takes, so the
precedence is the one you already know — the page's own icon, then (for a vault note) its tags, then the
kind mapping. A page with no entry simply falls through, which is why [[Syntax]] shows 📄 from the `kinds`
map: a published page is always kind `note`.

An `icons.pages` entry naming a page that does not exist — no `<name>.md` in the directory — is a **build
error** naming the entry and the file it looked for. It is a typo, or a page you renamed and forgot; it is
never a silent no-op.

Why the site config and not a little metadata file per page? Because in a vault nobody hand-writes a
sidecar: `track new` and `track open` create it, and `track rename` maintains it. A published directory has
no tool between you and the files, so a per-page sidecar would be boilerplate to hand-write and hand-rename
— thirteen files, plus one more rename every time you rename a page. One map in the config you already have
is not.

Note what does *not* set a page's icon: an inline `key:: value` field (see [[Properties]]). Those are for
data that belongs in your prose; an icon is not prose.

## The published site's config

A directory of Markdown files publishes with no config at all by default: no vault, and never your
machine's `~/.config/track/config.yml` — the same directory has to publish the same way on your laptop and
in CI. What the *site* is, though, belongs with the content, so `export-site --src <dir>` picks up an
optional `site.yml` — or `site.yaml`, either spelling — sitting in that directory (this help site has one,
at `docs/help/site.yml`). No file means exactly the plain export above: the `index` convention and no icons.

```yaml
# docs/help/site.yml
home: index          # the entry page: a file base name or a page title
icons:
  pages:             # file base name -> the page's own icon
    index: 🧭
    dashboard: 🏠
  kinds:             # a published page is always kind `note`
    note: 📄
```

- **`home`** is the site's landing page — the published counterpart of the workspace's `web.home`. It
  names a page the way a `[[wiki link]]` does, by file base name first and page title second. Unset — a
  `site.yml` that only maps icons, or no `site.yml` at all — a page named `index` is the fallback. If
  neither names a real page, the build fails loudly
  rather than quietly publishing a different front door. There is no flag for it: a site's front door is
  the same wherever it is deployed, so it belongs with the content.
- **`icons`** takes `kinds` — the ambient config's map, same meaning — plus `pages`, the one thing a
  directory has and a vault does not: a per-page icon keyed by file base name, filling the slot a note's
  sidecar `icon` fills. There is no `tags` map here, though the ambient config has one and the resolver
  consults it: a directory page carries no tags, so a tag mapping could never match one, and a mapping that
  can never match is not config — it is a line that does nothing. Writing `icons.tags` in a `site.yml` is an
  unknown key, and so a build error, like any other. An `icons.pages` entry with no icon (the emoji
  deleted, or a bare `dashboard:`) is one too — it is the same silent no-op wearing a different hat.

Everything a page says about itself, then, is said in one file. A page's *title* is the exception that
needs no config at all: it is the page's first `# H1`, or its file name when it has none.

Unknown keys are a **build error** naming the file and the key — as is a second `---` document, which a
single decode would never read — not a silent drop: a mistyped key in a config you only exercise at
publish time would otherwise ship the wrong site without a word.

What is *not* site config: `--base-url`, `--out`, and `--frontend`. They describe one *build* of the
content, not the site — where the output is written, which frontend build goes into it, which origin it is
served from — and this one `docs/help` directory is published twice, to GitHub Pages and, for every pull
request, to a preview URL. So they stay build flags.
