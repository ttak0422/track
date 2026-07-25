package store

// schemaVersion is bumped whenever the DDL below changes in a way that requires a rebuild.
// The schema is applied once when the database is fresh.
// 5: both the tasks table (task states) and the embeddings table (similar-notes) are present; each
// landed independently as "4", so any existing v4 database is missing one of them.
// 6: notes.meta_mtime records the sidecar file's mtime so RefreshIfStale also detects sidecar-only
// changes (a tag or title edit synced from another machine never touches the note body's mtime).
// 7: ext_links records outgoing cross-vault references ([[vault:title]]) as (vault, title) string
// keys — never the target's numeric id, which belongs to the other vault's namespace.
const schemaVersion = 7

// schemaSQL defines a rebuildable SQLite index, not the primary source of truth.
// Notes and sidecar metadata on disk are authoritative; this database caches keyword rows and computed links for fast lookup.
// notes.mtime stores the note file's last modification time as a Unix timestamp; RefreshIfStale compares it against disk to detect external changes.
const schemaSQL = `
CREATE TABLE notes (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL DEFAULT 'note',
  title      TEXT NOT NULL DEFAULT '',
  created    TEXT,
  mtime      INTEGER NOT NULL DEFAULT 0,
  meta_mtime INTEGER NOT NULL DEFAULT 0,
  icon       TEXT NOT NULL DEFAULT ''
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

-- ext_links holds outgoing cross-vault references ([[vault:title]]) by (vault name, title) string
-- key. The target's numeric id is deliberately absent: ids are vault-local, so a cross-vault edge
-- must never carry one. Inbound cross-vault backlinks are found by scanning other vaults' DBs for
-- rows naming this vault.
CREATE TABLE ext_links (
  src_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  vault  TEXT NOT NULL,
  title  TEXT NOT NULL,
  PRIMARY KEY (src_id, vault, title)
);
CREATE INDEX idx_ext_links_target ON ext_links(vault, title);

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

-- props holds a note's flattened typed properties: sidecar props (line = 0) and inline "key:: value"
-- body fields (line = 1-based). A list value is one row per item; ord preserves flattened order so a
-- list reads back in the order it was written.
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

-- embeddings caches one vector per note for semantic related-notes (track similar). hash is the content
-- hash the vector was computed from, so an unchanged note is never re-embedded; a stale hash triggers a
-- fresh shell-out to the configured embedder. vector is the JSON float array the embedder emitted. It is
-- a rebuildable cache like everything else here: dropping it only forces a re-embed on the next lookup.
CREATE TABLE embeddings (
  note_id INTEGER PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
  hash    TEXT NOT NULL,
  vector  TEXT NOT NULL
);

-- Full-text body index. rowid is the note id; body is the same text the indexer parses
-- (legacy footmatter stripped, code fences kept). The trigram tokenizer gives case-insensitive
-- substring matching that also works for CJK, matching the old per-file grep semantics while
-- adding bm25 ranking. Terms shorter than 3 characters cannot form a trigram, so callers fall
-- back to a per-file scan for those (see the CLI body search).
CREATE VIRTUAL TABLE notes_fts USING fts5(body, tokenize='trigram');
`
