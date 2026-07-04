# ADR 0025: Generation Snapshots and Soft Delete

## Status

Accepted.

## Context

Agents (memory-consolidation skills in particular) apply bulk rewrites to a
vault: merging duplicate notes, rewriting stale facts, deleting obsolete ones.
Such a run must be reviewable and reversible — the consolidation model is
"never destroy the input; adopt the output after review". Git would provide
that, but track is designed around cloud-storage auto-sync (ADR 0004, ADR
0014): most vaults are not git repositories, and a `.git` directory full of
pack files is exactly the kind of opaque blob cloud sync handles worst. The
vault also had no way to delete a note at all, so a consolidation could not
even retire a merged-away note safely.

## Decision

Add vault-level generation management to the engine (`internal/track/gen`, CLI
`track gen`) and a soft delete (`track rm`), designed for CLI-driven agent
workflows.

The model is a git release: a generation is an immutable save point, the
working vault is a disposable working tree, `increment` cuts a release, and
`undo`/`redo` check one out. Five subcommands: `increment`, `undo`, `redo`,
`list`, `peek`.

- `increment` saves the working vault as a new generation, drops generations
  past the cursor (history is linear), and prunes the oldest beyond `gen_keep`
  (config, default 10). When the working vault equals the cursor generation it
  reports `changed: false` instead of duplicating it.
- `undo` moves the cursor back one generation and restores it. Only at the
  head with unsaved changes does it first auto-save them as a new generation
  (so nothing is silently lost and `redo` can revisit them). Anywhere else,
  unsaved changes are discarded — restoring a release elsewhere would have to
  destroy future generations to preserve them, which turns "go back" into
  "erase the future".
- `redo` moves the cursor forward one generation, discarding unsaved changes
  like a release checkout.
- `peek [--gen N] (--id|--title|--path)` prints a note's content as of a
  generation (default: the cursor) without moving anything. Selective revert
  is therefore a read-side feature: diff the peeked content against the
  current note and write back only the wanted parts with `track update` — no
  transaction machinery needed.

Storage is a complete file copy per generation under `<vault>/.track/gen/<n>/`,
covering `note/`, `journal/`, and the `.track` sidecars (`notes/`,
`renames.yaml`). Delta or content-addressed storage is overkill for a note
vault and would create pack-like blobs that sync poorly; full copies are
trivial to implement, debug, and inspect. `assets/` and `data/` are excluded
so binary bulk never multiplies into cloud storage; `undo` restores note text
and metadata only. The store lives under `.track` so the indexer never scans
it and generations never snapshot it recursively, but it does sync — undo
working on every device is worth more than the space.

`track rm (--id|--title|--path)` moves a note file and its sidecar into
`<vault>/.track/trash/` with a timestamp prefix, then reindexes. track never
empties the trash itself.

After `undo`/`redo` the CLI resets and fully rebuilds the SQLite index; the
index is a rebuildable cache (ADR 0002), and `RefreshIfStale` remains the
safety net for readers that observe restored files before any rebuild.

Concurrent cloud-sync edits can race a restore and lose an edit. That is
accepted: memory notes degrade gracefully a few generations off, and the
alternative — merge machinery over synced snapshots — is the complexity this
design exists to avoid.

## Consequences

A consolidation run becomes structurally non-destructive:

```sh
track gen increment   # seal the pre-run state
# ... agent rewrites, renames, rm's notes ...
track gen increment   # approve: the result is the new head
track gen undo        # reject: back to the pre-run state; the rejected
                      # output survives as the auto-saved generation, redo revisits it
```

Vaults grow by up to `gen_keep` full text copies of the note trees; retention
is count-based and prunes automatically on `increment`. Generations restore
whole vault states, not individual notes — per-note history stays out of
scope, and `peek` plus `track update` covers the partial-restore need.
