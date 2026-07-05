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
