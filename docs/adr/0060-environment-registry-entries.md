# 0060. A checkout can carry a vault, through the registry

Status: Accepted

## Context

ADR 0059 made this repository's help site a vault. That closed the publishing
question and opened an editing one: `docs/help` is now a vault nobody can reach.
`make site` addresses it with `TRACK_VAULT=docs/help` per command, which covers
the build and nothing else — not the LSP, not `track search`, not the workspace.

Nothing in track resolves a vault from where you are standing. The resolution
order is `LoadAt` → `TRACK_VAULT` → `default_vault`/`vault_dir` → `$HOME/track`,
and the Neovim plugin's `vault_of` matches a buffer only against vaults that are
already configured. So a fresh clone has a vault in it that no tool will touch.

The registry would solve it — a named vault gets an LSP client, a workspace tab,
`--vault`, and `[[name:title]]` — but a repository cannot put itself there.
ADR 0051 keeps `vaults:` in the machine config precisely so a synced or cloned
vault cannot introduce vault paths, and that boundary is worth more than the
convenience.

`TRACK_VAULT` does not help either: it *replaces* the active vault, so while it
is set the user's own notes are out of reach. What is missing is a name, not a
selection.

## Decision

`TRACK_VAULTS_<NAME>=<path>` registers one vault for the process.

This is not a new mechanism. track already overrides configuration from the
environment by one rule — `TRACK_` plus the key, upper-snake — followed by
`TRACK_CACHE_DIR`/`cache_dir`, `TRACK_CAPTURE_INBOX`/`capture_inbox`,
`TRACK_ARCHIVE_NOTE`, `TRACK_GEN_KEEP`, `TRACK_DEFAULT_TEMPLATE`,
`TRACK_JOURNAL_TEMPLATE`. The rule simply had no entry point for `vaults:`. It is
now written down (ADR 0059's sibling change spelled `TRACK_DB` as
`TRACK_DB_PATH`, the one variable that did not follow it).

- **The name is derived, not chosen.** `vaults:` is plural in the config, so the
  variable is `TRACK_VAULTS_`. The suffix lowercases and maps `_` to the dash a
  vault name uses (`TRACK_VAULTS_TRACK_HELP` → `track-help`), which round-trips
  because an environment name cannot hold a dash and a vault name cannot hold an
  underscore.
- **Each variable sets the one thing it names.** A `TRACK_VAULTS_` entry adds a
  vault, or replaces the same-named one, and never displaces the rest of the
  registry — the same way `TRACK_WEB_THEME` would set `web.theme` and not all of
  `web`. There is no merge-versus-override special case to remember.
- **Everything else is validated identically.** Absolute path, name pattern, and
  one name per vault: a second name for a directory the config already registered
  is the same ambiguity whichever side it arrives from, so it is the same hard
  error. The fixed-`db_path` refusal now tests the *resolved* registry, so an
  environment-only registry is covered too.
- **Registering is not selecting.** The active vault is untouched. Commands still
  read and write the user's own vault while the checkout's is reachable by name.

## Why the environment and not discovery

Walking up from `$PWD` for a `.track/` — the git/direnv shape — would need no
setup at all, and is a reasonable follow-up. It is not this change, for two
reasons.

The consent question is already answered here. The vault does not name itself;
the shell entering the checkout names it, and the user's `.envrc`, devshell hook,
or Makefile is where that consent already lives — direnv's per-file allow-list
being the clearest example.

And discovery does not actually reach the motivating case. `docs/help` is a
*descendant* of the repository root, not an ancestor of it, so standing at the
root finds nothing; only a downward scan would, and that stops being cheap or
predictable. The variable covers a vault anywhere, including one the search would
miss.

## Consequences

- A repository can ship a vault and have every contributor get it from the
  devshell or `.envrc` — no per-machine registration, no absolute paths in
  anyone's config.
- The derived index cache needs no plumbing: it is already keyed by a hash of the
  vault path, so a registered vault gets its own `index.db` automatically. The
  `TRACK_CACHE_DIR=.site-cache` in this repository's Makefile exists only to keep
  the help vault's index out of the developer's cache directory, and is optional.
- A vault reached this way can be the target of a cross-vault reference, which
  means a note in it can be linked from outside it by a name that only exists
  inside one shell. That is the same situation as any registry entry that is not
  on another machine, and resolves the same way: an unresolved qualified link.
- The registry is now assembled from two sources, so `Vaults()` and `Load()` both
  go through one `vaultEntries` overlay rather than reading `mc.Vaults` directly.
