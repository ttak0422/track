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
