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
![Intro](https://www.youtube.com/watch?v=VIDEO_ID)
```

## Twitter / X

A tweet URL renders the actual post via Twitter's official widgets (matching how Obsidian embeds
tweets), not just a link card. While the widget loads it shows a plain link, and if the post cannot be
rendered — deleted, blocked, or offline — it falls back to the generic Open Graph card:

```markdown
![](https://x.com/jack/status/20)
```

## PDF

A PDF (local asset or remote URL) opens in a paged slide-deck viewer rather than a download link:

```markdown
![Deck](assets/slides.pdf)
```

## Web pages (Open Graph)

Any other `http(s)` page becomes an Open Graph card — its title, description, and preview image pulled
from the page's `og:` metadata:

```markdown
![](https://example.com/article)
```

## Text-file attachments

A text file imported with `track asset import` is fetched and rendered inline instead of shown as a
broken image. A Mermaid source (`.mmd` / `.mermaid`) renders as a diagram (see [[Diagrams]]), and any
other text file (`.txt`, `.json`, `.yaml`, `.csv`, shell scripts, …) renders as a syntax-highlighted
code block:

```markdown
![](assets/gantt.mmd)
```

A `.viewspec.json` attachment is a **chart** embed — see [[Charts]] for the View Spec that drives it.

The [[Web workspace]] renders every embed live; the static export ([[CLI]] `export-site`) renders the
same output for a published note.
