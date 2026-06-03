# Storage Specification

This document describes the current on-disk data model.

## Vault

The vault must be configured explicitly with `$TRACK_VAULT`. track does not fallback to an implicit user data directory because accidentally creating or reading the wrong vault is worse than failing early.

Notes are markdown files directly under the vault and are named by note id:

```text
<vault>/<id>.md
```

The first supported extension is `.md`; newly created notes use that extension.

Regular notes use Unix timestamp ids. Journal notes use `yyyyMMdd` ids and follow the same flat path rule:

```text
<vault>/yyyyMMdd.md
```

Because note files are flat, a note path is derived from its id and is not stored in the SQLite cache.

## Track Directory

Authoritative track-owned vault data lives under:

```text
<vault>/.track/
```

Current contents:

```text
<vault>/.track/notes/<id>.yaml
```

`.track/notes/` contains versioned sidecar metadata files for notes.

The rebuildable SQLite index is a cache outside the vault. By default it lives under the platform user cache directory:

```text
<user-cache>/track/<vault-key>/index.db
```

`TRACK_CACHE_DIR` overrides the `track` cache directory. The Neovim frontend sets it to:

```text
vim.fn.stdpath("cache") .. "/track"
```

`TRACK_DB` can still point at an explicit database path for debugging or tests.

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

Version 1 metadata:

```yaml
version: 1
title: リンク
aliases:
  - link
  - TEST
tags:
  - zettel
created: 2026-05-24
```

Fields:

- `version`: metadata schema version. Required for new writes.
- `title`: note title and primary link keyword. This mirrors the first H1 heading in the markdown body when one exists.
- `aliases`: additional link keywords.
- `tags`: note tags.
- `created`: creation date string. The current format is `YYYY-MM-DD`.

Readers reject unsupported metadata versions.
If a sidecar is missing, the current parser can still read the legacy trailing `<!--track ... -->` metadata block for compatibility, but new writes must use sidecar metadata.

The markdown body is authoritative for fields it can express.
If the first H1 heading and `metadata.title` disagree, parsing or reindexing updates the sidecar title from the body while preserving fields that cannot currently be derived from the body, such as aliases, tags, and created date.

## SQLite Index

The SQLite index is derived state.
It can be rebuilt from markdown note files and sidecar metadata.
The indexer scans supported note files recursively under the vault, excluding hidden directories such as `.track`.
SQLite `PRAGMA user_version` stores the database schema version and is independent from sidecar metadata versions.

Schema version 1 contains:

- `notes`: note id, title, created date, and mtime.
- `aliases`: aliases for each note.
- `tags`: tags for each note.
- `links`: computed directed links between notes.
- `keywords`: a view over note titles and aliases.

The index uses WAL mode and foreign keys. It intentionally does not cache note paths or bodies: flat note paths are derived from note ids, and body search reads markdown files directly.

### Schema Version 1

```sql
CREATE TABLE notes (
  id      INTEGER PRIMARY KEY,
  title   TEXT NOT NULL DEFAULT '',
  created TEXT,
  mtime   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE aliases (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  alias   TEXT NOT NULL,
  PRIMARY KEY (note_id, alias)
);
CREATE INDEX idx_aliases_alias ON aliases(alias);

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

CREATE VIEW keywords AS
  SELECT title AS term, id AS note_id, 'title' AS kind FROM notes WHERE title <> ''
  UNION ALL
  SELECT alias AS term, note_id, 'alias' AS kind FROM aliases;
```

Column notes:

- `notes.id`: numeric note id. Regular notes use Unix timestamps; journal notes use `yyyyMMdd`.
- `notes.title`: cached title used as the primary keyword. It mirrors the first H1 heading when available.
- `notes.created`: cached metadata creation date string.
- `notes.mtime`: note file modification time as a Unix timestamp. It is kept for future change detection and incremental reindexing.
- `aliases.note_id` and `tags.note_id`: metadata rows attached to a note. They are replaced on note upsert.
- `links.src_id` and `links.dst_id`: computed directed note links. Self-links are ignored by the writer.
- `keywords`: convenience view used by keyword dumping, resolution, and `[[...]]` link highlighting.

## Deletion

During a full reindex, notes missing from the filesystem are removed from the SQLite index.
Their sidecar metadata files are also removed.

## Durability: do not delete `.track/notes/`

The vault and cache hold two very different kinds of data:

- `.track/notes/<id>.yaml` are the **authoritative** per-note metadata sidecars. The markdown body only owns the fields it can express (the first H1 owns the title); `aliases`, `tags`, `created`, and Babel block results live *only* in the sidecar and cannot be reconstructed from the `.md` file.
- The SQLite index under the cache directory is **rebuildable**. The notes on disk are the source of truth; `track reindex --full` regenerates the database from them. Deleting it is safe.

Deleting `.track/notes/` is therefore irrecoverable data loss for everything except note titles. Treat it like `.git`: keep it under version control and back it up alongside the note bodies.

track intentionally does **not** provide a metadata "repair" command. Rebuilding a sidecar from the note body alone would silently drop aliases, tags, and block results while appearing to succeed, which is more dangerous than a clear "restore from backup" rule.
