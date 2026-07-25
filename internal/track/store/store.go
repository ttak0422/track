// Package store wraps the SQLite index that backs track's search, keyword dictionary, and link graph.
// It uses modernc.org/sqlite (pure Go, no cgo) so the binary stays statically buildable under Nix.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
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

	// The pragmas ride in the DSN so the driver applies them to every connection the pool opens.
	// Running them once as a statement would configure only whichever connection served that call:
	// a long-lived server answering concurrent requests opens more, and those would silently get
	// SQLite's defaults — foreign_keys OFF, so the schema's ON DELETE CASCADE would stop firing and
	// deleting a note would strand its tags, links, days, tasks and props.
	//
	// busy_timeout makes concurrent openers wait out a writer instead of failing with SQLITE_BUSY —
	// several track processes (CLI, web, LSP) share one DB per vault, and a federated connection
	// holds several vaults' DBs at once.
	dsn := "file:" + url.PathEscape(dbPath) +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
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

// ensureSchema brings the database up to schemaVersion. The check and the write happen under one
// write lock, and the version is re-read inside it, because two openers can reach a brand-new
// vault's index at the same moment — two track processes, or two requests in the long-lived web
// server. Without that, both would see version 0 and the loser would fail with "table notes already
// exists"; with it, the loser waits out the winner (busy_timeout) and finds the schema in place.
func (s *Store) ensureSchema() error {
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("lock for schema check: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var version int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version < schemaVersion {
		// An older schema is present (version > 0): the index is a rebuildable cache, so drop the
		// existing objects and re-apply. The emptied store is repopulated by the next RefreshIfStale
		// -> Full, which reparses every note. A fresh database (version 0) has nothing to drop.
		if version > 0 {
			if err := s.dropAllOn(conn); err != nil {
				return fmt.Errorf("drop stale schema: %w", err)
			}
		}
		if _, err := conn.ExecContext(context.Background(), schemaSQL); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		// user_version tracks the SQLite schema version independently from metadata file versions.
		if _, err := conn.ExecContext(context.Background(), fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		return fmt.Errorf("commit schema: %w", err)
	}
	committed = true
	return s.ensureCompatibleIndexes()
}

// dropAllOn removes every user-defined table and view so a stale schema can be rebuilt in place,
// running on the connection that already holds the schema write lock. Dropping a table also drops
// its indexes, so they need no separate handling.
func (s *Store) dropAllOn(conn *sql.Conn) error {
	ctx := context.Background()
	rows, err := conn.QueryContext(ctx, `SELECT type, name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'`)
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
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP %s IF EXISTS %q", o.kind, o.name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureCompatibleIndexes() error {
	_, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notes_kind_mtime ON notes(kind, mtime)`)
	return err
}
