package webui

import (
	"net/http"
	"os"
	"testing"

	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/task"
)

func TestTimelineEndpoint(t *testing.T) {
	server, cfg := putNoteSetup(t, 900, "Plan", "- [/] parent\n  - [x] child\n")
	meta, _, err := note.ReadMetadata(cfg.MetadataPath(900))
	if err != nil {
		t.Fatal(err)
	}
	meta.TaskLog = []task.LogEntry{{At: "2026-11-15T01:00:00Z", Line: 8, From: "TODO", To: "DONE", Text: "old task"}}
	if err := note.WriteMetadata(cfg.MetadataPath(900), meta); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfg.MetadataPath(900))
	if err != nil {
		t.Fatal(err)
	}
	got := getJSON(t, server.URL+"/api/timeline?id=900")
	if got["schema_version"] != float64(1) || got["title"] != "Plan" {
		t.Fatalf("snapshot: %+v", got)
	}
	items := got["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["parent_line"] != float64(1) {
		t.Fatalf("tree: %+v", items)
	}
	if len(got["history"].([]any)) != 1 {
		t.Fatalf("history: %+v", got)
	}
	after, _ := os.ReadFile(cfg.MetadataPath(900))
	if string(before) != string(after) {
		t.Fatal("read mutated sidecar")
	}
	resp, err := http.Get(server.URL + "/api/timeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing id: %d", resp.StatusCode)
	}
}
