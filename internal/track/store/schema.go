package store

// schemaVersion is bumped whenever the DDL below changes in a way that requires a rebuild.
// The schema is applied once when the database is fresh.
const schemaVersion = 3

// schemaSQL defines a rebuildable SQLite index, not the primary source of truth.
// Notes and sidecar metadata on disk are authoritative; this database caches keyword rows and computed links for fast lookup.
// notes.mtime stores the note file's last modification time as a Unix timestamp and is reserved for change detection and incremental reindexing.
const schemaSQL = `
CREATE TABLE notes (
  id      INTEGER PRIMARY KEY,
  kind    TEXT NOT NULL DEFAULT 'note',
  title   TEXT NOT NULL DEFAULT '',
  created TEXT,
  mtime   INTEGER NOT NULL DEFAULT 0
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

CREATE VIEW keywords AS
  SELECT title AS term, id AS note_id, 'title' AS kind FROM notes WHERE title <> '';

-- Full-text body index. rowid is the note id; body is the same text the indexer parses
-- (legacy footmatter stripped, code fences kept). The trigram tokenizer gives case-insensitive
-- substring matching that also works for CJK, matching the old per-file grep semantics while
-- adding bm25 ranking. Terms shorter than 3 characters cannot form a trigram, so callers fall
-- back to a per-file scan for those (see the CLI body search).
CREATE VIRTUAL TABLE notes_fts USING fts5(body, tokenize='trigram');
`
