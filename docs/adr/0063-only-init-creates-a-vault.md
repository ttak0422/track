# 0063. Only `track init` creates a vault

Status: Accepted

Supersedes the implicit-creation half of [0015](0015-default-vault-to-home-track.md) and the
auto-creation rule in [0051](0051-named-vault-registry.md). The default vault itself — `$HOME/track`
when nothing is configured — is unchanged.

## Context

ADR 0015 let the first write under an unconfigured setup lay down `$HOME/track`, so track needed no
setup step. ADR 0051 then had to carve an exception: a `--vault NAME` selection whose directory is
missing is refused, because that path may be an unmounted drive and a skeleton laid there would be
buried under the real vault when it mounts.

The exception was written as a guard on the *flag*, not on the *directory*. `requireVaultDir` returned
immediately unless `--vault` had been typed, so the same registered vault reached through
`default_vault`, or any path handed to `TRACK_VAULT`, walked straight into the create branch. Verified
against the built binary: `--vault work` was refused while the identical missing directory reached as
the default vault was scaffolded and written into, and `TRACK_VAULT=<typo> track new` silently created
a fresh vault at the typo and put the note in it, where no later search would find it.

That is worse than a stale rule, because ADR 0061 and the agent contract had just promoted
`TRACK_VAULT=<path>` to the way an unregistered vault is addressed. The unguarded door was the one
agents were being pointed at.

Deleting the create branch does not close it on its own. Every writer makes its own parents:
`createTitledNote`, the sidecar writer, and the journal all `MkdirAll` before writing, so a command
against a nonexistent vault root would still produce `<vault>/note/` and a note inside it.

## Decision

**Only `track init` creates a vault. Every other command refuses a directory that is not one.**

- The implicit skeleton in `open()` is gone, and `requireVaultDir` runs regardless of how the vault was
  selected — `--vault`, `TRACK_VAULT`, `--path` derivation, or the configured default.
- The guard asks whether the directory *is a vault*, not whether it *exists*. A path that exists but
  belongs to something else is the same wrong answer one step later, and the writers would scatter
  `note/` and `.track/` through it.
- Two directories pass. One already carrying part of the vault layout — a `.track/` (ADR 0061's
  marker, the rule `--path` derivation and the Neovim plugin already resolve a vault by) or any of the
  kind directories — is a vault, however it got that way: `track init`, a restored sync, a hand-built
  fixture. An **empty** directory is what a caller means by handing over a freshly made path, which is
  the shape CI's `TRACK_VAULT="$(mktemp -d)"` steps and agent scripts use; requiring `track init` there
  would put a setup step on the very flow ADR 0061 promotes. A directory holding nothing but a
  `.DS_Store` counts as empty — that file is Finder's, not its owner's.
- `track init` is the one exemption. It takes `--vault NAME` like everything else, and creates parents,
  so it works on a path that does not exist yet.
- Kind directories keep arriving lazily inside a vault that exists. Nothing scaffolds the whole layout
  behind a command; `journal/` appearing after a note is written is that note's day hub, not a skeleton.

## Consequences

- First run costs one `track init`. That is the whole price, once.
- A typo, an unmounted drive, and someone else's directory now all fail the same way, naming the path
  and the command that would make it a vault.
- The Neovim plugin gains `:Track init`, because the CLI now has a mandatory setup step and the editor
  had no way to perform it. `:checkhealth track` names the command when the configured vault is
  missing.
- Tests and E2E scripts that relied on the first command scaffolding a vault now create the directory
  themselves. That is one line each, and it makes them state what they depend on.
- This does not close the wrong-vault case where the path *is* a real vault — an ambient `TRACK_VAULT`
  pointing somewhere valid but unintended still writes there. That is a visibility problem, not a
  creation one, and `track vault current` reports the selection's `source` for it.
