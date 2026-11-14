package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCalendarDayAPI(t *testing.T) {
	_, server, _, work := twoVaultWorkspace(t)
	for _, tc := range []struct {
		method, query string
		status        int
	}{
		{"GET", "?date=2024-02-29&time_zone=UTC&vault=work", 200},
		{"POST", "?date=2024-02-29&time_zone=UTC", 405},
		{"HEAD", "?date=2024-02-29&time_zone=UTC", 405},
		{"GET", "?date=2024-02-29&time_zone=UTC&vault=missing", 400},
		{"GET", "?date=2024-02-29&time_zone=UTC&deleted=", 400},
		{"GET", "?date=2024-02-29&time_zone=UTC&deleted=0", 400},
		{"GET", "?date=2024-02-29&time_zone=UTC&deleted=1", 400},
		{"GET", "?date=2024-02-29", 400},
		{"GET", "?time_zone=UTC", 400},
		{"GET", "?date=2024-02-29&time_zone=Asia%2FTokyo", 400},
		{"GET", "?date=2024-02-29%20&time_zone=UTC", 400},
	} {
		req, err := http.NewRequest(tc.method, server.URL+"/api/calendar-events/day"+tc.query, nil)
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
				Vault, Date string
				Zone        string `json:"time_zone"`
				Start       string `json:"day_start"`
				End         string `json:"day_end"`
				Events      []any
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if out.Vault != "work" || out.Date != "2024-02-29" || out.Zone != "UTC" || out.Start != "2024-02-29T00:00:00Z" || out.End != "2024-03-01T00:00:00Z" || out.Events == nil || len(out.Events) != 0 {
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
	resp, err := server.Client().Get(server.URL + "/api/calendar-events/day?date=2024-02-29&time_zone=UTC&vault=work")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("corrupt projection: %d", resp.StatusCode)
	}
}

func TestCalendarDayUnavailableAndRetiredStorage(t *testing.T) {
	target := "/api/calendar-events/day?date=2024-02-29&time_zone=UTC"
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
