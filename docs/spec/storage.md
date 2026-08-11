# Storage Specification

This document describes the current on-disk data model.

## Vault

The vault must be configured explicitly. The normal CLI path is the platform user config file:

```yaml
vault_dir: ~/track
```

The default location is `~/.config/track/config.yml` on XDG-style systems, `~/Library/Application Support/track/config.yml` on macOS, or the platform user config equivalent. `TRACK_CONFIG` may point at another config file for tests and one-off runs. `TRACK_VAULT` overrides `vault_dir` only for tests and one-off commands.

With a `vaults:` registry, the active vault is named rather than pathed: `default_vault: <name>` picks one of the registered vaults, and `vault_dir` is refused — the path is written once, under `vaults:`. Without a registry there are no names, so `vault_dir` gives the path directly and must be absolute (or start with `~/`). When neither sets a vault, track defaults to `$HOME/track` (ADR 0015). Precedence is `TRACK_VAULT` > `default_vault`/`vault_dir` > `$HOME/track`. The fixed, conventional default is low-risk; tests must still set `TRACK_VAULT` (or `HOME`) to a temp path so they never write to a real `$HOME/track`.

Only `track init` creates the vault skeleton. The normal vault-opening path, including `track web`, refuses a missing directory and refuses a populated directory that is not a vault (ADR 0063), so a typo or an unmounted path is not scaffolded implicitly. `track init` creates `note/`, `journal/`, `assets/`, `template/`, `data/`, and `.track/notes/`; it is idempotent. Other kind directories are created lazily inside an existing vault as notes are written.

Notes are markdown files under managed vault directories and are named by note id:

```text
<vault>/note/<id>.md
```

The first supported extension is `.md`; newly created notes use that extension.

Regular note ids are `Unix seconds * 1000 + same-second sequence`, preserving chronological sort order while allowing multiple notes per second. Journal notes use `yyyyMMdd` ids:

```text
<vault>/journal/yyyyMMdd.md
```

A day with note activity always has a journal, unless the vault turns journals off (`journal: false`, see below): indexing a note (creating or editing it through the CLI, the editor LSP, or the web workspace) ensures that day's journal exists, so the journal is the day's aggregation hub. The auto-created journal uses the configured journal template (`journal_template`, builtin `journal` when unset). Journals roll up into `journal/<yyyyMM>.md` and `journal/<yyyy>.md` summaries, and are excluded from activity tracking (see `note_days` below).

Template files live under `template/` and use a template-specific extension:

```text
<vault>/template/<id>.template.md
```

A file path is derived from its kind and id, so paths are not stored in the SQLite cache. The current file kinds are `note`, `journal`, and `template`; the note index currently scans `note/` and `journal/` only.

Media for every note kind lives in a single top-level `assets/` directory, and a note references it with the relative path `assets/<file>`:

```text
<vault>/assets/<file>
```

Assets are part of the authoritative vault (back them up like note bodies), but they are not notes: the note scanner walks only `note/` and `journal/`, so the sibling `assets/` directory is never indexed or flagged by `track doctor`. `track asset import`/`track asset dir` and the `internal/track/asset` engine package manage this storage; see ADR 0016.

Templates are not notes and must not appear in note search or link resolution. When template expansion gains executable substitutions, track will validate the template content and require a first-use trust step keyed by the template content hash, similar to `mise trust`.

Template files begin with a template directive and then contain the markdown body to render:

```markdown
<!-- track-template
name: daily
-->
# {{ title }}
```

The directive names the template for `track template open --name <name>` and `track new/open/journal --template <name>`.
It is stripped from generated notes. Current substitutions are safe built-ins only: `{{ title }}`, `{{ id }}`, `{{ date }}`, and `{{ kind }}`.
See [templates.md](templates.md) for the current template behavior.

## Track Directory

Authoritative track-owned vault data lives under:

```text
<vault>/.track/
```

Current contents:

```text
<vault>/.track/config.yml
<vault>/.track/notes/<id>.yaml
<vault>/.track/gen/
<vault>/.track/trash/
```

`.track/config.yml` is the vault config: the note semantics that travel with the vault (`properties`, `queries`, `icons`, date formats, default templates, `capture_inbox`, `archive_note`, `web.home`, `gen_keep`, `extensions`). It is optional — a vault without one uses the defaults. Task states are fixed in the engine (`task.States`: `TODO`, `DOING`, `WAITING`, `DONE`, and `CANCELLED`) rather than configured per vault.

Two switches turn off whole features for a vault, both on by default:

```yaml
journal: false   # this vault keeps no daily journals
gen: false       # this vault keeps no generation snapshots
```

A vault checked into a repository or published wants both off. Journals are created automatically by indexing, so the journal tree is a record of which days its author worked; `.track/gen/` keeps a full copy of every past state, so a history the author meant to rewrite would be committed alongside the notes. With `journal: false` indexing simply creates no day hub, and `track journal` says so; with `gen: false` every `track gen` subcommand refuses.

`.track/notes/` contains versioned sidecar metadata files for notes. `.track/gen/` holds generation snapshots (ADR 0025). `.track/trash/` holds what `track rm` soft-deletes: only explicit commands move files into it (ADR 0051) — index reconciliation leaves the sidecar of a vanished note in place for `track doctor` to report as an orphan.

The rebuildable SQLite index is a cache outside the vault. By default it lives under the platform user cache directory:

```text
<user-cache>/track/<vault-key>/index.db
```

`TRACK_CACHE_DIR` overrides the `track` cache directory for tests and one-off runs. The CLI is the only resolver of the cache location: frontends (the Neovim plugin, the web workspace) never compute or export their own cache directory, so every process that opens a vault lands on the same physical `index.db`.

Every configuration key can be overridden from the environment by one rule: `TRACK_` plus the key in upper snake case. `TRACK_CACHE_DIR` sets `cache_dir`, `TRACK_GEN_KEEP` sets `gen_keep`, `TRACK_CAPTURE_INBOX` sets `capture_inbox`, and `TRACK_VAULTS_<NAME>` sets one entry of the `vaults:` registry. Each variable sets exactly the thing it names — a `TRACK_VAULTS_` entry adds one vault, or replaces the same-named one, and never displaces the rest of the registry. The rule spans both config files, which works because their key namespaces are disjoint by construction: a key in the wrong file is a hard error (ADR 0050).

An environment vault name comes from the suffix, lowercased with `_` mapped to the dash a vault name uses — `TRACK_VAULTS_TRACK_HELP` registers `track-help`. The mapping round-trips because an environment variable name cannot hold a dash and a vault name cannot hold an underscore. Everything else about the entry is validated exactly as a configured one: the path must be absolute, and a vault still gets exactly one name.

This is how a checkout carries a vault. The registry is machine state and a synced or cloned vault must never introduce vault paths (ADR 0051), so a repository cannot register itself — but the shell entering that checkout can, on the user's behalf: a devshell hook, a Makefile, or a direnv `.envrc` the user allowed. Registering is not selecting: the active vault is untouched, so commands still read and write the user's own vault while the checkout's is reachable by name (`--vault`, `[[name:title]]`, cross-vault search, and a per-vault LSP client).

Two variables sit outside the rule because neither names a key: `TRACK_CONFIG` is the config file itself, and `TRACK_VAULT` selects the active vault by path.

There is no key for the database path itself. The index is derived from the vault path (`<cache-dir>/<vault-key>/index.db`), so relocating it means moving the whole cache with `cache_dir`/`TRACK_CACHE_DIR` — a single named file could not serve the vaults a registry holds.

Cross-vault references (`[[vault:title]]`, ADR 0053) live in each vault's own index as `ext_links` rows keyed by `(vault name, title)` — the target's numeric id is never stored, because ids are vault-local. Inbound cross-vault backlinks are answered by scanning the other registered vaults' databases for rows naming this vault.

Configuration ownership is split (ADR 0050): the machine config file can also set `cache_dir` and the local web workspace's `web.theme`/`web.colors_path` (see [web.md](web.md)); everything about note semantics — `extensions`, `date_format`, `journal_date_format`, and the rest — lives in the vault config `<vault>/.track/config.yml`. Both files reject keys that belong to the other, so a synced vault can never redirect where this machine reads and writes. Environment values override the matching file values, but normal configuration should live in the files.

The vault path is canonicalized (symlinks resolved, made absolute) before use. A symlinked vault — for example `~/track` pointing at a cloud-synced `~/OneDrive/track` — therefore resolves to one stable path, so the `<vault-key>` cache key stays the same no matter which path the CLI is invoked through.

## Note Metadata

Metadata is separate from the markdown note body. Paths are derived from file kind and id rather than stored in the SQLite cache. A regular note, a journal, and a template use these forms:

```text
<vault>/note/1000.md
<vault>/journal/20260811.md
<vault>/template/1000.template.md
```

The sidecar for the regular note is:

```text
<vault>/.track/notes/1000.yaml
```

Metadata example:

```yaml
version: 3
title: リンク
tags:
  - zettel
created: 2026-05-24
days:
  - 2026-05-24
  - 2026-06-22
```

Fields:

- `version`: metadata schema version. Required for new writes. The version is the newest schema any present field needs: a sidecar carrying Babel block results is at least v2, and one carrying `days` is at least v3.
- `title`: note title and the link keyword. This sidecar field is authoritative.
- `tags`: note tags.
- `created`: creation date string. The current format is `YYYY-MM-DD`.
- `slug`: pins this note's published address. The static export normally derives a slug from the note id, so a note that already has a public URL under a different id — one imported from a published directory — would move; setting `slug` freezes the address it is already reachable at. Empty (the usual case) derives it as before.
- `days`: sorted, deduplicated set of local calendar days the note was created or updated on (`YYYY-MM-DD`). A day is stamped whenever the note is touched: a track mutation command stamps it via single-note reindex, and a direct editor/external edit is stamped during the mtime-divergence scan in `RefreshIfStale`. This is the authoritative activity record used by `track agenda` to answer "which notes were worked on that day". Sidecars predating the field have no `days`; the index then falls back to `created` so the note still appears on the day it was made.

Readers reject unsupported metadata versions.
If a sidecar is missing, the current parser can still read the legacy trailing `<!--track ... -->` metadata block for compatibility, but new writes must use sidecar metadata.

The markdown body is plain content. It may be empty or contain any headings, including a leading H1. Parsing and reindexing never derive or reconcile the title from the body; title changes must go through create/open/journal/append metadata writes, `track rename`, or LSP rename.

A rename rewrites every `[[old title]]` in the vault as part of the operation, so no record of past titles is kept. A link the rewrite could not reach — one in another vault, or one written by hand afterwards — surfaces as an ordinary unresolved-link diagnostic; the old title is not stored anywhere, so it is also free for a new note immediately.

## SQLite Index

The SQLite index is derived state.
It can be rebuilt from markdown note files and sidecar metadata.
The indexer scans the top-level `note/` and `journal/` directories only, matching the file-kind rules above.
SQLite `PRAGMA user_version` stores the database schema version and is independent from sidecar metadata versions.

Schema version 8 contains a central `notes` table with each indexed id, file kind, cached sidecar title and creation date, note-file `mtime`, sidecar `meta_mtime`, and icon override. The two mtimes let `RefreshIfStale` detect both body changes and sidecar-only changes. `tags` stores the note's tags, `links` stores computed directed links between notes, and `ext_links` stores outgoing cross-vault references as `(source id, vault name, title)` without a target numeric id because ids are vault-local.

`note_days` stores the local activity days for each non-journal note, mirrored from sidecar `days` and falling back to `created` for older sidecars. `tasks` stores one row per parsed checkbox line, including its fixed state, terminal flag, priority, scheduling, due, completion, and text fields. `props` stores flattened typed properties from sidecars and inline `key::` body fields; list values retain their order.

The `keywords` view exposes non-empty note titles for keyword resolution and link highlighting. `notes_fts` is an FTS5 trigram virtual table whose rowid is the note id and whose body is the parsed note text, providing case-insensitive substring body search with bm25 ranking. The removed semantic-related-notes feature has no table in the current schema (ADR 0056).

The exact columns, indexes, foreign keys, and virtual-table definition live in [`internal/track/store/schema.go`](../../internal/track/store/schema.go). Keeping the specification at the model level avoids duplicating that DDL here.

The index uses WAL mode and foreign keys, and sets no busy timeout: a database locked by another process fails fast instead of queueing. Note paths are never cached — they are derived from file kind plus note id — but note bodies are cached in `notes_fts` for body search; terms too short to form a trigram fall back to a per-file scan.

Because the index is a rebuildable cache, a schema bump needs no migration: when `Open` finds an older `user_version`, it drops the existing tables and views and re-applies the schema in place. The emptied store is repopulated by the next `RefreshIfStale` → full reindex, which reparses every note and sidecar.

## Deletion

During a full reindex, notes missing from the filesystem are removed from the SQLite index only. Their sidecar metadata remains at `.track/notes/<id>.yaml`; with no matching markdown file, `track doctor` reports it as an `orphan_sidecar`. Reconciliation does not move the sidecar or write a file in `.track/trash/`; file moves are reserved for explicit commands such as `track rm` and `doctor --fix`.

When the scan finds no note files at all while the index still lists several notes, the reindex refuses to reconcile: an unmounted or unreadable vault must fail loudly rather than silently empty the index. A small floor keeps legitimate cases working — emptying a tiny vault (down to `track rm` of its last file) still reconciles — so the refusal fires only when a populated index would be wiped wholesale. If the notes really are gone, `track reindex` resets the database and rebuilds from what is on disk.

Run `track doctor` when a vault may be only partially synced: it reports orphan sidecars (and other divergence) read-only, so a sync gap is not silently treated as a deletion. See [agent-workflows.md](agent-workflows.md) and ADR 0014.

## Durability: do not delete `.track/notes/`

The vault and cache hold two very different kinds of data:

- `.track/notes/<id>.yaml` are the **authoritative** per-note metadata sidecars. The markdown body is content only; `title`, `tags`, `created`, `days`, and Babel block results live in the sidecar and cannot be reconstructed from the `.md` file.
- The SQLite index under the cache directory is **rebuildable**. The notes on disk are the source of truth; `track reindex --full` deletes the cache database and regenerates it from them. Deleting it is safe.

Deleting `.track/notes/` is therefore irrecoverable data loss. Treat it like `.git`: keep it under version control and back it up alongside the note bodies.

track does **not** reconstruct lost metadata from the note body, because rebuilding a sidecar from the `.md` alone would silently drop tags and block results while appearing to succeed. The `track doctor --fix` repair is deliberately limited to restoring *structure and identity*, never inventing content: a missing sidecar is recreated with a placeholder `Untitled N` title, an orphan sidecar's markdown is recreated empty, a duplicate title is renumbered, and a stray conflict copy is imported as a new note. It never recovers the original title, tags, or block results — a backup of `.track/notes/` is still the only way to get those back. See ADR 0014 for the health-check and repair model.
