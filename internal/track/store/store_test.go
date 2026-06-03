package store

import (
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

	tables := []string{"notes", "aliases", "tags", "links"}
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

	var view string
	if err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='view' AND name='keywords'",
	).Scan(&view); err != nil {
		t.Fatalf("keywords view not found: %v", err)
	}

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
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
