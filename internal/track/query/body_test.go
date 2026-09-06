package query

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
)

// TestRunWithBodyStoreResolver exercises the store-backed path end to end: body conditions resolve
// through the FTS5 index for terms long enough to form trigrams, and through the per-file scan
// fallback for shorter terms (2-character CJK), matching the split body search makes.
func TestRunWithBodyStoreResolver(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir(), Extensions: []string{".md"}}
	if err := os.MkdirAll(cfg.NoteDir(), 0o755); err != nil {
		t.Fatalf("mkdir note dir: %v", err)
	}
	// The files must exist for the short-term scan fallback, which reads them from disk; the FTS
	// path locates its line/snippet in them too.
	bodies := map[int64]string{
		1: "The quick brown fox jumps over the lazy dog",
		2: "世界の平和と共に歩む",
		3: "another note body entirely",
	}
	for id, body := range bodies {
		if err := os.WriteFile(filepath.Join(cfg.NoteDir(), strconv.FormatInt(id, 10)+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write note %d: %v", id, err)
		}
	}

	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	for id, body := range bodies {
		n := &note.Note{ID: id, Kind: "note", Mtime: id * 100, Meta: note.Metadata{Title: "Note " + strconv.FormatInt(id, 10)}, Body: body}
		if err := s.UpsertNote(n); err != nil {
			t.Fatalf("upsert %d: %v", id, err)
		}
	}

	rows, err := RowsFromStore(s)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	resolve := StoreBodyResolver(cfg, s, len(rows))
	runBody := func(expr string) []int64 {
		t.Helper()
		q, err := Parse(expr)
		if err != nil {
			t.Fatalf("parse %q: %v", expr, err)
		}
		res, err := RunWithBody(q, rows, resolve)
		if err != nil {
			t.Fatalf("run %q: %v", expr, err)
		}
		return ids(res)
	}

	// FTS path: a 3+ character term hits the trigram index.
	if got := runBody(`TABLE title WHERE body = "quick fox"`); !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("fts = %v, want [1]", got)
	}
	// Scan fallback: two-character CJK terms form no trigram and scan the files instead.
	if got := runBody(`TABLE title WHERE body = "世界"`); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("scan fallback = %v, want [2]", got)
	}
	// Complement and AND with a regular condition. Rows keep the store's recency order (newest
	// first), so hits come back [3 2].
	if got := runBody(`TABLE title WHERE body != quick`); !reflect.DeepEqual(got, []int64{3, 2}) {
		t.Fatalf("!= = %v, want [3 2]", got)
	}
	if got := runBody(`TABLE title WHERE body = entirely AND title = "Note 3"`); !reflect.DeepEqual(got, []int64{3}) {
		t.Fatalf("body AND title = %v, want [3]", got)
	}
	// Two body conditions resolve independently and AND together.
	if got := runBody(`TABLE title WHERE body = quick AND body != quick`); len(got) != 0 {
		t.Fatalf("contradictory body conds = %v, want none", got)
	}

	// No body conditions: RunWithBody with a resolver matches plain Run.
	plain, err := Parse(`TABLE title WHERE title = "Note 2"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Run(plain, rows)
	got, err := RunWithBody(plain, rows, resolve)
	if err != nil {
		t.Fatalf("run plain: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved plain run = %+v, want %+v", got, want)
	}
}
