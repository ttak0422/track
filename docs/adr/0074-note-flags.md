# 0074. Note flags are implementation-defined markers

Status: Accepted

## Context

A note needs author-assigned markers such as `DEPRECATED` or `CONFIDENTIAL`, shown as a large, translucent
English-text watermark behind the article (and as badges in lists). Unlike NEW/read — computed
reading milestones (ADR 0072) — these are static markers the author assigns and that persist in the
sidecar.

The requester's constraints:

- A note can carry several flags at once (both `DEPRECATED` and `CONFIDENTIAL`).
- The value set is strictly closed: flags are provided by the implementation, not user-extensible,
  and new flags are added gradually, each with its own specialized behavior.
- `DEPRECATED` should lower the note's search ranking, not merely annotate it.

## Decision

Flags live in the sidecar as `flags: [DEPRECATED, CONFIDENTIAL]` (metadata version 10): a normalized,
sorted list of implementation-defined values. Unknown values are rejected at write time — the set is
closed, so the sidecar remains a parseable contract.

A code-level registry defines each flag and its behavior. v1 ships two:

- `DEPRECATED` — red watermark, list badge, and a search-rank penalty.
- `CONFIDENTIAL` — blue watermark and list badge (display only in this pass).

Per-flag behavior is implemented explicitly per code path (a switch over the flag), not a generic
data-driven color table. Adding a flag means extending the registry, its validation, its CSS, and its
search behavior together — deliberately gradual.

The index carries `notes.flags` (joined and sorted) so search can rank `DEPRECATED` down: the
title/tag rank vector and the body bm25 path both subtract a constant for a note carrying it. Editing
flows through the existing paths — CLI `track meta --flag/--unflag`, the web meta dialog, and the
Neovim popup via `MetaDoc`. Rendering uses repeating SVG text masks behind each body pane, with
`--note-flag-deprecated` (red) and `--note-flag-confidential` (blue) supplying distinct theme colors.
`--note-watermark-opacity` increases visibility without fading the content. An isolated surface keeps
the negative watermark layer above the pane background and below all content; the masks take no pointer events or selection. Both flags occupy separate lines
in each tile. The container supplies an accessible label. Badges remain in lists, keyed by
`.note-flag-badge-<flag>`, so a flag is visible before opening a note.

## Consequences

- Sidecar metadata bumps to version 10; the index schema bumps to 10 (rebuild, no migration).
- `CONFIDENTIAL` is display-only here. Per-note publish exclusion (visibility) stays unimplemented,
  as before — the chosen route is one vault per visibility level.
- A hand-written unknown flag in the sidecar is a validation error surfaced on edit, keeping the
  sidecar parseable rather than silently tolerating drift.

