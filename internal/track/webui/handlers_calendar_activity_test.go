package webui

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ttak0422/track/internal/track/note"
	"github.com/ttak0422/track/internal/track/store"
	"github.com/ttak0422/track/internal/track/task"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const activityTarget = "/api/calendar-activity/range?since=2024-02-29&until=2024-02-29&time_zone=UTC"

func markActivityReady(t *testing.T, v *vaultView) {
	t.Helper()
	if err := v.store.SetIndexedLinkFormat(""); err != nil {
		t.Fatal(err)
	}
	if err := v.store.SetReadState(store.ReadState{CheckedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
}
func TestCalendarActivityHTTPReadinessAndScope(t *testing.T) {
	workspace, server, main, work := twoVaultWorkspace(t)
	do := func(method, target string, headers map[string]string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+target, nil)
		for k, v := range headers {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, raw
	}
	for _, target := range []string{activityTarget, activityTarget + "&vault=work"} {
		if code, _ := do("GET", target, nil); code != 503 {
			t.Fatal(code)
		}
	}
	if _, ok := workspace.cachedView("work"); ok {
		t.Fatal("cold read opened peer")
	}
	markActivityReady(t, workspace.active)
	for _, tc := range []struct {
		method, target string
		want           int
	}{
		{"POST", activityTarget, 405}, {"HEAD", activityTarget, 405}, {"GET", activityTarget + "&vault=missing", 400},
		{"GET", "/api/calendar-activity/range?since=2024-02-01&until=2024-03-14&time_zone=UTC", 400},
		{"GET", "/api/calendar-activity/range?since=2024-02-29&until=2024-02-29&time_zone=Local", 400},
	} {
		if code, raw := do(tc.method, tc.target, nil); code != tc.want {
			t.Fatalf("%d %s", code, raw)
		}
	}
	for _, h := range []map[string]string{{"Host": "evil.example"}, {"Origin": "https://evil.example"}} {
		if code, _ := do("GET", activityTarget, h); code != 403 {
			t.Fatal(code)
		}
	}
	foreign, err := workspace.viewByName("work")
	if err != nil {
		t.Fatal(err)
	}
	markActivityReady(t, foreign)
	if err := foreign.store.UpsertNote(&note.Note{ID: 100, Meta: note.Metadata{Title: "foreign", Days: []string{"2024-02-29"}}}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, root := range []string{main, work, filepath.Dir(filepath.Dir(workspace.cfg.DBPath))} {
			if err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				info, e := d.Info()
				if e != nil {
					return e
				}
				v := info.ModTime().String()
				if !d.IsDir() {
					raw, e := os.ReadFile(p)
					if e != nil {
						return e
					}
					// SQLite read snapshots update existing WAL shared-memory reader marks, not index data.
					if strings.HasSuffix(p, ".db-shm") {
						v = "existing SQLite reader coordination file"
					} else {
						v += string(raw)
					}
				}
				out[p] = v
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	before := snapshot()
	for _, tc := range []struct {
		query, label string
		count        int
	}{{"", "", 0}, {"&vault=main", "", 0}, {"&vault=work", "work", 1}} {
		code, raw := do("GET", activityTarget+tc.query, nil)
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		var got struct {
			Vault, Since, Until, Today string
			Zone                       string `json:"time_zone"`
			Days                       []store.CalendarActivityDay
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Vault != tc.label || got.Zone != "UTC" || got.Today != time.Now().UTC().Format(time.DateOnly) || len(got.Days) != 1 || got.Days[0].NoteCount != tc.count || got.Days[0].Notes == nil || got.Days[0].Tasks == nil {
			t.Fatalf("%s", raw)
		}
	}
	if !reflect.DeepEqual(before, snapshot()) {
		for path, old := range before {
			if snapshot()[path] != old {
				t.Logf("changed: %s", path)
			}
		}
		t.Fatal("summary mutated vault or index files")
	}
	foreign.cfg.Provisional = true
	if code, _ := do("GET", activityTarget+"&vault=work", nil); code != 503 {
		t.Fatal(code)
	}
	foreign.cfg.Provisional = false
	foreign.refreshErr = errors.New("failed refresh")
	if code, _ := do("GET", activityTarget+"&vault=work", nil); code != 503 {
		t.Fatal(code)
	}
	foreign.refreshErr = nil
	foreign.async = true
	foreign.progress.verified = true
	foreign.progress.state = store.ReadState{CheckedAt: 1, Partial: true}
	if code, _ := do("GET", activityTarget+"&vault=work", nil); code != 503 {
		t.Fatal(code)
	}
	foreign.progress.state.Partial = false
	if code, raw := do("GET", activityTarget+"&vault=work", nil); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "http://localhost"+activityTarget, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	workspace.Handler().ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	workspace.closed = true
	if code, _ := do("GET", activityTarget, nil); code != 503 {
		t.Fatal(code)
	}
	workspace.closed = false
}
func TestCalendarActivityBoundedWire(t *testing.T) {
	workspace, server, _, _ := twoVaultWorkspace(t)
	markActivityReady(t, workspace.active)
	days := []string{}
	first, _ := time.Parse(time.DateOnly, "2024-02-01")
	for i := 0; i < 42; i++ {
		days = append(days, first.AddDate(0, 0, i).Format(time.DateOnly))
	}
	for id := int64(1); id <= 4; id++ {
		tasks := []task.Task{}
		for i, date := range days {
			tasks = append(tasks, task.Task{Line: i + 1, State: "DONE", Done: true, Scheduled: date, Due: date, Text: strings.Repeat("<", 1000)})
		}
		if err := workspace.store.UpsertNote(&note.Note{ID: id, Meta: note.Metadata{Title: strings.Repeat("<", 1000), Days: days}, Tasks: tasks}); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := server.Client().Get(server.URL + "/api/calendar-activity/range?since=2024-02-01&until=2024-03-13&time_zone=Asia%2FTokyo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || len(raw) > 512<<10 {
		t.Fatal(resp.StatusCode, len(raw), err, string(raw))
	}
	var got struct {
		Today string
		Days  []store.CalendarActivityDay
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Tokyo")
	if got.Today != time.Now().In(loc).Format(time.DateOnly) || len(got.Days) != 42 {
		t.Fatal(got.Today, len(got.Days))
	}
	for _, d := range got.Days {
		if d.TaskCount != 4 || d.NoteCount != 4 || len(d.Tasks) != 3 || len(d.Notes) != 3 {
			t.Fatal(d)
		}
		for _, p := range d.Tasks {
			if !p.TextTruncated || len(p.Text) > 256 || !utf8.ValidString(p.Text) {
				t.Fatal(p)
			}
		}
		for _, p := range d.Notes {
			if !p.TitleTruncated || len(p.Title) > 256 || !utf8.ValidString(p.Title) {
				t.Fatal(p)
			}
		}
	}
	t.Logf("42-day max escaped previews: %d bytes", len(raw))
}
func TestCalendarActivityNoFallback(t *testing.T) {
	b := newReadBoundary()
	target := "http://localhost" + activityTarget
	if w := boundaryRequest(b, target, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"days":[]}`)) }); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := boundaryRequest(b, target, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if len(b.cache) != 0 {
		t.Fatal("activity summary cached")
	}
}
