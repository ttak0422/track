# Schema-scoped live index caches

## Context

Native, web, LSP and one-shot CLI processes can run different Track builds against the
same vault. They previously shared `<cache>/<vault-key>/index.db`. Maintenance commands
unlinked that database and its WAL/SHM files before rebuilding it. A long-lived SQLite
pool could retain old connections while new connections opened the replacement file
without repeating `Store.Open`'s schema initialization.

In particular, a schema-10 CLI reindex can replace a schema-11 server's index with one
that has no `body_keywords` view. Every note response asks for hierarchy data, so this
breaks note opening regardless of whether it came from History, New or search.

## Decision

- Derive the index filename from the schema version (`index-v11.db` currently), using
  one version constant for both filename and DDL. Different schemas do not share a
  database. Leave old cache files untouched, including the former `index.db`.
- Reset a compatible cache with transactional SQLite DDL rather than unlinking its
  database, WAL or SHM. Existing read transactions keep their snapshot; subsequent
  transactions on the same connection see the rebuilt schema.
- Refuse a newer-than-supported database instead of attempting a downgrade.
- Populate a fresh or interrupted cache before index-dependent operations use it.
  The existing `index_state.link_format` row records a completed full index, including
  the empty value for title-based links. An empty table or a partial set of note rows
  is not proof that initialization completed. Cold population reads authoritative
  files without recording them as newly edited.

## Consequences

The first compatible launch builds its own cache; no vault migration or note renaming
is needed. Older cache files can consume disk space until removed with all processes
that use them stopped. `.track/` remains authoritative and must never be removed as
cache cleanup.

Schema reset is atomic, but repopulation is not a whole-vault transaction. A request
can still fail while a rebuild is in progress. The workspace keeps cached notes and
unsaved drafts visible on refetch errors and offers Retry; initial errors offer a real
close/escape action rather than implying that closing a tab repairs the shared index.
