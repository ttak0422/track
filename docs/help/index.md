# track

track is a linked Markdown knowledge base. Notes are plain Markdown files; you connect them with
explicit `[[Title]]` wiki links, and a Go engine indexes the vault for search, link resolution, and a
local web workspace.

This help site is itself produced by `track export-site`, so it doubles as a working example of the
static-site export.

## Where to go next

- [[Syntax]] — the Markdown a note is written in: bold, math, tables, and the Obsidian-style constructs.
- [[CLI]] — the command-line interface that owns parsing, indexing, and search.
- [[Searching notes]] — title, tag, and full-text body search, with ranking and CJK support.
- [[Linking notes]] — how `[[...]]` links, backlinks, and the note graph work.
- [[Web workspace]] — the local browser UI for reading, previewing, and navigating notes.
- [[Visualization]] — how notes render as visuals: [[Diagrams]] (full Mermaid support), [[Charts]] from
  a View Spec, and [[Embeds]] for YouTube, PDFs, tweets, and other rich media.
- [[Babel]] — run a note's fenced code blocks and keep their results in the sidecar.

## How the pieces fit

```mermaid
flowchart LR
  user[You] --> nvim[Neovim plugin]
  user --> web[Web workspace]
  nvim --> cli[track CLI]
  web --> cli
  cli --> engine[Go engine]
  engine --> index[(SQLite index)]
  engine --> vault[(Markdown vault)]
```

The Go CLI is the source of truth: the Neovim plugin and the web workspace are thin frontends that
shell out to it. Reusable engine code lives under `internal/track/*` so other integrations can build on
it without depending on the command layer.
