package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentTimelineAPI(t *testing.T) {
	workspace, server, main, work := twoVaultWorkspace(t)
	changes := workspace.events.subscribe()
	defer workspace.events.unsubscribe(changes)
	do := func(method, path, body, origin string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, out
	}
	path := "/api/timeline/project?id=example&vault=work"
	create := `{"source":"api-test","event_id":"create","expected_revision":0,"kind":"project.create","payload":{"title":"Agent work","note_id":100,"mode":"serial"}}`
	status, got := do("POST", path, create, "")
	if status != 200 || got["revision"] != float64(1) || got["vault"] != "work" {
		t.Fatalf("create: %d %+v", status, got)
	}
	if _, err := os.Stat(filepath.Join(work, ".track", "timelines", "example.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(main, ".track", "timelines", "example.json")); !os.IsNotExist(err) {
		t.Fatalf("write crossed vault: %v", err)
	}
	select {
	case <-changes:
	default:
		t.Fatal("no change notification")
	}
	status, got = do("POST", path, create, "")
	if status != 200 || got["revision"] != float64(1) {
		t.Fatalf("retry: %d %+v", status, got)
	}
	status, got = do("GET", path, "", "")
	if status != 200 || got["title"] != "Agent work" {
		t.Fatalf("read: %d %+v", status, got)
	}
	status, got = do("GET", "/api/timeline/projects?vault=work", "", "")
	if status != 200 || len(got["projects"].([]any)) != 1 {
		t.Fatalf("list: %d %+v", status, got)
	}
	stale := strings.ReplaceAll(strings.ReplaceAll(create, `"create"`, `"changed"`), `"project.create"`, `"project.update"`)
	if status, _ := do("POST", path, stale, ""); status != 409 {
		t.Fatalf("stale status %d", status)
	}
	mismatched := strings.ReplaceAll(create, "Agent work", "Different payload")
	if status, _ := do("POST", path, mismatched, ""); status != 409 {
		t.Fatalf("duplicate mismatch status %d", status)
	}
	for _, tc := range []struct {
		method, path, body, origin string
		status                     int
	}{
		{"GET", "/api/timeline/project", "", "", 400},
		{"GET", "/api/timeline/project?id=../bad", "", "", 400},
		{"GET", "/api/timeline/project?id=missing", "", "", 404},
		{"GET", "/api/timeline/project?id=example&vault=missing", "", "", 400},
		{"DELETE", path, "", "", 405},
		{"POST", "/api/timeline/projects", "{}", "", 405},
		{"POST", path, create, "https://untrusted.example", 403},
		{"GET", path, "", "https://untrusted.example", 403},
		{"POST", path, "null", "", 400},
		{"POST", path, `{"expected_revision":null}`, "", 400},
		{"POST", path, `{"expected_revision":1,"extra":true}`, "", 400},
		{"POST", path, create + " {}", "", 400},
		{"POST", path, strings.Repeat(" ", 1<<20+1), "", 400},
	} {
		if status, out := do(tc.method, tc.path, tc.body, tc.origin); status != tc.status {
			t.Fatalf("%s %s: %d, want %d: %+v", tc.method, tc.path, status, tc.status, out)
		}
	}
}

func TestAgentTimelineAPIRejectsBrowserFetchMetadata(t *testing.T) {
	server, _ := putNoteSetup(t, 900, "Plan", "")
	req, err := http.NewRequest("GET", server.URL+"/api/timeline/projects", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
