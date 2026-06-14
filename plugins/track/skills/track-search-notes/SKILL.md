---
name: track-search-notes
description: Use the track CLI to search, resolve, inspect, and read linked Markdown notes in a track vault. Use when the user asks to find notes by title/body text/tag, look up a note, read/export note contents, check backlinks, or inspect the local note graph without creating or modifying notes.
---

# Track Search Notes

Use the `track` CLI as the source of truth for note search, indexing, link resolution, backlinks, and graph queries. In the track source repo, `go run ./cmd/track` is an acceptable substitute for `track`.

## Preconditions

- Prefer the user's normal track config. `TRACK_VAULT` is for tests and one-off overrides.
- Commands print single-line JSON. Parse stdout as JSON and treat `{"error":...}` with exit code 1 as failure.
- Use this skill for read-only discovery. Use `track-create-note` when the task requires creating or appending notes.

## Search

Search across note titles and indexed fields:

```sh
track search --query "distributed systems" --limit 20
```

Limit the scope:

```sh
track search --scope title --query "roadmap"
track search --scope body --query "TODO"
```

Filter by tags with `#tag` terms. Multiple tags combine with remaining text:

```sh
track search --query "#project"
track search --query "#graph #web Workspace"
```

Results include note IDs, titles, file kind, paths when available, tags, and body snippets/line numbers for body hits. Search misses return an empty `results` array.

## Resolve and Read

Resolve an exact title/link term to a note:

```sh
track resolve --term "Title"
```

Read full Markdown for a known note:

```sh
track export --title "Title"
track export --id 123
track export --path /path/to/note.md
```

Prefer `export` before making claims about a note's contents.

## Links and Graph

Inspect inbound links:

```sh
track backlinks --id 123
track backlinks --path /path/to/note.md
```

Inspect the local note graph around a note:

```sh
track graph --id 123
track graph --path /path/to/note.md
```

Backlinks and graph output are JSON and should be summarized for the user unless they ask for raw output.
