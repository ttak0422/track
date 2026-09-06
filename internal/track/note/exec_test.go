package note

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
	"github.com/ttak0422/track/internal/track/dispatch"
)

func execCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		VaultDir:   t.TempDir(),
		DBPath:     filepath.Join(t.TempDir(), ".track", "index.db"),
		Extensions: []string{".md"},
		DateFormat: "2006-01-02",
	}
}

func TestAppendExecTransitionAppendsAndBumpsVersion(t *testing.T) {
	cfg := execCfg(t)
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	rec, err := AppendExecTransition(cfg, 1000, dispatch.Record{
		ID:           "d-1000-1",
		Note:         1000,
		Status:       dispatch.StatusDispatched,
		DispatchedAt: now.Format(time.RFC3339),
	}, now)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if rec.At != now.Format(time.RFC3339) {
		t.Fatalf("At = %q, want the transition time %q", rec.At, now.Format(time.RFC3339))
	}

	meta, found, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil || !found {
		t.Fatalf("read metadata: found=%v err=%v", found, err)
	}
	if meta.Version < MetadataVersionV11 {
		t.Fatalf("an exec_log sidecar is at least v%d, got %d", MetadataVersionV11, meta.Version)
	}
	if len(meta.ExecLog) != 1 {
		t.Fatalf("exec log has %d records, want 1: %+v", len(meta.ExecLog), meta.ExecLog)
	}
	if !reflect.DeepEqual(meta.ExecLog[0], rec) {
		t.Fatalf("stored record mismatch:\n got %+v\nwant %+v", meta.ExecLog[0], rec)
	}
}

func TestAppendExecTransitionIsAppendOnly(t *testing.T) {
	cfg := execCfg(t)
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	rec := dispatch.Record{ID: "d-1000-1", Note: 1000, Status: dispatch.StatusPending}

	first, err := AppendExecTransition(cfg, 1000, rec, now)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	second, err := AppendExecTransition(cfg, 1000, dispatch.Record{
		ID:           rec.ID,
		Note:         rec.Note,
		Status:       dispatch.StatusDispatched,
		DispatchedAt: now.Add(time.Minute).Format(time.RFC3339),
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second append: %v", err)
	}

	meta, _, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.ExecLog) != 2 {
		t.Fatalf("exec log has %d records, want 2 (append-only): %+v", len(meta.ExecLog), meta.ExecLog)
	}
	// The first record is untouched by the second append.
	if !reflect.DeepEqual(meta.ExecLog[0], first) {
		t.Fatalf("first record mutated:\n got %+v\nwant %+v", meta.ExecLog[0], first)
	}
	if !reflect.DeepEqual(meta.ExecLog[1], second) {
		t.Fatalf("second record mismatch:\n got %+v\nwant %+v", meta.ExecLog[1], second)
	}
}

func TestAppendExecTransitionCreatesSidecarForNewNote(t *testing.T) {
	cfg := execCfg(t)
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.FixedZone("JST", 9*3600))

	if _, err := AppendExecTransition(cfg, 1000, dispatch.Record{
		ID:     "d-1000-1",
		Note:   1000,
		Status: dispatch.StatusPending,
	}, now); err != nil {
		t.Fatalf("append: %v", err)
	}

	meta, found, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected the append to create the sidecar")
	}
	if meta.Created != now.Format(cfg.DateFormat) {
		t.Fatalf("Created = %q, want %q", meta.Created, now.Format(cfg.DateFormat))
	}
}

func TestAppendExecTransitionRejectsUnknownStatus(t *testing.T) {
	cfg := execCfg(t)
	_, err := AppendExecTransition(cfg, 1000, dispatch.Record{
		ID:     "d-1000-1",
		Note:   1000,
		Status: "ready",
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "invalid dispatch status") {
		t.Fatalf("expected an invalid-status rejection, got %v", err)
	}
	if _, found, _ := ReadMetadata(cfg.MetadataPath(1000)); found {
		t.Fatal("a refused append must not write a sidecar")
	}
}

func TestAppendExecTransitionRejectsNoteMismatch(t *testing.T) {
	cfg := execCfg(t)
	_, err := AppendExecTransition(cfg, 1000, dispatch.Record{
		ID:     "d-1000-1",
		Note:   1001,
		Status: dispatch.StatusPending,
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "targets note") {
		t.Fatalf("expected a note-mismatch rejection, got %v", err)
	}
}

func TestAppendExecTransitionRejectsMissingDispatchID(t *testing.T) {
	cfg := execCfg(t)
	_, err := AppendExecTransition(cfg, 1000, dispatch.Record{
		Note:   1000,
		Status: dispatch.StatusPending,
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "no id") {
		t.Fatalf("expected a missing-id rejection, got %v", err)
	}
}

func TestParseFileKeepsExecLog(t *testing.T) {
	cfg := execCfg(t)
	path := cfg.NotePath(1000)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Body\n\nplain content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := dispatch.Record{
		ID:              "d-1000-1",
		Note:            1000,
		Status:          dispatch.StatusDispatched,
		DispatchedAt:    "2026-09-06T10:00:00Z",
		LastHeartbeatAt: "2026-09-06T10:05:00Z",
	}
	if err := WriteMetadata(cfg.MetadataPath(1000), Metadata{ExecLog: []dispatch.Record{in}}); err != nil {
		t.Fatal(err)
	}

	n, err := ParseFile(path, cfg)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	// The execution log is sidecar data, never derived from the body: a body that says nothing
	// about executions still reads back the record, and the sidecar is not rewritten.
	if len(n.Meta.ExecLog) != 1 || !reflect.DeepEqual(n.Meta.ExecLog[0], in) {
		t.Fatalf("exec log lost in parse: %+v", n.Meta.ExecLog)
	}
	got, found, err := ReadMetadata(cfg.MetadataPath(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !found || !reflect.DeepEqual(got.ExecLog, []dispatch.Record{in}) {
		t.Fatalf("sidecar exec log changed: found=%v %+v", found, got.ExecLog)
	}
}

func TestWriteReadMetadataExecLogBumpsVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".track", "notes", "1001.yaml")
	in := Metadata{Title: "Alpha", ExecLog: []dispatch.Record{{
		ID:     "d-1001-1",
		Note:   1001,
		Status: dispatch.StatusPending,
	}}}
	if err := WriteMetadata(path, in); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	got, found, err := ReadMetadata(path)
	if err != nil || !found {
		t.Fatalf("read metadata: found=%v err=%v", found, err)
	}
	if got.Version < MetadataVersionV11 {
		t.Fatalf("an exec_log sidecar is at least v%d, got %d", MetadataVersionV11, got.Version)
	}
	in.Version = MetadataVersionV11
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("exec_log metadata mismatch:\n got %+v\nwant %+v", got, in)
	}
}
