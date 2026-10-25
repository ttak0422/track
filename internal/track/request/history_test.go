package request

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/config"
)

// This is a pre-retirement v1 record, including a completed attempt, follow-up context,
// a saved answer, and an interrupted apply. Reads must preserve all of it without recovery.
const historyV1 = `{"version":1,"id":"req-old","vault":"research","vault_path":"/old/vault","intent":"update","instruction":"revise","agent_id":"old-agent","client_request_id":"client-old","parent_request_id":"parent-old","input_fingerprint":"input-old","status":"applying","created_at":"2026-07-01T12:00:00Z","updated_at":"2026-07-01T12:01:00Z","context":{"quote":"selected text","note":{"note_id":100,"title":"Original","body":"old body","etag":"old","file_kind":"note"},"references":[{"note_id":200}],"prior_answers":[{"request_id":"parent-old","answer_markdown":"prior answer"}]},"update_target":{"note_id":100,"body":"old body","etag":"old","file_kind":"note"},"attempts":[{"id":"dispatch-old","status":"completed","delivery":"sent","result_fingerprint":"result-old"}],"result":{"answer_markdown":"stored answer","sources":["https://example.org/source"],"proposed_body":"new body","fingerprint":"result-old","saved":{"client_request_id":"save-old","vault":"research","note_id":300,"title":"Saved answer","status":"saved"},"apply":{"before_body":"old body","before_etag":"old","after_body":"new body","after_etag":"new","reason":"rationale"}}}`

func historyStore(t *testing.T) (*Store, *config.Config) {
	t.Helper()
	cfg := &config.Config{VaultDir: t.TempDir()}
	if err := os.MkdirAll(cfg.RequestsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RequestsDir(), "req-old.json"), []byte(historyV1), 0o600); err != nil {
		t.Fatal(err)
	}
	return New(cfg), cfg
}

func TestHistoryV1RemainsReadableAndUnchanged(t *testing.T) {
	st, cfg := historyStore(t)
	rec, err := st.Get("req-old")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusApplying || rec.Result.AnswerMarkdown != "stored answer" || rec.Result.Saved.NoteID != 300 || rec.Result.Apply.BeforeBody != "old body" || rec.Context.PriorAnswers[0].AnswerMarkdown != "prior answer" || rec.Attempts[0].ResultFingerprint != "result-old" {
		t.Fatalf("lost historical fields: %+v", rec)
	}
	records, cursor, err := st.List(20, "")
	if err != nil || len(records) != 1 || cursor != "" {
		t.Fatalf("list = %v, %q, %v", records, cursor, err)
	}
	raw, err := os.ReadFile(filepath.Join(cfg.RequestsDir(), "req-old.json"))
	if err != nil || !bytes.Equal(raw, []byte(historyV1)) {
		t.Fatalf("read mutated history: %s, %v", raw, err)
	}
}

func TestHistoryReadDoesNotCreateStorage(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	st := New(cfg)
	records, cursor, err := st.List(0, "")
	if err != nil || len(records) != 0 || cursor != "" {
		t.Fatalf("missing history: %v, %q, %v", records, cursor, err)
	}
	if _, err := st.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := os.Stat(cfg.TrackDir()); !os.IsNotExist(err) {
		t.Fatalf("read created storage: %v", err)
	}
}

func TestHistoryPaginationAndValidation(t *testing.T) {
	st, cfg := historyStore(t)
	newer := strings.ReplaceAll(historyV1, "req-old", "req-new")
	newer = strings.ReplaceAll(newer, "2026-07-01", "2026-07-02")
	if err := os.WriteFile(filepath.Join(cfg.RequestsDir(), "req-new.json"), []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	first, next, err := st.List(1, "")
	if err != nil || len(first) != 1 || first[0].ID != "req-new" || next != "req-new" {
		t.Fatalf("first: %v %q %v", first, next, err)
	}
	second, next, err := st.List(1, next)
	if err != nil || len(second) != 1 || second[0].ID != "req-old" || next != "" {
		t.Fatalf("second: %v %q %v", second, next, err)
	}
	for _, id := range []string{"../outside", "nested/../../outside", `..\outside`, ".", ""} {
		if _, err := st.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("unsafe id %q: %v", id, err)
		}
	}
	for name, raw := range map[string]string{
		"future":  strings.Replace(historyV1, `"version":1`, `"version":2`, 1),
		"status":  strings.Replace(historyV1, `"status":"applying"`, `"status":"unknown"`, 1),
		"attempt": strings.Replace(historyV1, `"status":"completed"`, `"status":"unknown"`, 1),
		"broken":  "{",
	} {
		path := filepath.Join(cfg.RequestsDir(), name+".json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Get(name); err == nil {
			t.Errorf("accepted %s", name)
		}
		if _, _, err := st.List(20, ""); err == nil {
			t.Errorf("list accepted %s", name)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}
