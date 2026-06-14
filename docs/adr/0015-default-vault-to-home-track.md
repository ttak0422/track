# ADR 0015: Default the Vault to $HOME/track

## Status

Accepted

Supersedes the no-implicit-vault decision in ADR 0004. The config-file
resolution from ADR 0004 still stands; only the "fail when unconfigured" part
is reversed.

## Context

ADR 0004 required an explicit vault and made track fail early when neither the
config file nor `TRACK_VAULT` set one, to avoid silently writing to an
unintended location. In practice the project converged on a single conventional
location — `$HOME/track` — and that strict failure became friction: a fresh
checkout, the `nix run .#test-nvim` launcher, and casual CLI use all required a
config file before track would do anything.

A predictable, conventional default is lower-risk than the original ADR assumed,
because the location is fixed and obvious (`$HOME/track`) rather than a hidden
XDG/`~/.local/share` path the user would not think to look in.

## Decision

When neither the config file's `vault_dir` nor `TRACK_VAULT` is set, default the
vault to `$HOME/track`.

- The Go engine (`config.Load`) resolves `vault_dir` from the config file or
  `TRACK_VAULT`, and otherwise uses `$HOME/track`. It only errors if the home
  directory itself cannot be determined.
- The Neovim plugin (`lua/track/config.lua`) applies the same fallback, so
  `require("track").setup({})` works with no config.
- Precedence is unchanged and explicit: `TRACK_VAULT` > config file `vault_dir` >
  `$HOME/track`.
- The path is still canonicalized for the cache key and kept symlink-intact for
  display, as before.

## Consequences

track works out of the box against `$HOME/track` with no configuration, while a
config file or `TRACK_VAULT` still points it elsewhere. The `nix run .#test-nvim`
launcher no longer forces a temp vault; tests and E2E scripts pass an explicit
`TRACK_VAULT` to stay isolated.

The original ADR 0004 guarantee that "no command creates a fallback vault" no
longer holds: the first write under an unconfigured setup creates `$HOME/track`.
This is intended. Automated tests must set `TRACK_VAULT` (or `HOME`) to a temp
path so they never touch a real `$HOME/track`.
