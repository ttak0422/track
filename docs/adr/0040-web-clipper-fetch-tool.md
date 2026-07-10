# 0040. Web clipper as a fetch tool with an in-repo readability heuristic

Status: Accepted

## Context

Saving a web page into the vault ("clipping") needs three things track deliberately does not do:
fetch a URL, decide which part of the page is the article, and convert that HTML to Markdown.
ADR 0021 already fixed where such work lives — separate `track-fetch-*` binaries that convert the
outside world into Canonical Data Model records — but a clip is note-shaped as much as
chart-shaped, and readability extraction is usually delegated to a heavyweight library
(go-readability and friends), which would be the module's largest dependency by far.

## Decision

**`track-fetch-web` is an ordinary fetch tool emitting one `event` record, with the clip riding in
the extra fields the contract already allows.** The record's canonical fields are `time` (the
page's declared publication time, else fetch time), `title`, and `url`; `markdown` (readable
content as Markdown) and `image` (lead image URL) are extra fields. No new record kind: a clip is
an event with content attached, and the same record can feed a chart (a reading log) or a note.
Note creation stays a shell pipeline — `track-fetch-web --note <url> | track new --title <t>` —
rather than a new track subcommand, keeping track unaware of the network (`--note` is a
convenience rendering of the same extraction, outside the JSONL contract).

**Content extraction is a compact in-repo readability heuristic on the parsed DOM**
(`internal/fetch/web`, on `golang.org/x/net/html` — the module's only new dependency): prune known
chrome (scripts, nav, footers, ad/share/comment classes), then prefer the page's own semantic
container (`<article>`, `<main>`) when it carries real text, else score paragraph parents by text
mass discounted by link density. Markdown conversion is a small best-effort renderer for prose
constructs (headings, lists, quotes, code, tables, links, images).

**The fetch is SSRF-guarded** with the same dial-time public-address check as the engine's
web-workspace link-preview fetcher. The guard is a local copy: the fetch contract keeps tools
independent of the engine, and the check is ~20 lines. Local file paths are the escape hatch for
intranet pages and the replay mechanism for tests, which never touch the network.

## Consequences

- Clipping quality is bounded by the heuristic: article-shaped pages extract well; script-rendered
  apps degrade to page metadata. If real-world quality demands it, a full readability port can
  replace `mainContent` behind the same `Extract` signature — that is the deliberate upgrade path.
- Consumers can rely on every clip being a valid `event` record; anything downstream that reads
  events (charts, future importers) gets clips for free.
- `golang.org/x/net` joins the module's dependencies; heavier scraping/readability libraries stay
  out unless the heuristic's ceiling is actually hit.
