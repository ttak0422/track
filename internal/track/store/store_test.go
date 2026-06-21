package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenAppliesSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sub", "index.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	tables := []string{"notes", "tags", "links"}
	for _, name := range tables {
		var got string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", name,
		).Scan(&got)
		if err != nil {
			t.Fatalf("table %q not found: %v", name, err)
		}
	}

	rows, err := s.db.Query("PRAGMA table_info(notes)")
	if err != nil {
		t.Fatalf("notes schema: %v", err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan notes column: %v", err)
		}
		columns[name] = true
	}
	for _, removed := range []string{"path", "body"} {
		if columns[removed] {
			t.Fatalf("notes table should not cache %s", removed)
		}
	}
	if !columns["kind"] {
		t.Fatalf("notes table should store file kind")
	}

	var view string
	if err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='view' AND name='keywords'",
	).Scan(&view); err != nil {
		t.Fatalf("keywords view not found: %v", err)
	}
	var indexName string
	if err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_notes_kind_mtime'",
	).Scan(&indexName); err != nil {
		t.Fatalf("mtime index not found: %v", err)
	}

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
	}
}

func TestOpenRebuildsStaleSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")

	// Simulate an older schema (no note_days table) stamped with an earlier user_version, holding a row.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, kind TEXT, title TEXT, created TEXT, mtime INTEGER);
		INSERT INTO notes (id, kind, title) VALUES (1, 'note', 'Old');
		PRAGMA user_version = 1;`); err != nil {
		t.Fatalf("seed old schema: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Open must rebuild in place rather than fail on the pre-existing notes table.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open over stale schema: %v", err)
	}
	defer s.Close()

	// The new note_days table exists and the stale row was dropped (the cache is rebuilt from disk later).
	var name string
	if err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='note_days'",
	).Scan(&name); err != nil {
		t.Fatalf("note_days table not found after rebuild: %v", err)
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&count); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if count != 0 {
		t.Fatalf("rebuilt notes table should be empty, got %d rows", count)
	}
}

func TestOpenIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}
