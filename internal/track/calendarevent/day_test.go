package calendarevent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

func TestDayBoundaries(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	for date, end := range map[string]string{"2024-02-29": "2024-03-01", "2026-12-31": "2027-01-01", "0001-01-01": "0001-01-02", "9998-12-31": "9999-01-01"} {
		day, err := Day(cfg, date, "UTC")
		if err != nil || day.Date != date || day.TimeZone != "UTC" || day.DayStart != date+"T00:00:00Z" || day.DayEnd != end+"T00:00:00Z" || day.Events == nil || len(day.Events) != 0 {
			t.Fatalf("%s: %+v %v", date, day, err)
		}
	}
	for _, date := range []string{"", "2026-02-29", "2024-2-29", "2024-02-29 ", " 2024-02-29", "0000-01-01", "9999-01-01", "２０２４-02-29"} {
		if _, err := Day(cfg, date, "UTC"); !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted %q: %v", date, err)
		}
	}
	for _, zone := range []string{"", "utc", "UTC ", "Local", "+09:00", "Mars/Test", strings.Repeat("X", 129)} {
		if _, err := Day(cfg, "2024-02-29", zone); !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted %q: %v", zone, err)
		}
	}
	entries, err := os.ReadDir(cfg.VaultDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read created storage: %v %v", entries, err)
	}
}

func TestDayMembershipAndPreservation(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	for i, tc := range []struct {
		name, start, end, zone, date string
		allDay, want                 bool
	}{
		{"ends at midnight", "2026-11-01T23:00:00Z", "2026-11-02T00:00:00Z", "UTC", "2026-11-02", false, false},
		{"starts next midnight", "2026-11-03T00:00:00Z", "2026-11-03T01:00:00Z", "UTC", "2026-11-02", false, false},
		{"cross midnight", "2026-11-01T23:00:00Z", "2026-11-02T00:00:00.000000001Z", "UTC", "2026-11-02", false, true},
		{"fraction before end", "2026-11-02T23:59:59.999999999Z", "2026-11-03T01:00:00Z", "UTC", "2026-11-02", false, true},
		{"offset changes date", "2026-11-01T20:00:00-05:00", "2026-11-01T21:00:00-05:00", "America/New_York", "2026-11-02", false, true},
		{"spring offset", "2026-03-08T01:30:00-05:00", "2026-03-08T03:30:00-04:00", "America/New_York", "2026-03-08", false, true},
		{"fall offset", "2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00", "America/New_York", "2026-11-01", false, true},
		{"multi day", "2026-11-01T23:00:00+09:00", "2026-11-04T01:00:00+09:00", "Asia/Tokyo", "2026-11-02", false, true},
		{"civil start", "2026-11-02", "2026-11-04", "", "2026-11-02", true, true},
		{"civil end", "2026-11-01", "2026-11-02", "", "2026-11-02", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := Fields{Title: tc.name, Start: tc.start, End: tc.end, TimeZone: tc.zone, AllDay: tc.allDay, Status: "cancelled"}
			event, err := Save(cfg, op(i+10), request(fields, 0, op(i+100)))
			if err != nil {
				t.Fatal(err)
			}
			day, err := Day(cfg, tc.date, "UTC")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, got := range day.Events {
				if got.ID == event.ID {
					found = true
					if !reflect.DeepEqual(got, event) {
						t.Fatalf("labels changed: %+v vs %+v", got, event)
					}
				}
			}
			if found != tc.want {
				t.Fatalf("found %v want %v", found, tc.want)
			}
		})
	}
	// Engine order is retained, and deleted records never surface.
	before, err := List(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	day, err := Day(cfg, "2026-11-02", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	var ordered []Event
	for _, event := range before {
		for _, match := range day.Events {
			if event.ID == match.ID {
				ordered = append(ordered, event)
			}
		}
	}
	if !reflect.DeepEqual(ordered, day.Events) {
		t.Fatal("order changed")
	}
	target := day.Events[0]
	target.Deleted = true
	if _, err := Save(cfg, target.ID, request(target.Fields, target.Revision, op(999))); err != nil {
		t.Fatal(err)
	}
	day, err = Day(cfg, "2026-11-02", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range day.Events {
		if event.Deleted || event.ID == target.ID {
			t.Fatal("trash included")
		}
	}
	// Snapshot content and modification times, including lock file, must not change.
	type snapshot struct {
		Data     string
		Modified time.Time
	}
	snapshotFiles := func() map[string]snapshot {
		out := map[string]snapshot{}
		err := filepath.WalkDir(cfg.VaultDir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			data := []byte{}
			if !entry.IsDir() {
				data, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			out[path] = snapshot{string(data), info.ModTime()}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	old := snapshotFiles()
	if _, err := Day(cfg, "2026-11-02", "UTC"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, snapshotFiles()) {
		t.Fatal("projection changed files")
	}
}

func TestDayReadFailuresAndLimit(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	event, err := Save(cfg, testID, request(allDay(), 0, testOp))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.VaultDir, "calendar", "events")
	path := filepath.Join(dir, testID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), event.Start, "bad-date", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Day(cfg, "2026-10-29", "UTC"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt read: %v", err)
	}
	// Construct valid isolated records without thousands of fsyncs.
	var saved record
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxDaySourceEvents+1; i++ {
		saved.ID = op(i + 10000)
		data, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, saved.ID+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Day(cfg, "2026-10-29", "UTC"); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("limit: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, saved.ID+".json")); err != nil {
		t.Fatal(err)
	}
	day, err := Day(cfg, "2026-10-29", "UTC")
	if err != nil || len(day.Events) != MaxDaySourceEvents {
		t.Fatalf("exact limit: %d %v", len(day.Events), err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Day(cfg, "2026-10-29", "UTC"); err == nil {
		t.Fatal("read failure became empty success")
	}
}

func TestDayMissingVaultAndAnchoredStorage(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	cfg := &config.Config{VaultDir: vault}
	if err := os.Mkdir(vault, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Day(cfg, "2026-10-29", "UTC"); err != nil {
		t.Fatalf("absent calendar should be empty: %v", err)
	}
	if err := os.Remove(vault); err != nil {
		t.Fatal(err)
	}
	if _, err := list(cfg, false, MaxDaySourceEvents); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bounded list hid missing vault: %v", err)
	}
	if _, err := Day(cfg, "2026-10-29", "UTC"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("day hid missing vault: %v", err)
	}
	if events, err := List(cfg, false); err != nil || len(events) != 0 {
		t.Fatalf("legacy list changed: %v %v", events, err)
	}
	if err := os.Mkdir(vault, 0755); err != nil {
		t.Fatal(err)
	}
	saved, err := Save(cfg, testID, request(allDay(), 0, testOp))
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := os.OpenRoot(vault)
	if err != nil {
		t.Fatal(err)
	}
	// Disappear the original pathname after opening, exactly at the former
	// preflight/reopen boundary. The traversal must still use the original root.
	if err := os.Rename(vault, filepath.Join(parent, "moved")); err != nil {
		anchor.Close()
		t.Fatal(err)
	}
	root, err := eventRootFromVault(anchor, false)
	if err != nil {
		t.Fatalf("reopened missing pathname instead of using anchor: %v", err)
	}
	defer root.Close()
	got, err := read(root, testID)
	if err != nil || !reflect.DeepEqual(got.Event, saved) {
		t.Fatalf("anchored record: %+v %v", got, err)
	}
}

func TestDayNamedZones(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	for _, tc := range []struct{ date, zone, start, end string }{
		{"2026-11-02", "Asia/Tokyo", "2026-11-01T15:00:00Z", "2026-11-02T15:00:00Z"},
		{"2026-03-08", "America/New_York", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"2026-11-01", "America/New_York", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"2018-11-04", "America/Sao_Paulo", "2018-11-04T03:00:00Z", "2018-11-05T02:00:00Z"},
		{"2011-12-29", "Pacific/Apia", "2011-12-29T10:00:00Z", "2011-12-30T10:00:00Z"},
		{"2011-12-31", "Pacific/Apia", "2011-12-30T10:00:00Z", "2011-12-31T10:00:00Z"},
		{"9998-12-31", "America/New_York", "9998-12-31T05:00:00Z", "9999-01-01T05:00:00Z"},
	} {
		t.Run(tc.date+tc.zone, func(t *testing.T) {
			day, err := Day(cfg, tc.date, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			if day.DayStart != tc.start || day.DayEnd != tc.end || day.TimeZone != tc.zone {
				t.Fatalf("%+v", day)
			}
			start, _ := time.Parse(time.RFC3339, day.DayStart)
			end, _ := time.Parse(time.RFC3339, day.DayEnd)
			if start.Unix() != day.DayStartUnix || end.Unix() != day.DayEndUnix {
				t.Fatal("inconsistent unix boundaries")
			}
		})
	}
	for _, tc := range []struct{ date, zone, problem string }{
		{"2011-12-30", "Pacific/Apia", "nonexistent"},
		{"0001-01-01", "Asia/Tokyo", "outside RFC3339"},
	} {
		if _, err := Day(cfg, tc.date, tc.zone); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.problem) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

// fixtureLocation builds deterministic TZif v1 data without consulting host zones.
func fixtureLocation(t *testing.T, offsets []int32, transitions []int32, indices []byte) *time.Location {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("TZif")
	b.Write(make([]byte, 16))
	for _, n := range []int32{0, 0, 0, int32(len(transitions)), int32(len(offsets)), 2} {
		binary.Write(&b, binary.BigEndian, n)
	}
	for _, sec := range transitions {
		binary.Write(&b, binary.BigEndian, sec)
	}
	b.Write(indices)
	for _, offset := range offsets {
		binary.Write(&b, binary.BigEndian, offset)
		b.Write([]byte{0, 0})
	}
	b.Write([]byte{'X', 0})
	loc, err := time.LoadLocationFromTZData("fixture", b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestCivilDayAdversarialTransitions(t *testing.T) {
	civil, _ := time.Parse(time.DateOnly, "2026-11-02")
	for _, tc := range []struct {
		name               string
		offsets            []int32
		at                 int64
		wantStart, wantEnd int64
		disconnected       bool
	}{
		{"repeated midnight", []int32{0, -3600}, 3600, 0, 90000, false},
		{"rollback across date", []int32{0, -7200}, 3600, 0, 0, true},
		{"missing midnight", []int32{0, 3600}, 0, 0, 82800, false},
		{"large legal TZif offset", []int32{360000}, 0, -360000, -273600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var transitions []int32
			var indices []byte
			if len(tc.offsets) > 1 {
				transitions = []int32{int32(civil.Unix() + tc.at)}
				indices = []byte{1}
			}
			loc := fixtureLocation(t, tc.offsets, transitions, indices)
			start, end, err := civilDayInterval(civil, loc)
			if tc.disconnected {
				if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "disconnected") {
					t.Fatalf("%v", err)
				}
			} else if err != nil || start.Unix() != civil.Unix()+tc.wantStart || end.Unix() != civil.Unix()+tc.wantEnd {
				t.Fatalf("%v %v %v", start, end, err)
			}
		})
	}
}

func TestDayNamedMembership(t *testing.T) {
	for i, tc := range []struct {
		start, end   string
		allDay, want bool
	}{
		{"2026-03-08T04:00:00Z", "2026-03-08T05:00:00Z", false, false},
		{"2026-03-09T04:00:00Z", "2026-03-09T05:00:00Z", false, false},
		{"2026-03-08T04:00:00Z", "2026-03-08T05:00:00.000000001Z", false, true},
		{"2026-03-09T03:59:59.999999999Z", "2026-03-09T04:00:00Z", false, true},
		{"2026-03-08", "2026-03-09", true, true},
		{"2026-03-07", "2026-03-08", true, false},
	} {
		cfg := &config.Config{VaultDir: t.TempDir()}
		zone := "UTC"
		if tc.allDay {
			zone = ""
		}
		event, err := Save(cfg, op(i+500), request(Fields{Title: "boundary", Start: tc.start, End: tc.end, TimeZone: zone, AllDay: tc.allDay, Status: "cancelled"}, 0, op(i+600)))
		if err != nil {
			t.Fatal(err)
		}
		day, err := Day(cfg, "2026-03-08", "America/New_York")
		if err != nil {
			t.Fatal(err)
		}
		if (len(day.Events) == 1) != tc.want {
			t.Fatalf("%+v: %+v", tc, day)
		}
		if tc.want && !reflect.DeepEqual(day.Events[0], event) {
			t.Fatal("changed original event")
		}
	}
}

func TestDayCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DayContext(ctx, &config.Config{VaultDir: t.TempDir()}, "2026-03-08", "America/New_York"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func BenchmarkCivilDayInterval(b *testing.B) {
	for _, tc := range []struct{ zone, date string }{
		{"Asia/Tokyo", "2026-11-02"}, {"America/New_York", "2026-11-02"},
		{"America/Sao_Paulo", "2026-11-02"}, {"America/New_York", "9998-12-31"},
	} {
		b.Run(tc.zone+tc.date, func(b *testing.B) {
			civil, _ := time.Parse(time.DateOnly, tc.date)
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := civilDayInterval(civil, loc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestCivilDayTransitionBudget(t *testing.T) {
	civil, _ := time.Parse(time.DateOnly, "2026-11-02")
	transitions := make([]int32, 65537)
	indices := make([]byte, len(transitions))
	for i := range transitions {
		transitions[i] = int32(civil.Unix() - 86400 + int64(i))
		indices[i] = byte(i % 2)
	}
	loc := fixtureLocation(t, []int32{0, 1}, transitions, indices)
	if _, _, err := civilDayInterval(civil, loc); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("budget: %v", err)
	}
}

func TestCivilDayPOSIXLeapYearTail(t *testing.T) {
	civil, _ := time.Parse(time.DateOnly, "2040-12-31")
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start, end, err := civilDayInterval(civil, loc)
	if err != nil || start.Format(time.RFC3339) != "2040-12-31T05:00:00Z" || end.Format(time.RFC3339) != "2041-01-01T05:00:00Z" {
		t.Fatalf("%v %v %v", start, end, err)
	}
}
