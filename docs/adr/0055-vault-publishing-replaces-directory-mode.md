# 0055. A vault can publish what a directory published

Status: Accepted

## Context

`track export-site` has two inputs. **Vault mode** publishes notes from a vault.
**Directory mode** (`--src`, ADR 0049) publishes a plain Markdown directory that
belongs to no vault — this repository's `docs/help` is published that way.

Directory mode was added because a repository's Markdown had nowhere else to be
published from. Since then a vault has grown everything that made the directory
special and more: sidecar metadata instead of a hand-written `site.yml` page map,
stable ids instead of positions in a listing (adding a file shifts every later
page's URL in directory mode), the vault config, tasks, typed properties. What
remains is the cost: `bundle.go` carries directory special-cases (`mtime = 0`,
`saved = nil`, calendar rejected), so every new export feature is implemented
twice or skipped in one mode.

Two things blocked simply deleting it.

**Published URLs.** A page's address is derived from its id
(`PublishID`), and importing a directory into a vault gives every page a new id —
so every existing link into the published help site would break.

**What a vault accumulates.** A vault checked into a repository grows two things
nobody asked for: indexing creates the day's journal as a side effect of touching
any note, making the journal tree a record of which days its author worked, and
`.track/gen/` keeps a full copy of every past state, so a history the author meant
to rewrite would be committed with the notes.

## Decision

Make a vault able to replace a published directory, then remove directory mode
separately.

- **A note may pin its published address.** A sidecar `slug:` freezes the URL a
  note is already reachable at; empty derives it from the id as before. One
  resolver decides a doc's slug and every surface that addresses a note — the
  bundle, the page writer, the link-resolution map, the OGP head — goes through
  it, so a pinned note is addressed consistently or not at all.
- **A vault may keep no journals and no generation snapshots.** `journal: false`
  and `gen: false` in the vault config. Indexing then creates no day hub and
  carries on; `track journal` says the vault keeps none; every `track gen`
  subcommand refuses.
- **Directory mode is deprecated, not yet removed.** `--src` prints a warning and
  the spec names its replacement. Removal waits until `docs/help` has moved and
  the shared-bundle tests that only run through directory mode — hierarchy pages,
  tag pages, query blocks — have been ported to vault mode. (Both happened in
  ADR 0059; `--src` is gone.)

## Why the published slug keeps its current derivation

The multi-vault design contemplated prefixing the slug input with the vault name
so two vaults' slugs differ. That is not done here. The publishing unit is one
vault to one site, so two vaults' pages never share an address space, and
prefixing would move every already-published URL to buy nothing. Pinning covers
the case that actually needs an address to survive.

## Consequences

- A vault can now be the source of a published site that a directory used to
  serve, with its URLs intact.
- Sidecar metadata gains a version (v8) for `slug`. Older builds reject a sidecar
  that carries one, which is the established behaviour for a newer version.
- The switches are named `JournalOff`/`GenOff` in Go so a zero-value `Config`
  keeps the default behaviour every existing caller expects, while the config
  file reads positively (`journal: false`).
- `gen: false` refuses reads (`list`, `status`, `peek`) as well as writes: the
  vault keeps no snapshots at all, so there is nothing for them to report.
- Directory mode still works unchanged; only its removal is scheduled.
