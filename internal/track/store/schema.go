package store

// schemaVersion is bumped whenever the DDL below changes in a way that requires a rebuild.
// The schema is applied once when the database is fresh.
const schemaVersion = 1

// schemaSQL defines a rebuildable SQLite index, not the primary source of truth.
// Notes and sidecar metadata on disk are authoritative; this database caches keyword rows and computed links for fast lookup.
// notes.mtime stores the note file's last modification time as a Unix timestamp and is reserved for change detection and incremental reindexing.
const schemaSQL = `
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
`
