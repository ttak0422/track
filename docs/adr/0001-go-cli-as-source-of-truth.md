# ADR 0001: Keep the Go CLI as the Source of Truth

## Status

Accepted

## Context

track needs a command-line interface, a Neovim frontend, and likely future
integrations such as an LSP server. Note parsing, metadata handling, indexing,
search, and link resolution must stay consistent across those entry points.

Duplicating persistent behavior in Lua would make the editor experience drift
from the CLI and would make future integrations harder to reuse.

## Decision

The Go engine is the source of truth.

- Core behavior lives in reusable packages under `internal/track/*`.
- `internal/cli` is a thin command router that calls the engine and prints JSON.
- The Neovim plugin shells out to the CLI for persistent operations.
- Lua may mirror lightweight interactive behavior, such as visible-range
  auto-link highlighting, but durable state and indexing belong to Go.

## Consequences

The CLI is required for normal Neovim plugin operation unless a future frontend
links the engine directly.

The upside is that behavior stays centralized and testable. Future integrations
can reuse the engine without depending on editor-specific code.
