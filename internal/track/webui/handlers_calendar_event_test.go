package webui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/calendarevent"
)

const calendarEventID = "20000000-0000-4000-8000-000000000001"
const calendarOperation = "20000000-0000-4000-8000-000000000002"

func TestCalendarEventAPI(t *testing.T) {
	workspace, server, main, work := twoVaultWorkspace(t)
	changes := workspace.events.subscribe()
	t.Cleanup(func() { workspace.events.unsubscribe(changes) })
	before, err := os.ReadFile(filepath.Join(main, "note", "100.md"))
	if err != nil {
		t.Fatal(err)
	}
	fields := calendarevent.Fields{Title: "Earnings review", AllDay: true, Start: "2026-10-29", End: "2026-11-01", NoteID: "100", Status: "confirmed"}
	req := calendarevent.WriteRequest{Fields: fields, ExpectedRevision: 0, OperationID: calendarOperation}
	do := func(method, path string, value any) (int, map[string]any) {
		t.Helper()
		var body io.Reader
		if value != nil {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(raw)
		}
		r, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, out
	}
	path := "/api/calendar-event?id=" + calendarEventID
	status, out := do(http.MethodPut, path, req)
	if status != 200 || out["event"].(map[string]any)["revision"] != float64(1) {
		t.Fatalf("%d %v", status, out)
	}
	select {
	case change := <-changes:
		if change.name != "change" {
			t.Fatalf("unexpected notification %s", change.name)
		}
	default:
		t.Fatal("calendar write did not notify clients")
	}
	if _, ok := out["event"].(map[string]any)["last_operation_id"]; ok {
		t.Fatal("operation metadata exposed")
	}
	if status, out = do(http.MethodPut, path, req); status != 200 || out["event"].(map[string]any)["revision"] != float64(1) {
		t.Fatalf("retry %d %v", status, out)
	}
	req.Title = "changed with same operation"
	if status, out = do(http.MethodPut, path, req); status != 409 {
		t.Fatalf("%d %v", status, out)
	}
	req.Title = fields.Title
	req.OperationID = "20000000-0000-4000-8000-000000000003"
	if status, out = do(http.MethodPut, path, req); status != 409 {
		t.Fatalf("stale %d %v", status, out)
	}
	if status, out = do(http.MethodGet, "/api/calendar-events?vault=work", nil); status != 200 || len(out["events"].([]any)) != 0 {
		t.Fatalf("foreign vault %d %v", status, out)
	}
	if _, err := os.Stat(filepath.Join(work, "calendar")); !os.IsNotExist(err) {
		t.Fatal("event leaked into other vault")
	}
	if status, _ = do(http.MethodGet, path+"&vault=work", nil); status != 404 {
		t.Fatalf("foreign id %d", status)
	}
	if status, _ = do(http.MethodGet, path+"&vault=missing", nil); status != 400 {
		t.Fatalf("unknown vault %d", status)
	}
	req.ExpectedRevision = 1
	req.Deleted = true
	if status, out = do(http.MethodPut, path, req); status != 200 || out["event"].(map[string]any)["revision"] != float64(2) {
		t.Fatalf("delete %d %v", status, out)
	}
	if status, out = do(http.MethodGet, "/api/calendar-events", nil); status != 200 || len(out["events"].([]any)) != 0 {
		t.Fatalf("trash in normal list %d %v", status, out)
	}
	if status, out = do(http.MethodGet, "/api/calendar-events?deleted=1", nil); status != 200 || len(out["events"].([]any)) != 1 {
		t.Fatalf("trash missing %d %v", status, out)
	}
	req.ExpectedRevision = 2
	req.Deleted = false
	req.OperationID = "20000000-0000-4000-8000-000000000004"
	if status, out = do(http.MethodPut, path, req); status != 200 || out["event"].(map[string]any)["revision"] != float64(3) {
		t.Fatalf("restore %d %v", status, out)
	}
	if status, out = do(http.MethodGet, path, nil); status != 200 || out["event"].(map[string]any)["title"] != fields.Title {
		t.Fatalf("reopen %d %v", status, out)
	}
	after, err := os.ReadFile(filepath.Join(main, "note", "100.md"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("note was changed: %v", err)
	}
}

func TestCalendarEventAPIBoundaries(t *testing.T) {
	_, server, main, _ := twoVaultWorkspace(t)
	valid := `{"title":"Review","all_day":true,"start":"2026-10-29","end":"2026-10-30","expected_revision":0,"operation_id":"` + calendarOperation + `"}`
	for _, tc := range []struct {
		name, method, path, body, host, origin string
		want                                   int
	}{
		{"foreign host", http.MethodGet, "/api/calendar-events", "", "evil.example", "", 403},
		{"foreign origin", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, valid, "", "https://evil.example", 403},
		{"null origin", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, valid, "", "null", 403},
		{"list method", http.MethodPost, "/api/calendar-events", "{}", "", "", 405},
		{"event method", http.MethodDelete, "/api/calendar-event?id=" + calendarEventID, "{}", "", "", 405},
		{"bad id", http.MethodGet, "/api/calendar-event?id=../../other", "", "", "", 400},
		{"bad filter", http.MethodGet, "/api/calendar-events?deleted=maybe", "", "", "", 400},
		{"bad JSON", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, "{", "", "", 400},
		{"unknown field", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, strings.TrimSuffix(valid, "}") + `,"external_invites":["someone"]}`, "", "", 400},
		{"trailing JSON", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, valid + "{}", "", "", 400},
		{"null payload", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, "null", "", "", 400},
		{"overlarge payload", http.MethodPut, "/api/calendar-event?id=" + calendarEventID, `{"description":"` + strings.Repeat("a", calendarevent.MaxRequestBytes) + `"}`, "", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.host != "" {
				r.Host = tc.host
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			resp, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("got %d want %d: %s", resp.StatusCode, tc.want, raw)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(main, "calendar")); !os.IsNotExist(err) {
		t.Fatal("refused requests wrote event storage")
	}
}

func TestCalendarEventAPICorruptionIsAnError(t *testing.T) {
	_, server, main, _ := twoVaultWorkspace(t)
	dir := filepath.Join(main, "calendar", "events")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, calendarEventID+".json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/calendar-events", "/api/calendar-event?id=" + calendarEventID} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "corrupt calendar event") {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
}
