# Storage Specification

This document describes the current on-disk data model.

## Vault

The vault must be configured explicitly with `$TRACK_VAULT`. track does not
fallback to an implicit user data directory because accidentally creating or
reading the wrong vault is worse than failing early.

Notes are markdown files directly under the vault and are named by note id:

```text
<vault>/<unix-timestamp>.md
```

The first supported extension is `.md`; newly created notes use that extension.

Journal notes are stored separately:

```text
<vault>/journal/yyyyMMdd.md
```

The journal file name is also the numeric journal note id. The `yyyyMMdd`
format is chosen so lexical file order follows chronological order.

## Track Directory

track-owned data lives under:

```text
<vault>/.track/
```

Current contents:

```text
<vault>/.track/index.db
<vault>/.track/notes/<id>.yaml
```

`.track/index.db` is the SQLite index. `.track/notes/` contains versioned
sidecar metadata files for notes.

## Note Metadata

Metadata is separate from the markdown note body. For a note:

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
- `title`: note title and primary auto-link keyword. This mirrors the first H1
  heading in the markdown body when one exists.
- `aliases`: additional auto-link keywords.
- `tags`: note tags.
- `created`: creation date string. The current format is `YYYY-MM-DD`.

Readers reject unsupported metadata versions. If a sidecar is missing, the
current parser can still read the legacy trailing `<!--track ... -->` metadata
block for compatibility, but new writes must use sidecar metadata.

The markdown body is authoritative for fields it can express. If the first H1
heading and `metadata.title` disagree, parsing or reindexing updates the sidecar
title from the body while preserving fields that cannot currently be derived
from the body, such as aliases, tags, and created date.

## SQLite Index

The SQLite index is derived state. It can be rebuilt from markdown note files
and sidecar metadata. The indexer scans supported note files recursively under
the vault, excluding hidden directories such as `.track`.

Schema version 1 contains:

- `notes`: note id, path, title, body, created date, and mtime.
- `aliases`: aliases for each note.
- `tags`: tags for each note.
- `links`: computed directed links between notes.
- `keywords`: a view over note titles and aliases.

The index uses WAL mode and foreign keys.

## Deletion

During a full reindex, notes missing from the filesystem are removed from the
SQLite index. Their sidecar metadata files are also removed.
