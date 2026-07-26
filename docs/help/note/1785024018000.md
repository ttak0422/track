# Web clipper

`track-fetch-web` clips a web page into your vault: it fetches the page, extracts the readable main
content (dropping navigation, sidebars, and other page furniture), and converts it to Markdown so a
page becomes a note in one pipeline. Like all `track-fetch-*` tools it is a separate binary — track
itself never talks to the network.

Back to [[track]]. See [[CLI]] for note commands and [[Charts]] for the Canonical Data Model that
fetch tools share.

## Clipping a page into a note

The `--note` flag prints a ready-to-pipe Markdown note body — a provenance line, the lead image,
then the content — which `track new` reads from stdin:

```sh
track-fetch-web --note https://example.com/essays/growing-tomatoes \
  | track new --title "Growing tomatoes"
```

The new note starts like this:

```markdown
[Source](https://example.com/essays/growing-tomatoes) — clipped 2026-07-10

![](https://example.com/images/tomatoes-lead.jpg)

Container gardening rewards small, steady adjustments. A pot dries faster than a bed…
```

From there it is an ordinary note: add `[[Title]]` links, tags, and your own commentary. `track
open` and `track append` work the same way if you prefer to clip into an existing note.

## What gets extracted

- **Title** — from `og:title`, the `<title>` element, or the first heading. A leading heading that
  repeats the title is dropped from the body so it is not duplicated.
- **Timestamp** — the page's declared publication time (`article:published_time` and friends),
  falling back to the fetch time.
- **Lead image** — `og:image`, or the first image in the content.
- **Main content** — a compact readability heuristic: the page's own `<article>`/`<main>` container
  when it carries real text, otherwise the element with the most paragraph text and the fewest
  links. Headings, lists, quotes, code blocks, tables, links, and images are converted to Markdown;
  scripts, navigation, footers, and ad/share/comment furniture are removed.

Pages rendered entirely by JavaScript have no readable HTML to extract; the clip degrades to the
page metadata.

## Canonical JSONL output

Without `--note`, the tool follows the `track-fetch-*` contract: it emits one Canonical Data Model
`event` record (JSONL) with the Markdown content and lead image as extra fields, so clips can also
feed [[Charts]] — a reading-log timeline, for example.

```sh
track-fetch-web https://example.com/essays/growing-tomatoes
```

```jsonl
{"version":1,"time":"2026-05-04T09:30:00Z","title":"Growing tomatoes","url":"https://example.com/essays/growing-tomatoes","image":"https://example.com/images/tomatoes-lead.jpg","markdown":"Container gardening rewards…"}
```

`--out <file>` writes the record to a file (conventionally the vault's `data/` directory) and
prints a JSON summary instead, matching the track CLI's result style.

## Flags

| Flag | Purpose |
| --- | --- |
| `--url <s>` | Page URL; a bare argument works too. A local file path replays a saved page. |
| `--note` | Print a Markdown note body for piping into `track new` instead of JSONL. |
| `--out <file>` | Write the JSONL record to a file and print a JSON summary. |
| `--timeout <dur>` | HTTP fetch timeout (default 30s). |

Fetches are guarded against SSRF: private, loopback, and link-local addresses are refused, so a
clipped URL can never probe your local network. To clip a page from a local or internal site, save
it to a file first and pass the path.
