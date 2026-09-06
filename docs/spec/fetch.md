# Fetch tools: the `track-fetch-*` contract

track never fetches external data (ADR 0021): converting the outside world into the Canonical Data
Model is the job of separate **`track-fetch-*`** binaries. This document is the contract those tools
target. They live in this repository (one Go module, separate `cmd/` mains and Nix packages) but are
deliberately independent of the track CLI: a fetch tool depends on `internal/track/dataset` — the
typed record schemas and validation — and nothing else of the engine.

## Contract

A fetch tool converts one external source into Canonical JSONL:

- **Output is JSONL** on stdout by default, or written to a file with `--out`. One record per line,
  **one kind per stream/file** (see `docs/spec/visualization.md` for the kinds). With `--out` the
  tool prints a JSON summary (`{"path": ..., "records": N, ...}`) to stdout, matching the track CLI's
  JSON-result style.
- **Every record validates** against its kind (`dataset.Validate`): required fields present, numeric
  fields numeric, and a schema `version` on every record. A tool must not emit non-conformant
  records — rendering validates again at the boundary and will fail the whole file loudly.
- **`time` is RFC 3339.** Source timestamps in other formats are normalized by the tool, so
  downstream consumers never parse source-specific dates.
- **Records are ordered by time, ascending**, so plain `tail`/diff work and appends stay coherent.
- **Diagnostics go to stderr** (items skipped, parse warnings); data never mixes with logs.
- Extra fields beyond the kind's schema are allowed (the render pipeline can chart them), but the
  canonical fields carry the meaning.

## Where the data goes

The conventional target is the vault's **`data/` directory** (created by `track init`):

```sh
track-fetch-rss --url https://example.com/feed.xml --out ~/track/data/news.jsonl
```

Charts reference the file by name (`data.source: "news.jsonl"`, resolved inside `data/`). The
`track web` workspace watches `data/` and emits a `data` Server-Sent Event on change, so embedded
charts re-render live when a fetch tool rewrites its file — running one on a schedule (cron,
launchd) gives live dashboards with no further wiring.

A tool writes a **complete snapshot** of what it fetched; merging with previous runs (dedup, rolling
windows) is left to the tool's own flags where it matters, not to a shared framework. Derived
columns and aggregation are likewise out of scope here — the fetch side may precompute them (they
are ordinary extra fields), or a future `track compute` may transform canonical JSONL; see the
visualization spec's "no computation in specs" stance.

## Packaging

The repository is a monorepo for these tools: each is a `cmd/track-fetch-<source>` main built as its
own Nix package (`nix build .#track-fetch-<source>`), sharing the module's dependencies and the
`dataset` contract. The first tool is `track-fetch-rss` (RSS 2.0 / Atom → `event` records:
`time` from the entry's published/updated date, `title`, `url`, optional `--entity`).

## Web clipper

`track-fetch-web <url>` clips one web page into a single `event` record: `time` from the page's
declared publication time (fetch time otherwise), `title`, `url`, plus the extra fields the
contract allows — `markdown` (the readable main content, extracted with a compact readability
heuristic and converted to Markdown; see ADR 0040) and `image` (the lead image URL). The fetch is
SSRF-guarded: private, loopback, and link-local addresses are refused, mirroring the engine's
web-workspace link-preview fetcher; a local file path replaces the URL for testing and for pages
saved from the local network.

Because a clip is note-shaped as much as chart-shaped, the tool also has a convenience output mode
outside the JSONL contract: `--note` prints a ready-to-pipe Markdown note body (provenance line,
lead image, content) for `track new --title`.

## Web element clip

`track-fetch-web` clips a whole page; the Web element clip clips **one element on a page**. It is a
separate tool, not an extension of `track-fetch-web`, for the same reason the web clipper is separate
from the RSS tool: the source shape is different (a browser-selected element payload, not an HTML
document) and so is the safety surface (a hostile page may feed the payload).

### Source

track never runs a browser (ADR 0021). The element clip tool is a pure converter: an external
browser tool captures a page element and hands the result to `track-fetch-elem` as a **grab payload**
on stdin (or `--in <file>`). The payload shape is the contract shared with such tools and mirrors the
fields a browser-inspector grab produces: page context (`sanitizedUrl`, `title`, viewport), the
target element (`tagName`, `selector`, `elementPath`, `fullPath`, `textSnippet`, `htmlSnippet`,
`attributes`, `accessibility`, rects, `computedStyles`), and the surrounding context (`nearbyText`,
`ancestorPath`, `cssClasses`, `selectedText`, `nearbyElements`). The tool accepts these fields
leniently — anything the browser tool cannot provide is simply omitted — but it never invents them.

### Safety: the clip is a converter, not a viewer

Everything in the payload originates in page-controlled DOM, so the tool re-validates and clamps it
before it becomes track data, mirroring the browser tool's own guest/main defense in depth:

- **Budgets** are enforced on every string and array: `textSnippet` ≤ 200, `htmlSnippet` ≤ 4096,
  `selector` ≤ 700, `path` ≤ 900, `cssClasses`/`sourceFile`/`reactComponents` ≤ 500, `nearbyText`
  ≤ 10 entries of ≤ 200, `ancestorPath` ≤ 10 entries, `nearbyElements` ≤ 6 entries of ≤ 160,
  `selectedText` ≤ 500. Oversized values are truncated with a `(truncated)` marker rather than
  dropped, so a clip stays readable while staying bounded.
- **Attributes are allowlisted**: only `id`, `class`, `name`, `type`, `role`, `href`, `src`, `alt`,
  `title`, `placeholder`, `for`, `action`, `method`, and any `aria-*` are kept; event handlers and
  other attributes are discarded.
- **Secrets are redacted**: attribute and metadata values matching credential-shaped patterns
  (`access_token`, `api_key`, `client_secret`, `csrf`, `password`, …) are replaced with `[redacted]`.
  The patterns are deliberately tight — broad words like `code` or `state` would match ordinary CSS
  class names and degrade every real site.
- **URLs are sanitized**: `href`/`src`/`action` values drop query strings and fragments, and
  non-`http(s)` schemes are rejected, so an OAuth callback URL cannot leak its token.
- **Computed styles are a curated subset**: only `display`, `position`, `width`, `height`, `margin`,
  `padding`, `color`, `background-color`, `border`, `border-radius`, `font-family`, `font-size`,
  `font-weight`, `line-height`, `text-align`, `z-index`, with noise values (`auto`, `normal`,
  `static`, `inline`, transparent backgrounds) omitted from the output.

### Output

Like `track-fetch-web`, the clip is note-shaped as much as chart-shaped: the default emits one
`event` record (time from the page's captured timestamp or the run time, `title` from the element's
accessible name/text/`tagName`, plus the rendered element as extra `markdown`/`selector` fields), and
`--note` emits a ready-to-pipe Markdown note body — provenance line, element label, selector/location,
bounds, computed styles, and fenced `html`/text snippets — for `track new --title`.

### Connection to `track web`/clip

The element clip writes into the same `data/` target and reuses the same `--out`/JSONL flow as every
other fetch tool, so `track web` picks clipped elements up with no new wiring. The SSRF-guarded page
fetch is **not** part of this tool: the browser tool has already fetched the page. The tool therefore
never touches the network (no SSRF surface) and never touches the vault directly — it converts one
payload into Canonical JSONL for the caller to store.
