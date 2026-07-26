# 0051. A named vault registry selects vaults; read commands never move files

Status: Accepted

## Context

track has always been one process, one vault: `TRACK_VAULT` > `vault_dir` > `$HOME/track`
(ADR 0004/0015). Notes with different publication levels want physically separate vaults — public
boundaries should be storage boundaries, not per-note flags — which means several vaults per
machine and a way to address them that is safer than pasting paths.

Two hazards shaped the design. First, ADR 0004's lesson: a mistyped vault path silently creates a
fresh empty vault where the typo points. Names make this worse if unknown names auto-create.
Second, the index cache: since the cache split was healed (Phase 0), each vault derives its own
`index.db` from its canonical path — but a fixed `db_path`/`TRACK_DB` would pin *every* selected
vault to the *same* database file, so two vaults would silently overwrite each other's index.

Separately, the index's deletion reconciliation used to move the sidecar of a vanished note into
`.track/trash` — and reconciliation runs as a *read-path* side effect (`RefreshIfStale` → `Full`
before search/query/tasks). A read command that relocates files turns every staleness check into a
potential mutation, which multiplies risk as soon as maintenance sweeps several vaults, some of
them unmounted cloud storage.

## Decision

- The machine config gains a `vaults:` registry — a `name: path` map. Names are `[a-z0-9-]+` so a
  name can later prefix a cross-vault link (`vault:title`); paths must be absolute (after `~`
  expansion). The registry is machine scope by definition: which vaults exist on a machine is
  machine state, and a synced vault must never introduce vault paths.
- **A vault has exactly one name.** Two entries resolving to the same directory are refused when the
  config loads, comparing canonically so a symlink or a trailing slash cannot slip a second name
  past. A name is not only how a vault is *addressed* — it is how the vault is *reported*: it labels
  a cross-vault search hit, it qualifies a note's id in the workspace, and it is what a
  `[[name:title]]` link is written with. With two names, every one of those has to pick a winner,
  and nothing makes one name more correct than the other. Refusing at load keeps that question from
  existing. (The launch vault is still reachable both as the default and by its registered name;
  that is one name, addressed two ways.)
- A global `--vault NAME` flag, pre-parsed in the CLI router, resolves the name and exports the
  path as `TRACK_VAULT`, so `config.Load` and every engine package stay selection-agnostic. An
  unknown name is a hard error listing the registered names. `TRACK_VAULT` itself stays a path —
  direnv-style project-local vaults keep working unchanged.
- Auto-creation stays default-vault-only: an ordinary command refuses a `--vault` selection whose
  directory is missing (it may be an unmounted drive; laying a skeleton there would bury the real
  vault when it mounts). `track init --vault NAME` creates it explicitly.
- A fixed `db_path`/`TRACK_DB` is a hard error whenever the registry is non-empty.
- `track vault list|current|which` inspect the registry; `which` never touches the vault, so it
  works while the vault is offline.
- With a registry and no `--vault` selection, `reindex`, `doctor`, and `refresh-all` sweep the
  active vault plus every registered vault, one result row each, aggregate `ok` on top. An
  unreachable vault is an error row, never a skeleton or a cache reset. `doctor --fix` still
  requires an explicit single-vault choice.
- Index reconciliation touches only index rows. The sidecar of a note missing from disk stays put
  as an `orphan_sidecar` for doctor to report; only explicit commands move files (`track rm` →
  trash, `doctor --fix` → restore). This retires the read-path half of the Phase 0 trash-on-
  reconcile behavior; the empty-scan guard stays, since refusing to mass-delete index rows from an
  unreadable vault is still the right failure mode.

## Consequences

- One cron `refresh-all` entry maintains every registered vault and doubles as a health roster:
  offline vaults surface as error rows instead of vanishing from the report.
- Deleting a note file by hand (not via `track rm`) now always leaves an orphan sidecar until
  doctor reports it — the trade for read commands that provably never move files.
- The registry is the foundation the cross-vault phases build on: ATTACH-based federated search,
  `vault:title` references, and per-vault LSP all resolve names through this one map.
