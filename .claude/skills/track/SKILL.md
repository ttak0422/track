---
name: track
description: Use the track CLI when the user asks to create, record, search, rename, or maintain notes, journal entries, Zettelkasten items, or linked Markdown knowledge in a track vault (the vault directory set by $TRACK_VAULT).
---

# track CLI

The track CLI is the source of truth for notes, indexing, search, and link resolution. The Go engine parses notes, maintains the SQLite index, and resolves `[[links]]`. Use it for any note/journal/Zettelkasten request in a configured track vault.

## When to use

Trigger on requests such as: take a note, record this, start a journal entry, find a note, rename a note (and fix backlinks), or maintain a knowledge base of linked Markdown.

## Prerequisites

- `track` binary on `PATH`. (Only when developing track itself, with the source repo as the working directory, `go run ./cmd/track` works as a substitute.)
- `TRACK_VAULT` set to the vault directory. Without it, commands error.

## Core commands

- `track new --title <t> [--body <s>] [--tag <s>] [--ai]` — create a note (fails if the title exists). Body from `--body` or stdin; leading `#` headings are fine.
- `track open --title <t> [--body <s>] [--tag <s>] [--ai]` — create-or-open by title (idempotent).
- `track journal [--offset <n>] [--body <s>]` — open/create a daily note.
- `track append (--id N | --title S | --path P) [--body <s>] [--tag <s>]` — append body and/or merge tags.
- `track search --query <s> [--scope all|title|body] [--limit N]` — search; `#tag` terms filter by tag.
- `track resolve --term <s>` — resolve a title to a note (existence check).
- `track rename (--id N | --title S | --path P) --to <s>` — rename title and rewrite backlinks.
- `track backlinks (--id N | --path P)` / `track graph (--id N | --path P)` — inbound links / local graph.
- `track export (--id N | --title S | --path P)` — full note Markdown to stdout.

## Output contract

All commands print single-line JSON; errors are `{"error":...}` with exit code 1. Parse stdout as JSON.

## Conventions

- Titles are link keywords; write `[[Title]]` in bodies to link. Title lives in sidecar metadata, not the body, so a body may start with any `#` heading.
- Use `--ai` to stamp the `generated-by-ai` provenance tag on agent-created notes.

## Typical workflow

Create with `new`/`open` (body via `--body` or piped stdin) → link related notes with `[[Title]]` in the body → confirm a target exists with `resolve --term` → rediscover later with `search` (use `#tag` to filter) → change a title and fix every backlink with `rename`. Read a note's full Markdown with `export`.

## Example

```sh
echo "本文 [[関連ノート]]" | track new --title "会議メモ" --ai
```

## For track contributors

When working inside the track source repository (the working directory is the repo root), the canonical, fuller CLI contract lives at `docs/spec/agent-workflows.md`. It is not required to use this skill — the commands above are self-contained.
