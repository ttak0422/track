// Package store wraps the SQLite index that backs track's search, keyword dictionary, and link graph.
// It uses modernc.org/sqlite (pure Go, no cgo) so the binary stays statically buildable under Nix.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the index database at dbPath, applying the schema on first use.
// The parent directory is created if missing.
func Open(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Reset removes the rebuildable SQLite cache files for dbPath.
func Reset(dbPath string) error {
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Store) ensureSchema() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= schemaVersion {
		return s.ensureCompatibleIndexes()
	}
	// An older schema is present (version > 0): the index is a rebuildable cache, so drop the existing
	// objects and re-apply. The emptied store is repopulated by the next RefreshIfStale -> Full, which
	// sees no indexed mtimes and reparses every note. A fresh database (version 0) has nothing to drop.
	if version > 0 {
		if err := s.dropAll(); err != nil {
			return fmt.Errorf("drop stale schema: %w", err)
		}
	}
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// user_version tracks the SQLite schema version independently from metadata file versions.
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}
	return s.ensureCompatibleIndexes()
}

// dropAll removes every user-defined table and view so a stale schema can be rebuilt in place. Dropping a
// table also drops its indexes, so they need no separate handling.
func (s *Store) dropAll() error {
	rows, err := s.db.Query(`SELECT type, name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	type object struct{ kind, name string }
	var objects []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.kind, &o.name); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, o := range objects {
		if _, err := s.db.Exec(fmt.Sprintf("DROP %s IF EXISTS %q", o.kind, o.name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureCompatibleIndexes() error {
	_, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notes_kind_mtime ON notes(kind, mtime)`)
	return err
}
