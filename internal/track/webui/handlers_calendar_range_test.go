package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ttak0422/track/internal/track/calendarevent"
	"github.com/ttak0422/track/internal/track/config"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCalendarRangeAPI(t *testing.T) {
	_, server, _, work := twoVaultWorkspace(t)
	for _, tc := range []struct {
		method, query string
		status        int
	}{
		{"GET", "?since=2024-02-29&until=2024-02-29&time_zone=UTC&vault=work", 200},
		{"POST", "?since=2024-02-29&until=2024-02-29&time_zone=UTC", 405},
		{"HEAD", "?since=2024-02-29&until=2024-02-29&time_zone=UTC", 405},
		{"GET", "?since=2024-02-29&until=2024-02-29&time_zone=UTC&vault=missing", 400},
		{"GET", "?since=2024-02-29&until=2024-02-29&time_zone=UTC&deleted=", 400},
		{"GET", "?since=2024-02-29&until=2024-02-29&time_zone=UTC&deleted=0", 400},
		{"GET", "?since=2024-02-29&until=2024-02-29&time_zone=UTC&deleted=1", 400},
		{"GET", "?since=2024-02-29", 400},
		{"GET", "?time_zone=UTC", 400},
		{"GET", "?since=2024-02-29&until=2024-02-29&time_zone=Asia%2FTokyo&vault=work", 200},
		{"GET", "?since=2024-02-29%20&until=2024-02-29&time_zone=UTC", 400},
	} {
		req, err := http.NewRequest(tc.method, server.URL+"/api/calendar-events/range"+tc.query, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.query, resp.StatusCode, raw)
		}
		if tc.status == 200 {
			var out struct {
				Vault, Since, Until string
				TimeZone            string `json:"time_zone"`
				Days                []struct {
					Date     string
					Count    int
					Previews []any
				}
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if out.Vault != "work" || out.Since != "2024-02-29" || out.Until != "2024-02-29" || len(out.Days) != 1 || out.Days[0].Date != "2024-02-29" || out.Days[0].Count != 0 || out.Days[0].Previews == nil {
				t.Fatalf("shape: %s", raw)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(work, "calendar")); !os.IsNotExist(err) {
		t.Fatal("projection created event storage")
	}
	dir := filepath.Join(work, "calendar", "events")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, calendarEventID+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Get(server.URL + "/api/calendar-events/range?since=2024-02-29&until=2024-02-29&time_zone=UTC&vault=work")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("corrupt projection: %d", resp.StatusCode)
	}
}

func TestCalendarRangeUnavailableAndRetiredStorage(t *testing.T) {
	target := "/api/calendar-events/range?since=2024-02-29&until=2024-02-29&time_zone=UTC"
	b := newBootstrap(context.Background(), nil, nil, nil)
	for _, phase := range []string{"initializing", "unavailable"} {
		b.status.Phase = phase
		if w := bootRequest(b, "GET", target, ""); w.Code != 503 {
			t.Fatalf("%s: %d %s", phase, w.Code, w.Body.String())
		}
	}
	boundary := newReadBoundary()
	good := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"date":"2024-02-29","time_zone":"UTC","events":[]}`))
	})
	if w := boundaryRequest(boundary, "http://localhost"+target, good); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := boundaryRequest(boundary, "http://localhost"+target, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "failed load", 500) }); w.Code != 500 {
		t.Fatalf("failure returned cached projection: %d", w.Code)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan int, 1)
	go func() {
		w := boundaryRequest(boundary, "http://localhost"+target, func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; good(w, r) })
		finished <- w.Code
	}()
	<-entered
	boundary.advance(2)
	close(release)
	if code := <-finished; code != 503 {
		t.Fatalf("retired projection: %d", code)
	}
	if len(boundary.cache) != 0 {
		t.Fatal("projection was cached")
	}
}

func TestCalendarRangeBoundedWire(t *testing.T) {
	_, server, _, work := twoVaultWorkspace(t)
	cfg := &config.Config{VaultDir: work}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("20000000-0000-4000-8000-%012d", i+10)
		fields := calendarevent.Fields{Title: strings.Repeat("<", 500), AllDay: true, Start: "2024-02-01", End: "2024-03-14", Status: "cancelled", Description: "private body", URL: "https://example.com/private"}
		if _, err := calendarevent.Save(cfg, id, calendarevent.WriteRequest{Fields: fields, OperationID: calendarOperation}); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := server.Client().Get(server.URL + "/api/calendar-events/range?since=2024-02-01&until=2024-03-13&time_zone=UTC&vault=work")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || len(raw) > 512*1024 {
		t.Fatalf("%d %d %v", resp.StatusCode, len(raw), err)
	}
	var out struct {
		Days []struct {
			Count    int
			Previews []map[string]any
		}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Days) != 42 {
		t.Fatal(len(out.Days))
	}
	for _, day := range out.Days {
		if day.Count != 4 || len(day.Previews) != 3 {
			t.Fatal(day)
		}
		for _, p := range day.Previews {
			if len(p) != 4 || p["title"] != strings.Repeat("<", 500) || p["status"] != "cancelled" || p["id"] == nil || p["revision"] != float64(1) {
				t.Fatal(p)
			}
		}
	}
	t.Logf("42 days, 126 maximally escaped titles: %d wire bytes", len(raw))
}

func TestCalendarRangeDoesNotInitializeVaults(t *testing.T) {
	workspace, server, main, work := twoVaultWorkspace(t)
	// Neither launch nor foreign notes have been indexed yet.
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, root := range []string{main, work, filepath.Dir(filepath.Dir(workspace.active.cfg.DBPath))} {
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				value := info.ModTime().String()
				if !d.IsDir() {
					raw, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					value += string(raw)
				}
				out[path] = value
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	before := snapshot()
	target := "/api/calendar-events/range?since=2024-02-29&until=2024-02-29&time_zone=UTC"
	for _, tc := range []struct{ query, label string }{{"", ""}, {"&vault=main", ""}, {"&vault=work", "work"}} {
		got := getVaultJSON(t, server.URL+target+tc.query)
		if got["vault"] != tc.label {
			t.Fatalf("%s label: %v", tc.query, got["vault"])
		}
	}
	if _, ok := workspace.cachedView("work"); ok {
		t.Fatal("range opened an indexed foreign view")
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("range changed vault/index files or directories")
	}
	for _, tc := range []struct{ host, origin string }{{"evil.example", ""}, {"", "https://evil.example"}} {
		req, _ := http.NewRequest("GET", server.URL+target, nil)
		if tc.host != "" {
			req.Host = tc.host
		}
		req.Header.Set("Origin", tc.origin)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatal(resp.StatusCode)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "http://localhost"+target+"&vault=work", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	workspace.Handler().ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("cancelled request succeeded")
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("failed range changed vault/index files")
	}
}

func TestCalendarRangeRegisteredStorageFailures(t *testing.T) {
	for _, kind := range []string{"missing", "nonregular", "event-symlink"} {
		t.Run(kind, func(t *testing.T) {
			workspace, server, _, work := twoVaultWorkspace(t)
			if kind == "missing" {
				workspace.active.cfg.Vaults["work"] = filepath.Join(t.TempDir(), "missing")
			}
			if kind == "nonregular" {
				path := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(path, []byte("not a vault"), 0600); err != nil {
					t.Fatal(err)
				}
				workspace.active.cfg.Vaults["work"] = path
			}
			if kind == "event-symlink" {
				if err := os.Mkdir(filepath.Join(work, "calendar"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(work, "calendar", "events")); err != nil {
					t.Fatal(err)
				}
			}
			resp, err := server.Client().Get(server.URL + "/api/calendar-events/range?since=2024-02-29&until=2024-02-29&time_zone=UTC&vault=work")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			want := 400
			if kind == "event-symlink" {
				want = 500
			}
			if resp.StatusCode != want {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("%d want%d %s", resp.StatusCode, want, raw)
			}
			if _, ok := workspace.cachedView("work"); ok {
				t.Fatal("failed range opened an index")
			}
		})
	}
}

func TestCalendarRangeIgnoresProvisionalPeerIndex(t *testing.T) {
	workspace, _, _, work := twoVaultWorkspace(t)
	cfg, err := config.LoadAt(work)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Provisional = true
	peer := &vaultView{name: "work", label: "work", cfg: cfg, async: true}
	peer.progress.state.Partial = true
	workspace.views["work"] = peer
	t.Cleanup(func() { delete(workspace.views, "work") })
	workspace.active.progress.verified = true
	b := newBootstrap(context.Background(), nil, nil, nil)
	b.server = workspace
	b.cfg = workspace.cfg
	b.status = StorageStatus{Phase: "ready"}
	target := "/api/calendar-events/range?since=2024-02-29&until=2024-02-29&time_zone=UTC&vault=work"
	w := bootRequest(b, "GET", target, "")
	if w.Code != 200 {
		t.Fatalf("provisional peer index blocked verified event files: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Vault string
		Days  []any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Vault != "work" || len(got.Days) != 1 {
		t.Fatal(got)
	}
	if workspace.views["work"] != peer || !peer.cfg.Provisional || !peer.progress.state.Partial {
		t.Fatal("range mutated peer index state")
	}
	if w := bootRequest(b, "GET", "/api/calendar-events/day?date=2024-02-29&time_zone=UTC&vault=work", ""); w.Code != 503 {
		t.Fatalf("legacy day readiness changed: %d", w.Code)
	}
	b.status = StorageStatus{Phase: "initializing", ReadOnly: true}
	workspace.cfg.Provisional = true
	if w := bootRequest(b, "GET", target, ""); w.Code != 503 {
		t.Fatalf("global readiness bypassed: %d", w.Code)
	}
	b.status = StorageStatus{Phase: "ready"}
	workspace.cfg.Provisional = false
	workspace.active.cfg.Vaults["work"] = filepath.Join(t.TempDir(), "missing")
	if w := bootRequest(b, "GET", target, ""); w.Code != 400 {
		t.Fatalf("provisional peer hid missing registered storage: %d %s", w.Code, w.Body.String())
	}
}
