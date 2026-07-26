# 0052. Cross-vault reads federate per-vault databases with ATTACH

Status: Accepted; the ATTACH mechanism below is superseded by
[0062](0062-cross-vault-reads-merge-in-go.md), which merges per-vault queries in Go instead. The
identity model this ADR decided — a `vault` field only where a read crosses, physical databases
one-per-vault and schema-unchanged, per-vault degradation rather than a failed search — still holds.

## Context

ADR 0051 gave one machine several named vaults, each with its own index database. Search should
cross them: a note lives in whichever vault matches its publication level, but "where did I write
about X" is one question. Two obvious shapes were rejected in the multi-vault design round: a
single merged index (every primary key becomes (vault, id), the FTS rowid mapping breaks, every
query and consumer changes), and per-vault queries merged in application code (loses a single
global ORDER BY/LIMIT, and each further crossing surface — backlinks, graph — would grow its own
merge code).

Ids collide across vaults by construction — journal ids are dates, note ids are creation
timestamps — so any crossing surface must carry (vault, id), while vault-local commands should not
change at all.

## Decision

- Cross-vault search opens one in-memory SQLite connection and ATTACHes every reachable vault's
  index database (generated schema aliases v0, v1, …; SQLite's attach limit bounds how many join one
  query, not how many the registry may hold — the rest are reported as skipped).
  Physical databases stay one-per-vault and schema-unchanged; only the query crosses.
- The federated query is a UNION ALL of per-vault subqueries — each single-vault, schema-prefixed,
  labeling its rows with the vault name — under one global ORDER BY (rank columns selected
  explicitly) and LIMIT, with the vault name as the final tiebreak so ordering is deterministic.
  bm25 scores from different FTS indexes are only approximately comparable; the merged ranking
  accepts that.
- Identity is upgraded only where it crosses: search results gain a `vault` field (`""` for the
  unregistered active vault) as the (vault, id) identity, and follow-ups target a foreign hit with
  the global `--vault` flag. Vault-local commands keep bare ids. Backlinks and graph stay
  vault-local until cross-vault links exist (phase 3), which is when their wire formats gain the
  vault field — a constant label before then would be dead scaffolding.
- Before federating, each vault self-heals (RefreshIfStale) over its own single-vault connection;
  a vault that cannot be healed or attached is reported in the response (`unavailable`) instead of
  failing the search. Store connections set busy_timeout so concurrent processes sharing these
  databases wait instead of failing SQLITE_BUSY.
- Staleness detection includes sidecars: the index stores each sidecar's mtime (notes.meta_mtime),
  so a synced tag/title edit that never touches the note body still refreshes. Orphan sidecars are
  excluded from the comparison — a read must not rebuild in a loop over a file only explicit
  commands may move (ADR 0051).

## Consequences

- One `track search` answers across all registered vaults, degrading gracefully per vault; agents
  learn the hit's vault from the row and scope follow-ups with `--vault`.
- The federation layer is the template for the remaining crossing surfaces: phase 3 backlinks/graph
  reuse the same attach-and-union shape with (vault, title) edges.
- The attached connection is read-write by discipline (SELECT-only); if a write ever sneaks in,
  switch the ATTACH to mode=ro URIs.
