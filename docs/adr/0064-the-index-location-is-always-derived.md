# 0064. The index location is always derived; `db_path` is gone

Status: Accepted

Removes the `db_path` key [0050](0050-vault-owned-configuration.md) assigned to machine scope and the
refusal [0051](0051-named-vault-registry.md) had to add around it.

## Context

The SQLite index is a rebuildable cache. Its default location is derived — a hash of the vault's
canonical path under `cache_dir`, so every vault gets its own database without anyone naming one — and
`cache_dir`/`TRACK_CACHE_DIR` moves the whole cache when a machine wants it elsewhere. This
repository's own Makefile uses exactly that to keep the help vault's index out of a developer's cache
directory.

`db_path` sat beside it as a way to name one database outright. With one vault per machine that was
merely redundant. With a registry it became actively wrong: one fixed database cannot serve several
vaults, and two vaults sharing an index overwrite each other's rows. ADR 0051 handled that by refusing
the combination at load, and ADR 0060 had to extend the refusal to the resolved registry once
environment entries could contribute to it.

So the key earned two validation rules and a paragraph of documentation in three places, and paid for
them with a capability nothing used. In the tree it appeared twice in anger against forty-two
defensive `t.Setenv("TRACK_DB_PATH", "")` lines whose only job was to stop an ambient value leaking
into an unrelated test.

## Decision

- `db_path` and `TRACK_DB_PATH` are removed. The index path is always
  `<cache_dir>/<hash of vault path>/index.db`.
- The cross-key refusal goes with them. Two vaults sharing one index is now **unrepresentable** rather
  than rejected at load, which is the stronger form of the same guarantee — there is no key left to
  say it with.
- Relocation stays with `cache_dir`/`TRACK_CACHE_DIR`, which is machine scope for the reason ADR 0050
  gave: it describes this machine, not these notes.
- An existing `db_path:` in a machine config is an unknown key, and the hint names `cache_dir` as the
  replacement rather than leaving the reader with a bare "field not found".

## Consequences

- A machine config carrying `db_path:` fails loudly on upgrade. That is deliberate; a silently ignored
  key would leave the index somewhere the user did not expect.
- Pointing a single command at an arbitrary `.db` file for debugging is no longer possible. Setting
  `TRACK_CACHE_DIR` to a scratch directory covers the same ground for the case that actually occurred.
- Tests stop neutralizing a variable nothing reads.
