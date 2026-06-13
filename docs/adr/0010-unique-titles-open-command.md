# ADR 0010: Unique Note Titles via an `open` (Create-or-Open) Command

## Status

Accepted

## Context

A note's sidecar metadata title is also a link keyword: `[[title]]` resolves against titles by exact match, and ambiguous terms fall back to the first note by id (`store.ResolveTerm` uses `LIMIT 1`). If two notes share a title, every `[[title]]` silently points at only one of them and the other becomes unreachable by name.

The original creation path did not enforce this. `track new` only guarded against an existing *file id*, not an existing *title*, and the Neovim frontend created notes with `new --title <t> --id <os.time()>`. Triggering creation twice for the same title (e.g. from the word under the cursor) minted a second note with a duplicate title. The LSP `track.createNote` command already refused duplicate titles, so the two creation paths disagreed.

## Decision

Titles are unique, and creation enforces it at every entry point.

- A new strict primitive and an idempotent opener split the two intents:
  - `track new --title <t>` **creates** and fails when the title already resolves. It no longer mints duplicates.
  - `track open --title <t>` **resolves or creates**: if the title resolves it returns that note (`created: false`); otherwise it creates one (`created: true`). Repeating `open` on the same title is a no-op that just returns the note.
- Both routes share one `createTitledNote` helper, and both check `ResolveTerm` before writing, so the LSP `createNote`, the CLI, and the Neovim frontend all uphold the same invariant.
- The Neovim frontend exposes only `:Track open` (the former `:Track new` is removed) and calls the `open` CLI command, so acting on an existing title opens it instead of erroring or duplicating. A reindex runs only when `open` actually created a note.

## Consequences

Sidecar titles stay unique by construction, so `[[title]]` resolution is unambiguous and no note is shadowed by a namesake. The common editor gesture — "make a note for this word" — becomes safely repeatable: the second invocation opens the first note.

`open` resolves against the index, so a note that exists on disk but is not yet indexed could still be missed and duplicated; this matches the existing keyword-resolution model and is the price of an O(1) lookup. `new` remains for callers that want an explicit id or an error on collision (scripts, tests), while `open` is the default interactive path. Uniqueness is enforced on titles; this ADR does not add a separate uniqueness constraint for ids, which `createTitledNote` still guards by refusing to overwrite an existing file.
