package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAgentTimelineCLI(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "init"); code != 0 {
		t.Fatal("init failed")
	}
	raw := `{"source":"cli-test","event_id":"create","expected_revision":0,"kind":"project.create","payload":{"title":"Work","mode":"serial"}}`
	created, code := runInWithStdin(t, vault, raw, "timeline-project", "apply", "--id", "example")
	if code != 0 || created["revision"] != float64(1) {
		t.Fatalf("create: %+v (%d)", created, code)
	}
	retry, code := runInWithStdin(t, vault, raw, "timeline-project", "apply", "--id", "example")
	if code != 0 || retry["revision"] != float64(1) {
		t.Fatalf("retry: %+v (%d)", retry, code)
	}
	got, code := runIn(t, vault, "timeline-project", "get", "--id", "example")
	if code != 0 || got["title"] != "Work" {
		t.Fatalf("get: %+v (%d)", got, code)
	}
	listed, code := runIn(t, vault, "timeline-project", "list")
	if code != 0 || len(listed["projects"].([]any)) != 1 {
		t.Fatalf("list: %+v (%d)", listed, code)
	}
	stale := `{"source":"cli-test","event_id":"update","expected_revision":0,"kind":"project.update","payload":{"title":"Changed","mode":"serial"}}`
	if _, code := runInWithStdin(t, vault, stale, "timeline-project", "apply", "--id", "example"); code == 0 {
		t.Fatal("stale revision accepted")
	}
	for _, args := range [][]string{{"get"}, {"list", "--id", "example"}, {"get", "--id", "../bad"}, {"get", "--id", "missing"}, {"unknown"}} {
		if _, code := runIn(t, vault, append([]string{"timeline-project"}, args...)...); code == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestAgentTimelineCLIRejectsMalformedRequest(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "init"); code != 0 {
		t.Fatal("init failed")
	}
	for i, raw := range []string{
		`null`, `{}`, `{"expected_revision":null}`, `{"expected_revision":0,"unknown":true}`,
		`{"expected_revision":0} {}`, `{"expected_revision":-1}`,
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			out, code := runInWithStdin(t, vault, raw, "timeline-project", "apply", "--id", "example")
			if code == 0 || out["error"] == nil {
				t.Fatalf("malformed input accepted: %+v", out)
			}
		})
	}
}

func TestAgentTimelineCLIRequestLimit(t *testing.T) {
	vault := t.TempDir()
	if _, code := runIn(t, vault, "init"); code != 0 {
		t.Fatal("init failed")
	}
	f, err := os.CreateTemp(t.TempDir(), "request")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Repeat(" ", 1<<20+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	out, code := runIn(t, vault, "timeline-project", "apply", "--id", "example")
	if code == 0 || !strings.Contains(out["error"].(string), "exceeds") {
		t.Fatalf("limit: %+v (%d)", out, code)
	}
}
