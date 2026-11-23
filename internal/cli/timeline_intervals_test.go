package cli

import (
	"bytes"
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
