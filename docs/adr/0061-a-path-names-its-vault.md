# 0061. A path names its vault

Status: Accepted

## Context

ADR 0060 let a checkout carry a vault by registering it from the environment.
That gives the vault a name, which is what every surface reporting or accepting
one needs. It does not help the case where nothing was set up at all: a fresh
clone, an agent invoking the CLI, a one-off command against a directory the user
happens to have.

The Neovim plugin has never had this problem, and the reason is worth copying. It
resolves a buffer's vault from the buffer's own path, then starts an LSP client
rooted there with `TRACK_VAULT` scoping the server to it. No registry entry, no
name, no search — the file says which vault it is in, because a note sits
directly under `<vault>/note/` or `<vault>/journal/`.

Several CLI commands name a file the same way, with `--path`. They did not use
it: `KindFromPath` anchors a path against the *active* vault, so `track meta
--path <other vault>/note/200.md` was refused with "path is not a vault note"
however clearly the path identified its vault.

## Decision

A `--path` argument selects the vault it lives in.

The rule is the plugin's, unchanged: the vault root is two directories up, if
that directory holds a `.track/`. The marker is the directory, not its
`config.yml` — a vault's config is optional. Resolution is one `stat`; there is no
search and no walk.

- **It only replaces a hard error.** A path outside the active vault is refused
  today, so nothing that currently works changes meaning.
- **An explicit `--vault` wins.** A flag is a decision; a derived vault is an
  inference.
- **A path in no vault is left alone**, for the command to reject as it did before.
- **Commands that name no file infer nothing.** `search`, `new --title`, `notes`,
  `query` still take `--vault` or `TRACK_VAULT`. This is the important half of the
  rule: those are exactly the commands where a wrong guess writes to the wrong
  vault, and they have no argument that could justify a guess.

Implemented next to `applyVaultFlag`, which already pre-scans argv and exports
`TRACK_VAULT` before any command loads config, so the selection reaches
everything without threading a parameter through.

## Why not discovery from the working directory

Walking up from `$PWD` for a `.track/` is the git shape, and it was considered.
It answers a different question — "where am I" rather than "what did you name" —
and it answers it wrongly for the case that motivated all of this: `docs/help` is
a *descendant* of this repository's root, not an ancestor, so standing at the root
finds nothing. Only a downward scan would, and that stops being cheap or
predictable.

More importantly, `$PWD` would move the vault for *every* command, including the
ones with nothing to derive from. `track new` writing somewhere else because of a
`cd` is a silent wrong-vault write. A `--path` cannot do that: it moves the vault
only for the file it already named.

## Consequences

- `track meta --path <any vault>/note/<id>.md` works from anywhere, and writes the
  sidecar into that vault. A test covers that the active vault is left untouched.
- The CLI and the editor now resolve a vault by the same rule, so a command
  addressing a file and an editor editing it can no longer disagree.
- `track fmt <paths>...` takes positional paths, not `--path`, and is not covered.
  Several paths could name several vaults, so the derivation has no single answer;
  it keeps taking `--vault`/`TRACK_VAULT`.
- The agent contract (`docs/spec/agent-workflows.md`) stops describing
  `TRACK_VAULT` as a test-only override. It is how an unregistered vault is
  addressed, and an agent is the most likely caller.
