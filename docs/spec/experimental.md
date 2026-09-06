# Experimental Features

Some track features land in the tree before they are proven. This document defines what
*experimental* means, how the help site marks a feature as experimental, and when a change may be
added experimental instead of going through the normal verification path.

## Definition

A feature is **experimental** when its interface is not yet stable: its commands, flags, file
formats, or renderers may change or disappear without notice. An experimental feature is outside the
stable CLI contract of `docs/spec/agent-workflows.md`. The marker is the author's judgment of
stability and is never a commitment to keep or finish the feature.

## Notation in the help vault

`docs/help` is a track vault, not a directory of Markdown (ADR 0059). Mark an experimental feature in
its page's prose, matching the existing help style — lowercase, in parentheses, suffixed to the thing
it qualifies:

- **A section** whose whole content is experimental appends `(experimental)` to that section's
  heading:

  ```markdown
  ## Tangling source to files (experimental)
  ```

- **A whole page** that is experimental appends the marker to the page title:

  ```markdown
  # Babel (experimental)
  ```

- **A single flag or subcommand** that is experimental is marked with an inline sentence at its first
  mention, for example:

  ```markdown
  `--suffix` is experimental and may change without notice.
  ```

The marker is prose, not a flag: unlike `DEPRECATED` and `CONFIDENTIAL` (ADR 0074) it neither stamps
the page nor affects search ranking, and it is removed simply by editing the text when the feature
stabilizes.

## When a change may be experimental

The boundary is whether the change touches anything that already exists:

- **Additive and isolated.** A new command, flag, format, or renderer that does not change existing
  behavior and does not break existing tests may be added as experimental *without mandatory new
  tests* and without the full verification pass. The existing test suite must still pass.
- **Anything that touches existing behavior or tests.** A change that alters how an existing feature
  behaves, changes an existing output, or requires editing existing tests goes through the normal
  verification path: tests updated and added, docs updated, review as usual — experimental marking
  does not waive that.

When an experimental feature stabilizes, removing the marker is itself a normal change: it joins the
stable contract, gets its tests, and stops promising a changeable interface.

## Scope

This document governs new work only. Existing experimental branches that already sit unmerged (for
example `feat/fetch-kindle`) are not retroactively re-marked or re-verified under this policy.