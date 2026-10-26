package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

// Released binaries used index.db and unlinked it during reindex. A new pooled
// connection then saw that binary's schema without re-running Store.Open. Before
// namespacing, this reproduces "no such table: body_keywords (1)" exactly.
func TestLegacyReindexCannotReplaceLiveCache(t *testing.T) {
	t.Setenv("TRACK_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	t.Setenv("TRACK_VAULT", t.TempDir())
	t.Setenv("TRACK_CACHE_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	live, err := Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	upsert(t, live, 1, "Parent", "", 1)
	upsert(t, live, 2, "Child", "up:: [[Parent]]", 2)
	// A long-lived database/sql handle can need fresh physical connections at any
	// point; no idle connections makes that transition deterministic.
	live.db.SetMaxIdleConns(0)
	legacyPath := filepath.Join(filepath.Dir(cfg.DBPath), "index.db")
	for i := 0; i < 3; i++ {
		for _, path := range []string{legacyPath, legacyPath + "-wal", legacyPath + "-shm"} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		legacy, err := sql.Open("sqlite", legacyPath)
		if err != nil {
			t.Fatal(err)
		}
		// Only the relevant schema difference is needed: v10 has no body_keywords.
		_, err = legacy.Exec(schemaSQL + `DROP VIEW body_keywords; PRAGMA user_version = 10;`)
		closeErr := legacy.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("legacy rebuild: %v / %v", err, closeErr)
		}
		parents, err := live.UpNotes(2)
		if err != nil || len(parents) != 1 || parents[0].NoteID != 1 {
			t.Fatalf("live hierarchy after legacy rebuild %d: %+v, %v", i, parents, err)
		}
	}
}

func TestResetKeepsLiveConnectionsAndReadSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	live, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	upsert(t, live, 1, "Before", "", 1)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reader, err := live.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.ExecContext(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	defer reader.ExecContext(ctx, "ROLLBACK")
	count := func(want int) {
		t.Helper()
		var n int
		if err := reader.QueryRowContext(ctx, "SELECT count(*) FROM body_keywords").Scan(&n); err != nil || n != want {
			t.Fatalf("snapshot count = %d, want %d: %v", n, want, err)
		}
	}
	count(1) // establish a WAL read snapshot before another connection resets
	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("reset replaced the live database: %v", err)
	}
	count(1) // the pre-reset reader remains valid, including its view
	if _, err := reader.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	count(0) // the same physical connection observes the committed rebuild
	for i := 0; i < 3; i++ {
		upsert(t, live, 1, "After", "", 1)
		upsert(t, live, 2, "Child", "up:: [[After]]", 2)
		if parents, err := live.UpNotes(2); err != nil || len(parents) != 1 {
			t.Fatalf("live query after reset: %+v, %v", parents, err)
		}
		if err := Reset(path); err != nil {
			t.Fatal(err)
		}
		count(0)
	}
}

func TestOpenAndResetRefuseNewerSchemaWithoutDroppingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE future_data (value TEXT); INSERT INTO future_data VALUES ('keep'); PRAGMA user_version = %d`, schemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []func() error{
		func() error {
			s, err := Open(path)
			if s != nil {
				s.Close()
			}
			return err
		},
		func() error { return Reset(path) },
	} {
		if err := attempt(); err == nil || !strings.Contains(err.Error(), "newer than supported") {
			t.Fatalf("expected schema compatibility error, got %v", err)
		}
		var value string
		if err := db.QueryRow("SELECT value FROM future_data").Scan(&value); err != nil || value != "keep" {
			t.Fatalf("future cache was mutated: %q, %v", value, err)
		}
	}
}

func TestResetRepairsMissingHierarchyView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	live, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	if _, err := live.db.Exec("DROP VIEW body_keywords"); err != nil {
		t.Fatal(err)
	}
	if _, err := live.UpNotes(1); err == nil || !strings.Contains(err.Error(), "no such table: body_keywords") {
		t.Fatalf("expected original failure, got %v", err)
	}
	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	upsert(t, live, 1, "Parent", "", 1)
	upsert(t, live, 2, "Child", "up:: [[Parent]]", 2)
	if parents, err := live.UpNotes(2); err != nil || len(parents) != 1 || parents[0].NoteID != 1 {
		t.Fatalf("live hierarchy did not recover: %+v, %v", parents, err)
	}
}
