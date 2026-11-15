package task

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTimelineTreeAndProgress(t *testing.T) {
	body := "- [/] parent\n  - [x] child\n  - [?] waiting\n- [-] cancelled\n\n# Another list\n  - [ ] root\n```md\n- [ ] example\n```\n"
	got := NewTimeline(42, "Plan", body, nil)
	if len(got.Items) != 5 {
		t.Fatalf("items: %+v", got.Items)
	}
	if got.Items[1].ParentLine != 1 || got.Items[1].Depth != 1 || got.Items[2].ParentLine != 1 {
		t.Fatalf("nested tasks: %+v", got.Items)
	}
	if got.Items[3].ParentLine != 0 || got.Items[4].ParentLine != 0 {
		t.Fatalf("roots: %+v", got.Items)
	}
	if got.Progress != (Progress{Total: 5, Done: 1, Cancelled: 1, Open: 3}) {
		t.Fatalf("progress: %+v", got.Progress)
	}
}

func TestTimelineHistoryIsNotJoinedOrSorted(t *testing.T) {
	history := []LogEntry{{At: "2026-11-15T02:00:00Z", Line: 99, From: "TODO", To: "DOING", Text: "removed"},
		{At: "2026-11-15T01:00:00Z", Line: 1, From: "DOING", To: "DONE", Text: "old task"}}
	got := NewTimeline(42, "Plan", "- [ ] replacement", history)
	if got.History[0].Line != 99 || got.History[1].Text != "old task" {
		t.Fatalf("history: %+v", got.History)
	}
	got.History[0].Text = "changed"
	if history[0].Text != "removed" {
		t.Fatal("aliased authoritative history")
	}
}

func TestTimelineEmptyArrays(t *testing.T) {
	raw, err := json.Marshal(NewTimeline(1, "Empty", "", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"items":[]`, `"history":[]`, `"schema_version":1`} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("missing %s in %s", expected, raw)
		}
	}
}

func TestTimelineNonTaskSiblingEndsChildContext(t *testing.T) {
	got := NewTimeline(1, "", "- [ ] parent\n  - [ ] child\n  - plain container\n    - [ ] another\n", nil)
	if got.Items[2].ParentLine != 1 || got.Items[2].Depth != 1 {
		t.Fatalf("wrong ancestor: %+v", got.Items)
	}
}
