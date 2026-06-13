# Architecture

track is a journal and Zettelkasten note tool built around a Go engine and a thin Neovim frontend.

## Source of Truth

The Go engine is the source of truth for note parsing, metadata loading, indexing, search, and link resolution.
The CLI and LSP server are frontends over the same engine packages.
The Neovim plugin delegates persistent behavior to those Go frontends instead of implementing state management itself.

Reusable engine code lives under `internal/track/*`:

- `config`: vault, database, note, and metadata paths.
- `note`: note file parsing and versioned sidecar metadata.
- `store`: SQLite schema and queries.
- `index`: filesystem scan, metadata ingestion, and link graph rebuilds.
- `link`: extraction of `[[...]]` references from note text.

The CLI layer under `internal/cli` handles argument parsing, command routing, and JSON output.
`cmd/track/main.go` is only the process entry point.
The LSP layer under `internal/track/lsp` handles JSON-RPC, document link requests, definition requests, completion, and open document text.
`cmd/track-lsp/main.go` is only the LSP process entry point.
The local web workspace under `internal/track/webui` serves HTTP APIs and a browser UI over the same SQLite index. It is for interactive local exploration, not publication; public output belongs to export/static-site tooling.

## Data Flow

1. A command creates or updates a markdown note in the vault.
2. Per-note metadata is written under `.track/notes/`.
3. The indexer parses note bodies and metadata.
4. The SQLite cache index under the user cache directory is updated.
5. Neovim commands fetch keywords, resolve terms, and open paths through the CLI or LSP server.

## Neovim Frontend

The Lua plugin is intentionally thin:

- It resolves the `track` and `track-lsp` binaries.
- It requires an explicit vault through the user config file, `TRACK_VAULT`, or `setup({ vault_dir = ... })`.
- It registers the `:Track` dispatcher command with subcommands such as `:Track open`, `:Track follow`, and `:Track journal`.
- It can start the local web workspace through `:Track web`, delegating the server to the Go CLI.
- It starts `track-lsp` for markdown buffers under the vault.
- It renders resolved `textDocument/documentLink` results as underlined ranges and highlights unresolved `[[...]]` distinctly.
- It follows links through `textDocument/definition` and completes titles inside `[[` through `textDocument/completion`.

Persistent behavior should stay in the Go engine unless there is a clear reason to duplicate it in Lua.

## Packaging

Nix builds two artifacts:

- `track-cli`: the Go CLI.
- `track-lsp`: the Go LSP server, installed by the same Go package as `track-cli`.
- `track`: the Neovim plugin.

The Nix-built plugin patches the Lua client with the store paths of the matching CLI and LSP binaries, so Nix users do not need to put them on `$PATH`.
