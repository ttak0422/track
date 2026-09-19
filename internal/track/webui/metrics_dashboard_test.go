package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ttak0422/track/internal/track/metrics"
)

func TestMetricsDashboardEndpoint(t *testing.T) {
	server, cfg := putNoteSetup(t, 100, "Dashboard", "body\n")
	if err := os.MkdirAll(cfg.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir(), "cpu.jsonl"), []byte("{\"name\":\"cpu\",\"entity\":\"web1\",\"time\":\"2026-09-19\",\"value\":42}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	spec := `{"title":"Services","panels":[{"type":"stat","title":"CPU","datasource":{"type":"track","uid":"cpu.jsonl"},"targets":[{"expr":"cpu"}]}]}`
	for _, tc := range []struct {
		spec, entity, from string
		status, count      int
	}{
		{spec, "web1", "", http.StatusOK, 1},
		{spec, "other", "", http.StatusOK, 0},
		{spec, "", "bad-date", http.StatusBadRequest, 0},
		{`{"title":"Bad","panels":[{"type":"heatmap"}]}`, "", "", http.StatusBadRequest, 0},
	} {
		raw, _ := json.Marshal(map[string]string{"spec": tc.spec, "entity": tc.entity, "from": tc.from})
		resp, err := http.Post(server.URL+"/api/metrics/dashboard", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status {
			t.Fatalf("status=%d want=%d", resp.StatusCode, tc.status)
		}
		if tc.status == http.StatusOK {
			var d metrics.Dashboard
			if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
				t.Fatal(err)
			}
			if len(d.Panels) != 1 || len(d.Panels[0].Values) != tc.count || len(d.Entities) != 1 {
				t.Fatalf("wrong dashboard scope: %+v", d)
			}
		}
		resp.Body.Close()
	}
	resp, err := http.Get(server.URL + "/api/metrics/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET accepted: %d", resp.StatusCode)
	}
}
