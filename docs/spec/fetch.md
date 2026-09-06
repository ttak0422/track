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
