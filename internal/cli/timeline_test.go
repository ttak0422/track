package cli

import (
	"testing"
)

func TestTimelineCLI(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "init"); code != 0 {
		t.Fatal("init failed")
	}
	created, code := runInWithStdin(t, vault, "- [ ] parent\n  - [ ] child\n", "new", "--title", "Timeline plan")
	if code != 0 {
		t.Fatalf("create: %+v", created)
	}
	got, code := runIn(t, vault, "timeline", "--title", "Timeline plan")
	if code != 0 || got["schema_version"] != float64(1) {
		t.Fatalf("timeline: %+v (%d)", got, code)
	}
	if len(got["items"].([]any)) != 2 {
		t.Fatalf("items: %+v", got)
	}
	if _, code := runIn(t, vault, "timeline"); code == 0 {
		t.Fatal("missing selector accepted")
	}
}
