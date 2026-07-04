---
name: track
description: >-
  Use the track CLI for linked Markdown knowledge-base maintenance beyond basic
  note creation or read-only search: renaming notes and fixing backlinks,
  checking backlinks/graphs, running diagnostics, maintaining indexes, or
  coordinating multi-step vault workflows. For creating notes use
  track-create-note; for searching or reading notes use track-search-notes.
---

# track CLI

The track CLI is the source of truth for notes, indexing, search, and link resolution. The Go engine parses notes, maintains the SQLite index, and resolves `[[links]]`.

## When to use

Use this general skill for maintenance tasks such as renaming a note and fixing backlinks, inspecting backlinks or graph context as part of a workflow, reindexing/diagnostics, or coordinating multiple track operations.

Use the narrower skills when they fit:

- `track-create-note`: create/open notes, journals, and template-backed notes.
- `track-search-notes`: search, resolve, export/read notes, backlinks, and graph inspection without modifying notes.

## Prerequisites

- `track` binary on `PATH`. (Only when developing track itself, with the source repo as the working directory, `go run ./cmd/track` works as a substitute.)
- Prefer the user's normal track config. `TRACK_VAULT` is only for tests and one-off overrides.

## Core commands

- `track rename (--id N | --title S | --path P) --to <s>` — rename title and rewrite backlinks.
- `track backlinks (--id N | --path P)` / `track graph (--id N | --path P)` — inbound links / local graph.
- `track doctor [--fix]` — diagnose or repair vault/index drift when available in the current build.
- `track reindex [--full]` — rebuild the SQLite index.
- `track export (--id N | --title S | --path P)` — full note Markdown to stdout.
- `track toggle (--id N | --title S | --path P) --line N [--state toggle|check|uncheck]` — flip or set a task checkbox.
- `track rm (--id N | --title S | --path P)` — soft-delete a note into `.track/trash` (track never empties it) and reindex.
- `track gen increment|undo|redo|list|peek` — vault generation snapshots (undo/redo across bulk edits); see below.

## Generations (bulk-edit safety net)

`track gen` snapshots the vault's notes and metadata as numbered generations with an undo/redo
cursor — the release model: `increment` cuts an immutable save point, `undo`/`redo` check one out.
Bracket any bulk rewrite (mass rename/update/rm, memory consolidation) with generations so the run
is reviewable and rejectable:

```sh
track gen increment    # seal the pre-run state
# ... rename / update / rm notes ...
track gen increment    # approve the result
# or
track gen undo         # reject; the run's output is auto-saved, redo revisits it
```

- `track gen list` reports generations, the `cursor`, and `dirty` (unsaved changes). Check `dirty`
  before `undo`/`redo`: off the head, a cursor move discards unsaved changes.
- `track gen peek [--gen N] (--id N | --title S | --path P)` prints a note's Markdown as of a
  generation (default: cursor) without moving anything — deleted notes peek by `--id`. For a
  partial restore, diff the peeked body against the current note and write back the wanted parts
  with `track update`.
- Snapshots cover note bodies, journals, and sidecars only; `assets/` is excluded.

## Updating task checkboxes

To change a `- [ ]`/`- [x]` task item, prefer `track toggle` over editing the Markdown by hand: it
flips exactly one box and leaves the surrounding text untouched. First locate the box's line number
(`track search --scope body …` and `track export` both report 1-based line numbers), then:

```sh
track toggle --id 1781359469000 --line 14            # flip the box on line 14
track toggle --title "Tasks" --line 3 --state check  # idempotent: force checked
```

The result reports the resulting `checked` state and whether anything `changed`. `--state check`/
`uncheck` are idempotent, so re-running is safe. Toggling reindexes the note automatically.

## Output contract

All commands print single-line JSON; errors are `{"error":...}` with exit code 1. Parse stdout as JSON.

## Conventions

- Titles are link keywords; write `[[Title]]` in bodies to link. Title lives in sidecar metadata, not the body, so a body may start with any `#` heading.

## Typical workflow

For maintenance workflows, inspect the target (`resolve`, `export`, `backlinks`, or `graph` as needed) → apply the maintenance command such as `rename` or `doctor --fix` → verify with search/backlinks/export. Prefer the narrower create/search skills for simple note creation or read-only lookup.

## Example

```sh
track rename --title "Old title" --to "New title"
```

## For track contributors

When working inside the track source repository (the working directory is the repo root), the canonical, fuller CLI contract lives at `docs/spec/agent-workflows.md`. It is not required to use this skill — the commands above are self-contained.
