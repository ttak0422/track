# 0053. Cross-vault references are explicit, string-keyed, and gated on registered names

Status: Accepted

## Context

With several vaults per machine (ADR 0051) and search crossing them (ADR 0052), notes want to
reference notes in other vaults. Obsidian-style implicit resolution (search every vault until a
title matches) was rejected in the multi-vault design round: it makes resolution order significant,
couples every vault to every other, and turns a new vault into a behavior change for existing
links. The other constraint is identity: note ids are vault-local (ADR 0052), so a cross-vault
edge must never record the target's numeric id.

## Decision

- The reference form is `[[vault:title]]` — explicit prefix only, no fallback. An unqualified
  `[[title]]` stays strictly vault-local.
- The prefix is a qualifier **only when it is a registered vault name** (the gate); any other
  colon-containing key remains an ordinary local title. The gate runs before local title
  resolution everywhere (indexer, LSP), so a registered name always wins; `track doctor` reports
  local titles a registration shadows (`shadowed_title`) since registering a vault can create
  these retroactively. Rename is naturally safe: backlink rewriting matches the full key text, so
  renaming a local "title" never touches a qualified "vault:title".
- **A vault's own name is not a qualifier.** The gate is registry membership and the active vault is
  in its own registry, so `[[own:Title]]` used to take the external branch: an `ext_links` row and no
  `links` row, which made the edge invisible to backlinks, orphans and both graphs, while the CLI
  reported it under `external` and the web reported it nowhere. A self-qualified key now folds back
  into the ordinary local lookup at every gate site, which is what anyone writing it means. Databases
  written before this keep their stale self `ext_links` rows until a full reindex.
- Outgoing edges are stored in the source vault's own index as `(vault name, title)` strings
  (`ext_links`, schema v7) — never the target's id. Inbound backlinks are found by scanning the
  other registered vaults' databases for rows naming this vault (under any of its registered
  names); `track backlinks` reports them under `external`, and vaults that cannot be consulted
  appear under `unavailable` — a missing backlink must be distinguishable from a missing vault.
- Resolution never creates or rebuilds another vault's cache: a vault whose directory or index is
  missing is "unavailable", and each vault's own processes keep its index fresh.
- Anchors and aliases compose unchanged: `[[vault:title#Heading|alias]]` splits display and anchor
  exactly as before, with the vault gate applied to the remaining key. Journal titles are dates,
  so `[[vault:20260723]]` resolves like any other title — deliberately not special-cased (decided
  2026-07-25: no dedicated allow/deny code for journals).
- LSP: the qualifier gate runs before the dictionary in definition, hover, document links,
  diagnostics, and the save-time link refresh. Diagnostics distinguish three cases: resolved
  (silent), title missing in a reachable vault (warning naming the vault), and vault unavailable
  (Information, `vault-unavailable`) — an explicit gap instead of a silent drop. Completion opts
  into another vault's dictionary only once its registered prefix is typed.
- The Neovim plugin starts one LSP client per vault, rooting each at the buffer's own vault
  (active or registered) with `TRACK_VAULT` scoped to it.

## Consequences

- Cross-vault links survive target renames only as well as titles do: the edge is the title
  string, so renaming a note in vault B breaks `[[b:old]]` references in vault A. That is the
  accepted best-effort boundary (rewriting is same-vault only; phase 6 may add best-effort
  cross-vault repair) — the broken ref then shows up as "no note titled ... in vault b".
- Graph surfaces don't cross yet: ext edges are indexed but only backlinks/resolve/LSP consume
  them; the graph and web views join in the vault-aware web phase.
- The registered-name gate is a heuristic by design; the shadowed-title lint is its guard rail.
