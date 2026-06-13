# ADR 0014: Vault Health Checks and Auto-Numbered Repair

## Status

Accepted

## Context

A track vault is two stores with different durability: the markdown bodies plus
`.track/notes/<id>.yaml` sidecars are authoritative, while the SQLite index is a
rebuildable cache. Many users keep the vault on a cloud-synced folder (e.g. a
`~/track` symlink into OneDrive). Cloud sync can leave the vault in states the
normal create/reindex paths do not produce:

- a markdown file whose sidecar never synced (no title, unresolvable);
- a sidecar whose markdown was removed elsewhere (a full reindex would delete it
  silently, indistinguishable from an intended delete);
- conflict copies such as `1781359469000 (conflicted copy).md` that break the
  `<id>.md` naming rule;
- a sidecar that fails to parse;
- a title shared by two notes after a merge, leaving `[[title]]` ambiguous.

ADR 0013 established that a title is never reconstructed from the body, and
`storage.md` stated track provides no "repair" command at all. That left no way
to even *see* this divergence, and no safe way to recover from it.

## Decision

Add `track doctor`, a read-only health check, and `track doctor --fix`, a
repair pass that restores structure and identity but never invents content.

`track doctor` treats the on-disk markdown and sidecars as the source of truth
(the index is ignored) and reports divergence as a JSON `issues` array:
`missing_sidecar`, `orphan_sidecar`, `stray_file`, `unreadable_sidecar`, and
`duplicate_title`. Finding issues is not an error: it exits 0, reserving the
`{"error":...}`/exit 1 contract for real failures.

`track doctor --fix` repairs by **auto-numbered restore**, then reindexes:

- `missing_sidecar`: write a sidecar with a fresh `Untitled N` title.
- `orphan_sidecar`: recreate the missing markdown as an empty note.
- `duplicate_title`: keep the lowest id's title; renumber the rest to fresh
  `Untitled N` titles.
- `stray_file`: import the file as a new note with a fresh time-based id and an
  `Untitled N` title.
- `unreadable_sidecar`: reported under `skipped`, never auto-fixed, because the
  intended contents are unknown.

Detection logic lives in one internal `scan()` so `Diagnose` and `Fix` cannot
drift.

## Consequences

Repair is intentionally lossy in a predictable direction: it recovers a note's
*existence and uniqueness* but never its original title, tags, or Babel block
results. This keeps the guarantee from ADR 0013 — metadata is never invented
from body text — while still giving a one-command recovery from a partially
synced vault. A backup of `.track/notes/` remains the only way to recover the
original metadata, so the durability rules in `storage.md` are unchanged.

Because `--fix` mutates the vault (writing sidecars, recreating and renaming
files), the read-only `track doctor` is the default; `--fix` is opt-in and
rebuilds the index afterward so the cache matches the repaired vault.
