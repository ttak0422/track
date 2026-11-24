package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTimelineProjectIntervalsReadOnly(t *testing.T) {
	vault := t.TempDir()
	if err := os.Mkdir(filepath.Join(vault, ".track"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, code := runIn(t, vault, "timeline-session", "init", "--project", "demo", "--title", "Demo"); code != 0 {
		t.Fatalf("fixture init: %v", out)
	}
	path := filepath.Join(vault, ".track", "timelines", "demo.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) (map[string]any, int) {
		t.Helper()
		return runIn(t, vault, append([]string{"timeline-project", "intervals", "--id", "demo"}, args...)...)
	}
	got, code := call("--limit", "2", "--until", "2020-05-01T09:30:00+09:00")
	if code != 0 || got["revision"] != float64(1) || got["interval_minutes"] != float64(15) || got["project_id"] != "demo" ||
		got["schema_version"] != float64(1) {
		t.Fatalf("interval list: %v (%d)", got, code)
	}
	list := got["intervals"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["start"] != "2020-05-01T00:15:00Z" ||
		list[1].(map[string]any)["end"] != "2020-05-01T00:15:00Z" {
		t.Fatalf("UTC interval boundaries: %v", list)
	}
	same, code := call("--limit", "2", "--until", "2020-05-01T00:30:00Z")
	if code != 0 || !reflect.DeepEqual(same, got) {
		t.Fatalf("same instant should yield the same projection: %v (%d)", same, code)
	}
	earliest := time.Now().UTC().Truncate(15 * time.Minute)
	defaults, code := call()
	latest := time.Now().UTC().Truncate(15 * time.Minute)
	if code != 0 {
		t.Fatalf("default intervals: %v", defaults)
	}
	list = defaults["intervals"].([]any)
	if len(list) != 8 {
		t.Fatalf("default interval count: %d", len(list))
	}
	endStr := list[0].(map[string]any)["end"].(string)
	end, err := time.Parse(time.RFC3339Nano, endStr)
	if err != nil || end.Before(earliest) || end.After(latest) {
		t.Fatalf("default completed boundary %q: %v", endStr, err)
	}
	for _, args := range [][]string{
		{"--limit", "0"}, {"--limit", "97"}, {"--until="}, {"--until", "2020-05-01T00:30:00"},
		{"--until", "2020-05-01T00:30:01Z"}, {"--until", "9999-01-01T00:00:00Z"},
	} {
		if out, code := call(args...); code == 0 {
			t.Fatalf("accepted %v: %v", args, out)
		}
	}
	if _, code := runIn(t, vault, "timeline-project", "intervals", "--id", "missing"); code == 0 {
		t.Fatal("missing project accepted")
	}
	if out, code := runIn(t, filepath.Join(vault, "not-a-vault"), "timeline-project", "intervals", "--id", "demo"); code == 0 {
		t.Fatalf("missing vault accepted: %v", out)
	}
	for _, p := range []string{filepath.Join(vault, ".test-cache"), filepath.Join(vault, "not-a-vault")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("read created %s: %v", p, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("read modified journal: %v", err)
	}
}

func TestTimelineIntervalsAuthoredProgressCLI(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "init"); code != 0 {
		t.Fatal("init")
	}
	apply := func(raw string, rev float64) {
		t.Helper()
		got, code := runInWithStdin(t, vault, raw, "timeline-project", "apply", "--id", "demo")
		if code != 0 || got["revision"] != rev {
			t.Fatalf("apply: %+v (%d)", got, code)
		}
	}
	apply(`{"source":"cli-test","event_id":"create","expected_revision":0,"kind":"project.create","payload":{"title":"Work"}}`, 1)
	apply(`{"source":"cli-test","event_id":"active","expected_revision":1,"kind":"task.upsert","payload":{"id":"alpha","title":"Test","status":"active"}}`, 2)
	progress := `{"source":"interval-progress-v1","event_id":"progress-result-test","expected_revision":2,"kind":"discussion.add","payload":{"id":"progress-result-test","target_type":"task","target_id":"alpha","author":"agent","text":"Fixture passes"}}`
	apply(progress, 3)
	apply(progress, 3)
	path := filepath.Join(vault, ".track", "timelines", "demo.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal struct {
		SchemaVersion int              `json:"schema_version"`
		ProjectID     string           `json:"project_id"`
		Events        []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	for _, e := range journal.Events[:2] {
		e["at"] = "2020-05-01T00:14:59Z"
	}
	journal.Events[2]["at"] = "2020-05-01T00:15:00Z"
	before, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(extra ...string) (map[string]any, int) {
		return runIn(t, vault, append([]string{"timeline-project", "intervals", "--id", "demo", "--limit", "1", "--until", "2020-05-01T00:30:00Z"}, extra...)...)
	}
	for _, version := range []string{"0", "3"} {
		if out, code := call("--evidence-version", version); code == 0 {
			t.Fatalf("accepted version %s: %+v", version, out)
		}
	}
	v1, code := call()
	if code != 0 {
		t.Fatalf("v1: %+v", v1)
	}
	if legacy := v1["intervals"].([]any)[0].(map[string]any); legacy["progress"] != nil || legacy["evidence_sequences"] != nil {
		t.Fatalf("legacy output changed: %+v", legacy)
	}
	v2, code := call("--evidence-version", "2")
	if code != 0 || v2["schema_version"] != float64(2) || v2["report_source"] != "interval-report-v2" {
		t.Fatalf("v2: %+v", v2)
	}
	got := v2["intervals"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(got["evidence_sequences"], []any{float64(3)}) || len(got["transitions"].([]any)) != 0 || got["progress"].([]any)[0].(map[string]any)["text"] != "Fixture passes" {
		t.Fatalf("active-only progress: %+v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read mutated journal", err)
	}
	apply(`{"source":"interval-report-v2","event_id":"interval-20200501T0030Z","expected_revision":3,"kind":"report.add","payload":{"id":"interval-20200501T0030Z","author":"agent","period_start":"2020-05-01T00:15:00Z","period_end":"2020-05-01T00:30:00Z","summary":"Fixture passes","event_sequences":[3]}}`, 4)
	v2, code = call("--evidence-version", "2")
	if code != 0 || v2["intervals"].([]any)[0].(map[string]any)["report_status"] != "saved" {
		t.Fatalf("progress-only receipt: %+v", v2)
	}
	if out, code := runInWithStdin(t, vault, `{"source":"interval-report","event_id":"interval-20200501T0030Z","expected_revision":4,"kind":"report.add","payload":{"id":"interval-20200501T0030Z","author":"legacy","period_start":"2020-05-01T00:15:00Z","period_end":"2020-05-01T00:30:00Z","summary":"Must not replace","event_sequences":[2]}}`, "timeline-project", "apply", "--id", "demo"); code == 0 {
		t.Fatalf("duplicate cross-version receipt accepted: %+v", out)
	}
}
