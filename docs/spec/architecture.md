# Architecture

track is a journal and Zettelkasten note tool built around a Go engine and a thin Neovim frontend.

## Source of Truth

The Go CLI is the source of truth for note parsing, metadata loading, indexing, search, and link resolution.
The Neovim plugin shells out to the CLI instead of implementing persistent state management itself.

Reusable engine code lives under `internal/track/*`:

- `config`: vault, database, note, and metadata paths.
- `note`: note file parsing and versioned sidecar metadata.
- `store`: SQLite schema and queries.
- `index`: filesystem scan, metadata ingestion, and link graph rebuilds.
- `match`: auto-link keyword matching.

The CLI layer under `internal/cli` handles argument parsing, command routing, and JSON output.
`cmd/track/main.go` is only the process entry point.

## Data Flow

1. A command creates or updates a markdown note in the vault.
2. Per-note metadata is written under `.track/notes/`.
3. The indexer parses note bodies and metadata.
4. The SQLite index under `.track/index.db` is updated.
5. Neovim commands fetch keywords, resolve terms, and open paths through the CLI.

## Neovim Frontend

The Lua plugin is intentionally thin:

- It resolves the `track` binary.
- It requires an explicit vault through `TRACK_VAULT` or `setup({ vault_dir = ... })`.
- It registers user commands such as `:TrackNew`, `:TrackFollow`, and `:TrackJournal`.
- It fetches the keyword dictionary from `track keywords`.
- It highlights auto-link matches in visible markdown buffers under the vault.
- It follows links by opening the path returned by the CLI or by the highlighter cache.

Persistent behavior should stay in the Go engine unless there is a clear reason to duplicate it in Lua.

## Packaging

Nix builds two artifacts:

- `track-cli`: the Go CLI.
- `track`: the Neovim plugin.

The Nix-built plugin patches the Lua client with the store path of the matching CLI binary, so Nix users do not need to put `track` on `$PATH`.
