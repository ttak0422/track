# 0062. Cross-vault reads merge per-vault queries in Go

Status: Accepted

Supersedes the mechanism decided in [0052](0052-federated-cross-vault-search.md). Its identity
model — a `vault` field only on the surfaces that cross, physical databases one-per-vault and
schema-unchanged, per-vault degradation rather than a failed search — stands unchanged.

## Context

ADR 0052 crossed vaults by ATTACHing every reachable index to one in-memory connection and running a
generated `UNION ALL` of schema-prefixed subqueries under a single `ORDER BY` and `LIMIT`. It rejected
the obvious alternative — query each vault, merge in application code — on two grounds: that merging
in Go "loses a single global ORDER BY/LIMIT", and that "each further crossing surface would grow its
own merge code".

Neither survived. The first is simply not true: each vault's own query already sorts by the same total
order, so a k-way merge over per-vault top-k *is* the global top-k, exactly and not approximately. The
second was falsified by the next phase of the same design round — cross-vault backlinks (ADR 0053)
shipped as `vaultref.Inbound`, which loops the registered vaults, calls the ordinary single-vault
`ExtBacklinks`, and concatenates. It attaches nothing. The federated search path itself already
contained the Go merge, for the short-term body-search fallback that trigram FTS cannot serve.

What ATTACH cost was a second implementation of ranking. `Federated.Search` was a hand copy of
`searchTagged` and `searchQuery` — same kind filter, same title-match clause, same tag-EXISTS clause,
same CASE rank vector — kept in step by hand. It did not even win on work done: the subqueries carried
no `LIMIT`, so every matching row in every vault was materialized and sorted before the outer limit
applied, where a per-vault query transfers at most `limit` rows each.

And it imported a constraint the problem does not have. SQLite attaches ten databases by default, so
an eleventh vault dropped out of the query. That produced a third outcome a vault could be in —
reachable, but not in this query — with its own type and reporting channel, which the CLI never
consulted: past ten vaults `track search` silently returned short, the exact failure the degradation
machinery existed to prevent.

## Decision

- Cross-vault reads run each vault's ordinary single-vault query and merge the results in Go. There is
  no federated connection, no generated SQL, and no attach limit.
- **SQL stays the only source of ranking semantics.** The single-vault queries now select their sort
  key as a column rather than only ordering by it: a title/tag search packs its 0/1 CASE vector into
  one integer (`rankExpr`), and a body search selects `bm25`. Both land in `SearchResult.Rank`
  (`json:"-"`). Go never recomputes a rank from the row — SQLite's `LIKE` and `COLLATE NOCASE` and
  Go's case folding disagree on non-ASCII text and on `%`/`_` inside a query, so a reimplementation
  would rank differently from the vault's own answer.
- `MergeSearchResults` orders by `(rank, mtime desc, id desc, vault)` — the single-vault `ORDER BY`
  expressed in Go, with the vault name as the final tiebreak so the merged order is deterministic.
- The CLI keeps its two phases. `searchResults` composes title-then-body internally, so per-vault
  composed lists cannot be merged directly without interleaving bm25-ranked body hits into the title
  hits: title hits merge across vaults first, then body hits with per-vault skip sets.
- A vault has two outcomes in a cross-vault read: searched, or reported `unavailable` with a reason.
  The "skipped" state goes with ATTACH.

## Consequences

- `store/federated.go` and its test are gone, with four types and three hand-generated SQL builders.
  The remaining ranking code has one home.
- The registry is no longer bounded by SQLite's attach limit for search, and `track web` no longer
  lists a gap for the eleventh vault.
- Each vault's index is opened once per read instead of twice — the callers already opened every
  vault's store to self-heal it before attaching the same files again.
- bm25 scores from different FTS indexes remain only approximately comparable, exactly as ADR 0052
  accepted. Merging per-vault top-k adds one further approximation for body search: a vault's
  `limit+1`-th hit can no longer displace another vault's `limit`-th. At the scale this tool runs
  at, that is not observable.
- The read-write ATTACH the old connection used — SELECT-only by discipline, flagged in a comment as
  something to switch to `mode=ro` URIs if a write ever slipped in — no longer exists to worry about.
