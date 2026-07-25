# Storage Specification

This document describes the current on-disk data model.

## Vault

The vault must be configured explicitly. The normal CLI path is the platform user config file:

```yaml
vault_dir: ~/track
```

The default location is `~/.config/track/config.yml` on XDG-style systems, `~/Library/Application Support/track/config.yml` on macOS, or the platform user config equivalent. `TRACK_CONFIG` may point at another config file for tests and one-off runs. `TRACK_VAULT` overrides `vault_dir` only for tests and one-off commands.

When neither the config file nor `TRACK_VAULT` sets a vault, track defaults to `$HOME/track` (ADR 0015). Precedence is `TRACK_VAULT` > config file `vault_dir` > `$HOME/track`. The fixed, conventional default is low-risk; tests must still set `TRACK_VAULT` (or `HOME`) to a temp path so they never write to a real `$HOME/track`.

On first launch — the first command that touches a vault whose directory does not exist yet (including `track web`) — track lays down the directory skeleton: `note/`, `journal/`, `assets/`, `template/`, and `.track/notes/`. An existing vault is left alone (directories are otherwise created lazily as notes are written), so this never resurrects a directory that was intentionally removed. `track init` creates the skeleton explicitly and is idempotent.

Notes are markdown files under managed vault directories and are named by note id:

```text
<vault>/note/<id>.md
```

The first supported extension is `.md`; newly created notes use that extension.

Regular note ids are `Unix seconds * 1000 + same-second sequence`, preserving chronological sort order while allowing multiple notes per second. Journal notes use `yyyyMMdd` ids:

```text
<vault>/journal/yyyyMMdd.md
```

A day with note activity always has a journal: indexing a note (creating or editing it through the CLI, the editor LSP, or the web workspace) ensures that day's journal exists, so the journal is the day's aggregation hub. The auto-created journal uses the configured journal template (`journal_template`, builtin `journal` when unset). Journals roll up into `journal/<yyyyMM>.md` and `journal/<yyyy>.md` summaries, and are excluded from activity tracking (see `note_days` below).

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
<vault>/.track/renames.yaml
<vault>/.track/gen/
<vault>/.track/trash/
```

`.track/config.yml` is the vault config: the note semantics that travel with the vault (`task_states`, `properties`, `queries`, `icons`, date formats, default templates, `capture_inbox`, `archive_note`, `web.home`, `gen_keep`, `extensions`). It is optional — a vault without one uses the defaults. `.track/notes/` contains versioned sidecar metadata files for notes. `.track/renames.yaml` is title rename history (repair only, not a link source). `.track/gen/` holds generation snapshots (ADR 0025). `.track/trash/` holds what `track rm` soft-deletes: only explicit commands move files into it (ADR 0051) — index reconciliation leaves the sidecar of a vanished note in place for `track doctor` to report as an orphan.

The rebuildable SQLite index is a cache outside the vault. By default it lives under the platform user cache directory:

```text
<user-cache>/track/<vault-key>/index.db
```

`TRACK_CACHE_DIR` overrides the `track` cache directory for tests and one-off runs. The CLI is the only resolver of the cache location: frontends (the Neovim plugin, the web workspace) never compute or export their own cache directory, so every process that opens a vault lands on the same physical `index.db`.

`TRACK_DB` can still point at an explicit database path for debugging or tests. When the machine config registers named vaults (`vaults:`, ADR 0051), `db_path`/`TRACK_DB` is refused: a fixed path would pin every selected vault to the same database file.

Cross-vault references (`[[vault:title]]`, ADR 0053) live in each vault's own index as `ext_links` rows keyed by `(vault name, title)` — the target's numeric id is never stored, because ids are vault-local. Inbound cross-vault backlinks are answered by scanning the other registered vaults' databases for rows naming this vault.

Configuration ownership is split (ADR 0050): the machine config file can also set `cache_dir`, `db_path`, `embedder`, and the local web workspace's `web.theme`/`web.colors_path` (see [web.md](web.md)); everything about note semantics — `extensions`, `date_format`, `journal_date_format`, and the rest — lives in the vault config `<vault>/.track/config.yml`. Both files reject keys that belong to the other, so a synced vault can never configure which commands run on a machine. Environment values override the matching file values, but normal configuration should live in the files.

The vault path is canonicalized (symlinks resolved, made absolute) before use. A symlinked vault — for example `~/track` pointing at a cloud-synced `~/OneDrive/track` — therefore resolves to one stable path, so the `<vault-key>` cache key stays the same no matter which path the CLI is invoked through.

## Note Metadata

Metadata is separate from the markdown note body.
For a note:

```text
<vault>/1000.md
```

the metadata path is:

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
- `days`: sorted, deduplicated set of local calendar days the note was created or updated on (`YYYY-MM-DD`). A day is stamped whenever the note is touched: a track mutation command stamps it via single-note reindex, and a direct editor/external edit is stamped during the mtime-divergence scan in `RefreshIfStale`. This is the authoritative activity record used by `track agenda` to answer "which notes were worked on that day". Sidecars predating the field have no `days`; the index then falls back to `created` so the note still appears on the day it was made.

Readers reject unsupported metadata versions.
If a sidecar is missing, the current parser can still read the legacy trailing `<!--track ... -->` metadata block for compatibility, but new writes must use sidecar metadata.

The markdown body is plain content. It may be empty or contain any headings, including a leading H1. Parsing and reindexing never derive or reconcile the title from the body; title changes must go through create/open/journal/append metadata writes, `track rename`, or LSP rename.

Title changes are also recorded in `.track/renames.yaml` as repair history. Rename history is not a link keyword source: an old title remains available for a new note, and `[[old title]]` does not resolve through the history. LSP code actions may use the history only when an old title is unresolved, offering to rewrite the link to the newest recorded title.

## SQLite Index

The SQLite index is derived state.
It can be rebuilt from markdown note files and sidecar metadata.
The indexer scans the top-level `note/` and `journal/` directories only, matching the file-kind rules above.
SQLite `PRAGMA user_version` stores the database schema version and is independent from sidecar metadata versions.

Schema version 5 contains:

- `notes`: note id, file kind, title, created date, mtime, and icon override.
- `tags`: tags for each note.
- `links`: computed directed links between notes.
- `note_days`: the activity days each note was created or updated on.
- `tasks`: one row per checkbox line with its state, priority, and scheduling fields.
- `props`: flattened typed note properties (sidecar props and inline `key::` body fields).
- `embeddings`: cached per-note vectors for `track similar`.
- `notes_fts`: an FTS5 trigram index over note bodies (ADR 0045).
- `keywords`: a view over note titles.

The index uses WAL mode and foreign keys, and sets no busy timeout: a database locked by another process fails fast instead of queueing. Note paths are never cached — they are derived from file kind plus note id — but note bodies are cached in `notes_fts` for body search; terms too short to form a trigram fall back to a per-file scan.

Because the index is a rebuildable cache, a schema bump needs no migration: when `Open` finds an older `user_version`, it drops the existing tables and views and re-applies the schema in place. The emptied store is repopulated by the next `RefreshIfStale` → full reindex, which reparses every note and sidecar.

### Schema Version 5

```sql
CREATE TABLE notes (
  id      INTEGER PRIMARY KEY,
  kind    TEXT NOT NULL DEFAULT 'note',
  title   TEXT NOT NULL DEFAULT '',
  created TEXT,
  mtime   INTEGER NOT NULL DEFAULT 0,
  icon    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_notes_kind_mtime ON notes(kind, mtime);

CREATE TABLE tags (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag     TEXT NOT NULL,
  PRIMARY KEY (note_id, tag)
);
CREATE INDEX idx_tags_tag ON tags(tag);

CREATE TABLE links (
  src_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  dst_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  PRIMARY KEY (src_id, dst_id)
);
CREATE INDEX idx_links_dst ON links(dst_id);

CREATE TABLE note_days (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  day     TEXT NOT NULL,
  PRIMARY KEY (note_id, day)
);
CREATE INDEX idx_note_days_day ON note_days(day);

CREATE TABLE tasks (
  note_id   INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  line      INTEGER NOT NULL,
  state     TEXT NOT NULL,
  done      INTEGER NOT NULL DEFAULT 0,
  priority  TEXT NOT NULL DEFAULT '',
  scheduled TEXT NOT NULL DEFAULT '',
  due       TEXT NOT NULL DEFAULT '',
  completed TEXT NOT NULL DEFAULT '',
  text      TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (note_id, line)
);
CREATE INDEX idx_tasks_state ON tasks(state);
CREATE INDEX idx_tasks_due ON tasks(due);

CREATE TABLE props (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL,
  type    TEXT NOT NULL,
  line    INTEGER NOT NULL DEFAULT 0,
  ord     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_props_note ON props(note_id);
CREATE INDEX idx_props_key ON props(key, value);

CREATE VIEW keywords AS
  SELECT title AS term, id AS note_id, 'title' AS kind FROM notes WHERE title <> '';

CREATE TABLE embeddings (
  note_id INTEGER PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
  hash    TEXT NOT NULL,
  vector  TEXT NOT NULL
);

CREATE VIRTUAL TABLE notes_fts USING fts5(body, tokenize='trigram');
```

Column notes:

- `notes.id`: numeric note id. Regular notes use time-derived second buckets plus a sequence; journal notes use `yyyyMMdd`.
- `notes.kind`: file kind used with `id` to derive the path. Current values are `note` and `journal` for indexed notes; `template` is reserved for template files.
- `notes.title`: cached sidecar title used as the primary keyword.
- `notes.created`: cached metadata creation date string.
- `notes.mtime`: note file modification time as a Unix timestamp. `RefreshIfStale` compares it against the files on disk to detect notes added, edited, or removed outside track.
- `notes.icon`: cached sidecar icon override; display resolution is sidecar icon > tag icon > kind icon.
- `tags.note_id`: metadata rows attached to a note. They are replaced on note upsert.
- `links.src_id` and `links.dst_id`: computed directed note links. Self-links are ignored by the writer.
- `note_days.day`: one local calendar day the note was active, mirrored from the sidecar `days` field and replaced on note upsert. When a sidecar has no `days` yet, the upsert falls back to its `created` day so the note still surfaces on the day it was made. Journals (the daily note and the month/year summary journals) contribute no rows: activity tracks real notes worked on, so a day's journal does not count as activity on the days it is opened. The `agenda` query and the web activity heatmap read from this table and so exclude journals.
- `tasks`: one row per checkbox line of a note, replaced on upsert; `state` is the configured task state, `done` its terminal flag, and `priority`/`scheduled`/`due`/`completed` the parsed task fields (ADR 0035).
- `props`: a note's flattened typed properties — sidecar props at `line = 0`, inline `key:: value` body fields at their 1-based line; a list value is one row per item with `ord` preserving order (ADR 0032).
- `embeddings`: one cached vector per note for `track similar`; `hash` is the content hash the vector was computed from, so unchanged notes are never re-embedded (ADR 0037).
- `notes_fts`: FTS5 trigram table whose rowid is the note id and whose `body` is the parsed note text, giving case-insensitive substring body search with bm25 ranking (ADR 0045).
- `keywords`: convenience view used by keyword dumping, resolution, and `[[...]]` link highlighting.

## Deletion

During a full reindex, notes missing from the filesystem are removed from the SQLite index, and their sidecar metadata files are moved into `.track/trash/` with the same `<stamp>-<basename>` naming `track rm` uses. The sidecar is authoritative data and a sync gap looks identical to a deletion, so the indexer sets sidecars aside instead of destroying them.

When the scan finds no note files at all while the index still lists several notes, the reindex refuses to reconcile: an unmounted or unreadable vault must fail loudly rather than silently empty the index. A small floor keeps legitimate cases working — emptying a tiny vault (down to `track rm` of its last file) still reconciles — so the refusal fires only when a populated index would be wiped wholesale. If the notes really are gone, `track reindex` resets the database and rebuilds from what is on disk.

Run `track doctor` when a vault may be only partially synced: it reports orphan sidecars (and other divergence) read-only, so a sync gap is not silently treated as a deletion. See [agent-workflows.md](agent-workflows.md) and ADR 0014.

## Durability: do not delete `.track/notes/`

The vault and cache hold two very different kinds of data:

- `.track/notes/<id>.yaml` are the **authoritative** per-note metadata sidecars. The markdown body is content only; `title`, `tags`, `created`, `days`, and Babel block results live in the sidecar and cannot be reconstructed from the `.md` file.
- `.track/renames.yaml` is repair history for manual title edits. It can improve unresolved-link quickfixes, but it is not used for normal link resolution.
- The SQLite index under the cache directory is **rebuildable**. The notes on disk are the source of truth; `track reindex --full` deletes the cache database and regenerates it from them. Deleting it is safe.

Deleting `.track/notes/` is therefore irrecoverable data loss. Treat it like `.git`: keep it under version control and back it up alongside the note bodies.

track does **not** reconstruct lost metadata from the note body, because rebuilding a sidecar from the `.md` alone would silently drop tags and block results while appearing to succeed. The `track doctor --fix` repair is deliberately limited to restoring *structure and identity*, never inventing content: a missing sidecar is recreated with a placeholder `Untitled N` title, an orphan sidecar's markdown is recreated empty, a duplicate title is renumbered, and a stray conflict copy is imported as a new note. It never recovers the original title, tags, or block results — a backup of `.track/notes/` is still the only way to get those back. See ADR 0014 for the health-check and repair model.
