# ADR 0004: Require Explicit Vault Configuration

## Status

Superseded by config-file based explicit vault configuration.

## Context

track previously fell back to an implicit user data directory when `TRACK_VAULT` was not set.
That behavior is convenient, but it can silently create or read a vault the user did not intend to use.

The cost of a missing configuration error is lower than the cost of writing notes and metadata into an unexpected location.

## Decision

Require explicit vault configuration, but use the user config file as the normal CLI path.

- The Go CLI reads the platform user config file by default (`~/.config/track/config.yml` on XDG-style systems, `~/Library/Application Support/track/config.yml` on macOS).
- The config file must set `vault_dir`.
- `TRACK_VAULT` remains available as an override for tests and one-off runs.
- The Neovim plugin reads the same config file by default, and `require("track").setup({ vault_dir = ... })` can override it for editor-local configuration.
- When `vault_dir` is resolved in Neovim, the plugin exports it as `TRACK_VAULT` for child CLI/LSP processes so they use the same vault.

## Consequences

First-time setup has one required config step, but failures are clear and early.

No command should create a fallback vault under XDG or `~/.local/share`.
