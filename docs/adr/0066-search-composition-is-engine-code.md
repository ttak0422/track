# 0066: The title-then-body search composition is engine code

## Status

Accepted (2026-07-31)

## Context

The store answers two different searches. `SearchScoped` is a title/tag query
ranked by a packed rank vector; body search is `SearchBodyFTS`, a trigram FTS5
query ranked by bm25, with a scan fallback for terms too short to trigram. What
a user calls "search" is neither: it is title hits first, then full-text hits
for notes the titles did not already name, with each body hit carrying the line
it matched on and a snippet of it — which means reading the matched files, so
it needs the vault config as well as the store.

That composition existed only in `internal/cli`. The web workspace could not
reuse it: `internal/cli` imports `internal/track/webui`, so the dependency
cannot run the other way. `/api/search` therefore called `SearchScoped`
directly and was **title-only** — with `store.SearchAll` as the argument, which
the store treats as the title query (body has its own method), so the call read
as if it searched everything while it did not.

Multi-vault made it worse: the web merged each vault's page on `Rank`, ascending.
bm25 is negative and title ranks are 0..3, so the moment a page carried both,
every body hit would sort above every title hit.

## Decision

Move the composition to `internal/track/search`: `Scoped` (one vault) and
`Federated` (several), plus the body-search helpers and the file reading that
produces line and snippet. `internal/cli` and `internal/track/webui` both call
it; neither owns it. This is the rule CLAUDE.md already states — reusable engine
code lives under `internal/track/*` so integrations need not depend on the
command layer — applied to the case that had violated it.

Each result carries `match` ("title" or "body"), set at the composition point.
A frontend groups on that rather than on the presence of a snippet, because a
body hit whose terms straddle lines legitimately has neither line nor snippet.

`Federated` keeps its two phases apart — all vaults' title hits merge into one
page, then all vaults' body hits into another — since the two rank scales are
not comparable.

## Consequences

- The web search box is full-text, from the same endpoint and the same
  composition the CLI and telescope use; there is one ranking story, not two.
- `--scope all` keeps a single shared limit: title hits spend it first, so a
  query matching `limit` titles yields no body group. Left as is (marked in the
  code) rather than doubling the CLI's row budget for a case nobody has hit.
- Each search now reads up to `limit` matched files for line and snippet — the
  cost telescope already paid, new for a long-lived server.
- The published static site is full-text too, by running the composition's
  **scan path** in the browser — see ADR 0067. It has no server, so it cannot
  run the FTS index; but the scan is the fallback the engine already ships for
  queries the index cannot serve, so the published site runs engine behaviour
  rather than a second, different search.
- A body hit now reaches the browser with the line it matched on. The web note
  route has no scroll-to-line yet, so the line is carried but unused — the
  natural follow-up.
