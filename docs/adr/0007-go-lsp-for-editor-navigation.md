# ADR 0007: Use a Go LSP Server for Editor Navigation

## Status

Accepted

## Context

Calling editor commands for every note link jump is too much friction.
The Neovim Lua plugin can shell out to CLI commands, but link navigation, document links, hover, completion, and future rename support benefit from a long-lived process that understands open buffers.

The Go engine already owns parsing, indexing, matching, and link resolution.
Duplicating that behavior in Lua would make editor behavior drift from the indexed graph.

## Decision

Implement `track-lsp` in Go and keep Lua as a thin startup and rendering layer.

The LSP surface is:

- `textDocument/documentLink` returns ranges for resolved `[[...]]` links (see ADR 0008).
- `textDocument/definition` jumps from a link to the target note.
- `textDocument/completion` offers titles and aliases inside an open `[[`, triggered on `[`.
- `textDocument/didOpen` and `textDocument/didChange` keep unsaved buffer text available for link detection.
- `textDocument/didSave` reindexes the saved note's outgoing links.

The server uses the same `$TRACK_VAULT` and SQLite index as the CLI, resolving links through the shared keyword dictionary.
The Neovim plugin starts `track-lsp` by default for markdown buffers under the vault, renders resolved document links as underlined ranges, and highlights unresolved `[[...]]` distinctly.

## Consequences

Editor navigation becomes interactive without requiring explicit track commands for every jump.

The CLI remains useful for scripts and commands, while LSP owns low-latency editor features.

Future features such as hover, diagnostics, and rename can be added to the Go LSP without reimplementing core note logic in Lua.
