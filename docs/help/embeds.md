# Embeds

An embed is a standalone Markdown image link — `![alt](src)` **on its own line** — that track renders
as rich media instead of a plain `<img>`. Inline image syntax inside a paragraph stays inline, so
embedding is always opt-in and ordinary `[text](url)` links are never turned into noisy previews. track
routes each embed by the kind of target (below); only `http(s)` and relative URLs feed an iframe, so a
note cannot smuggle a `javascript:` document into an embed.

Part of [[Visualization]] (see also [[Diagrams]] and [[Charts]]). Back to [[track]].

## Local files

Local media belongs under the vault's `assets/` directory. `track asset import ./image.png` copies the
file there and prints the relative reference you can paste into a note:

```markdown
![track logo](assets/logo.png)
```

![track logo](assets/logo.png)

A relative `assets/<file>` reference is served from the vault by the local server, so it is never
treated as a YouTube/tweet/OGP URL.

## YouTube

A YouTube watch, share, or embed URL becomes an inline player:

```markdown
![Intro](https://www.youtube.com/watch?v=aqz-KE-bpKQ)
```

![Intro](https://www.youtube.com/watch?v=aqz-KE-bpKQ)

## Google Maps

A Google Maps share or embed URL becomes an inline map. Paste the `Share → Embed a map` URL as-is, or
build the keyless form yourself with a query or coordinates — no API key is needed. Short
`maps.app.goo.gl` links need a network redirect to resolve, so those fall back to an Open Graph card
instead:

```markdown
![都庁](https://maps.google.com/maps?q=35.6896,139.6917&z=16&output=embed)
```

This one is live — it centers on the Tokyo Metropolitan Government building (都庁):

![都庁](https://maps.google.com/maps?q=35.6896,139.6917&z=16&output=embed)

## Twitter / X

A tweet URL renders the actual post via Twitter's official widgets (matching how Obsidian embeds
tweets), not just a link card. While the widget loads it shows a plain link, and if the post cannot be
rendered — deleted, blocked, or offline — it falls back to the generic Open Graph card:

```markdown
![](https://x.com/elonmusk/status/1585341984679469056?s=20)
```

![](https://x.com/elonmusk/status/1585341984679469056?s=20)

## PDF

A PDF (local asset or remote URL) opens in a paged slide-deck viewer rather than a download link:

```markdown
![Deck](assets/slides.pdf)
```

This one is live — page through it with the arrows (or click a page). The sample is a three-page
deck with just the page number on each page:

![Deck](assets/slides.pdf)

## Web pages (Open Graph)

Any other `http(s)` page becomes an Open Graph card — its title, description, and preview image pulled
from the page's `og:` metadata:

```markdown
![](https://example.com/article)
```

## Text-file attachments

A text file imported with `track asset import` is fetched and rendered inline instead of shown as a
broken image. A config file, script, or data snippet you want to keep beside a note (`.txt`, `.json`,
`.yaml`, `.csv`, `.sh`, …) renders as a syntax-highlighted code block:

```markdown
![](assets/pod.yaml)
```

![](assets/pod.yaml)

Two text kinds render as something richer instead: a Mermaid source (`.mmd` / `.mermaid`) becomes a
diagram — see [[Diagrams]] — and a `.viewspec.json` becomes a **chart** — see [[Charts]].

## HTML pages

An HTML file is a vault asset like any other: `track asset import ./widget.html` copies it under
`assets/`, and the standard embed syntax mounts it in a **sandboxed iframe** — its own JS and CSS run,
but it is isolated from the workspace (unique opaque origin, no access to the app, cookies, or storage):

```markdown
![Widget](assets/widget.html)
```

![Widget](assets/widget.html)

A remote `http(s)://…/page.html` URL is mounted the same way. The frame has a fixed default height since
an arbitrary page has no intrinsic aspect ratio.

The [[Web workspace]] renders every embed live; the static export ([[CLI]] `export-site`) renders the
same output for a published note.
