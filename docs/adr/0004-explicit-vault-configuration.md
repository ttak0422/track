# ADR 0004: Require Explicit Vault Configuration

## Status

Superseded by config-file based explicit vault configuration.

## Context

track previously fell back to an implicit user data directory when `TRACK_VAULT` was not set.
That behavior is convenient, but it can silently create or read a vault the user did not intend to use.

The cost of a missing configuration error is lower than the cost of writing notes and metadata into an unexpected location.

## Decision

Require explicit vault configuration, but use the user config file as the normal CLI path.

- The Go CLI reads `~/.config/track/config.yml` by default.
- The config file must set `vault_dir`.
- `TRACK_VAULT` remains available as an override for tests and one-off runs.
- The Neovim plugin requires either `TRACK_VAULT` or `require("track").setup({ vault_dir = ... })`.
- When `vault_dir` is set in Neovim, the plugin exports it as `TRACK_VAULT` so child CLI commands use the same vault.

## Consequences

First-time setup has one required config step, but failures are clear and early.

No command should create a fallback vault under XDG or `~/.local/share`.
