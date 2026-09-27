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
- **`time` is RFC 3339** — except that a record whose whole identity is a day (a daily OHLCV bar)
  carries a date-only `time` (`YYYY-MM-DD`), which category axes label directly. Source timestamps in
  other formats are normalized by the tool, so downstream consumers never parse source-specific dates.
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

## Market data

`track-fetch-jquants` converts J-Quants daily quotes into `price` records — one daily OHLCV bar per
line. `--code` selects the issue, `--from`/`--to` bound the range, and `--entity` names the series
(defaulting to the code). The refresh token is read from `TRACK_JQUANTS_REFRESH_TOKEN` — an
environment variable, never a flag, so it stays out of shell history — and the tool exchanges it for
the short-lived ID token itself. Days without a full price set (halts) are skipped and counted on
stderr; split-adjusted prices are preferred over raw ones so a chart survives a split. Daily bars
carry the date-only `time` described above.

## Web clipper

`track-fetch-web <url>` clips one web page into a single `event` record: `time` uses the page's
publication metadata only when it is an explicit zoned instant; otherwise it uses the retrieval
instant. A modified timestamp is never treated as publication time. Date-only and timezone-less
publication values are not instants. The record also carries `title`, `url`, and the extra fields the
contract allows — `markdown` (the readable main content, extracted with a compact readability
heuristic and converted to Markdown; see ADR 0040), `image` (the lead image URL), `published` and
`modified` (each with `raw`, `precision`, and nullable `timestamp`), and `retrieved_at` (the RFC 3339
fetch instant). `modified` uses document metadata first and the HTTP `Last-Modified` header only as a
fallback. The fetch is
SSRF-guarded: private, loopback, and link-local addresses are refused, mirroring the engine's
web-workspace link-preview fetcher; a local file path replaces the URL for testing and for pages
saved from the local network.

Because a clip is note-shaped as much as chart-shaped, the tool also has a convenience output mode
outside the JSONL contract: `--note` prints a ready-to-pipe Markdown note body (retrieval-dated
provenance line, any declared publication/modification labels, lead image, content) for
`track new --title`.

The note provenance line's `retrieved YYYY-MM-DD` date comes from retrieval time, not the page's
publication or modification date. It is intentionally a date-only note label; use `--snapshot-dir`
when the exact retrieval instant and source bytes must be retained.

### Reproducible web snapshot

`--snapshot-dir DIR <url>` is an explicit URL-acquisition mode, mutually exclusive with `--note` and
`--out`. It accepts HTTP(S) only, fetches once through the same SSRF-guarded client, and rejects a
response body larger than 20 MiB rather than saving a truncation. It writes the body bytes exposed by
`net/http` (after any transparent content decompression) as `original.html`, plus the extracted
Markdown as `text.md`. `text.md` is only the extracted content: it has no source header, summary, or
retrieval banner. `DIR` is the snapshot container (created if absent); each run creates a unique
`snapshot-*` child and absolute paths to its fixed `original.html` and `text.md` files. The child and
files are created exclusively (child mode `0700`, file mode `0600`), so existing snapshots are never
overwritten. If a write fails, the command removes the partial snapshot when possible and reports any
remaining path to stderr.

On success stdout contains exactly one compact JSON manifest line (no JSONL record or summary). The
hash fields below are illustrative placeholders; actual SHA-256 values contain 64 lowercase hex characters:

```json
{"schema_version":1,"source_url":"https://example.com/article","final_url":"https://example.com/article","retrieved_at":"2026-09-27T12:34:56.123456789Z","original_path":"/snapshots/article/snapshot-abc123/original.html","text_path":"/snapshots/article/snapshot-abc123/text.md","original_sha256":"0000000000000000000000000000000000000000000000000000000000000000","text_sha256":"0000000000000000000000000000000000000000000000000000000000000000000000","extraction_method":"readability-v1","published":{"raw":"2026-09-26T10:00:00-04:00","precision":"instant","timestamp":"2026-09-26T10:00:00-04:00"},"modified":{"raw":"","precision":"absent","timestamp":null}}
```

Manifest v1 fields are fixed: `schema_version` is `1`; `source_url` is the requested URL;
`final_url` is the response URL after redirects; `retrieved_at` is the RFC 3339 instant recorded
after the response body has been received; the absolute `*_path` values name the secured files; and
each `*_sha256` hashes that file's bytes. `published` and `modified` each contain the
selected metadata `raw` value as returned by the HTML parser or response header (empty when absent),
`precision`, and `timestamp`
(an RFC 3339 instant for a known instant, otherwise JSON `null`). Precision is `instant` for an
explicitly zoned instant, `date` for a date-only value, `local_datetime` for a timezone-less
date-time, `unknown` for an unrecognized non-empty value, and `absent` when no value was declared.
`local_datetime` therefore retains its wall-clock precision without inventing an instant. The
`published.raw` value comes from document publication metadata. `modified.raw` comes from document
update metadata, falling back to the HTTP `Last-Modified` header only when the document declares no
update value. A header with an explicit `GMT`/`UTC` zone is an instant; offsetless values are never
assigned UTC. The
`extraction_method` value `readability-v1` identifies the Markdown conversion. The manifest is
stdout-only; it is not written into the snapshot directory.

The HTML-to-Markdown converter handles simple pipe tables. Tables with captions, multiple header
rows, row/column spans, semantic header associations, or ragged rows are emitted as fenced HTML with
a notice rather than flattened into guessed columns. Row/column spans are not expanded into Markdown
grid positions.

Example:

```sh
track-fetch-web --snapshot-dir ./article-snapshot https://example.com/article
```

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

## Kindle clipper (experimental)

`track-fetch-kindle <clippings.txt>` converts a Kindle "My Clippings.txt" export into one `event`
record per clipping: `title` from the clipped text, `entity` from the book title (the trailing
author group is stripped, so the entity is the book itself), `time` from Amazon's added-on instant —
English and Japanese annotation lines are recognized, and because the export carries no zone, times
normalize to RFC 3339 UTC — ordered ascending. `type` (highlight / note / bookmark), `location`, and
a content-derived `anchor` ride along as extra fields.

The file is normalized in place (UTF-8 BOM, CRLF/CR line endings, split on Amazon's `==========`
separator), so the same export parses identically however it was transferred. Malformed blocks are
skipped and counted on stderr; records are deduplicated by their deterministic anchor, so re-running
the tool never doubles a highlight.

Like the web clipper, a convenience output mode sits outside the JSONL contract: `--note` prints a
ready-to-pipe Markdown note body for `track new --title` — an `up::` property pointing at the book,
then one list item per clipping carrying its stable `^h…` anchor, so `[[Book#^id]]` quotes keep
resolving across regeneration (ADR 0038). `--book <title>` restricts the export to one book (and
names the note's `up::` parent); it is required when the file holds several books:

```sh
track-fetch-kindle "My Clippings.txt" --out ~/track/data/books.jsonl
track-fetch-kindle --note --book "The Pragmatic Programmer" "My Clippings.txt" \
  | track new --title "The Pragmatic Programmer — highlights"
```
