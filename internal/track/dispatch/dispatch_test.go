package dispatch

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidStatusClosedSet(t *testing.T) {
	for _, s := range Statuses() {
		if !ValidStatus(s) {
			t.Fatalf("ValidStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []Status{"", "ready", "blocked", "circuit_broken", "DISPATCHED"} {
		if ValidStatus(s) {
			t.Fatalf("ValidStatus(%q) = true, want false", s)
		}
	}
}

func TestRecordYAMLRoundTrip(t *testing.T) {
	in := Record{
		At:              "2026-09-06T10:00:00Z",
		ID:              "d-1000-1",
		Note:            1000,
		Status:          StatusDispatched,
		FailureCount:    1,
		DispatchedAt:    "2026-09-06T10:00:00Z",
		CompletedAt:     "2026-09-06T10:07:00Z",
		LastHeartbeatAt: "2026-09-06T10:05:00Z",
	}
	out, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	for _, key := range []string{"at:", "id:", "note:", "status:", "failure_count:", "dispatched_at:", "completed_at:", "last_heartbeat_at:"} {
		if !strings.Contains(string(out), key) {
			t.Fatalf("marshaled record missing %q:\n%s", key, out)
		}
	}
	var got Record
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("record mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestRecordOmitsEmptyState(t *testing.T) {
	in := Record{ID: "d-1000-1", Note: 1000, Status: StatusPending}
	out, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	for _, absent := range []string{"failure_count", "dispatched_at", "completed_at", "last_heartbeat_at"} {
		if strings.Contains(string(out), absent) {
			t.Fatalf("a zero %s must be omitted from the sidecar:\n%s", absent, out)
		}
	}
}
