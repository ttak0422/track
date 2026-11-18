package calendarevent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ttak0422/track/internal/track/config"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func rangeAsDay(cfg *config.Config, date, zone string) (DayProjection, error) {
	r, err := RangeContext(context.Background(), cfg, date, date, zone)
	d := DayProjection{}
	if err == nil {
		d.Events = make([]Event, r.Days[0].Count)
	}
	return d, err
}
func TestRangeReadFailuresAndLimit(t *testing.T) {
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
	if _, err := rangeAsDay(cfg, "2026-10-29", "UTC"); !errors.Is(err, ErrCorrupt) {
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
	if _, err := rangeAsDay(cfg, "2026-10-29", "UTC"); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("limit: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, saved.ID+".json")); err != nil {
		t.Fatal(err)
	}
	day, err := rangeAsDay(cfg, "2026-10-29", "UTC")
	if err != nil || len(day.Events) != MaxDaySourceEvents {
		t.Fatalf("exact limit: %d %v", len(day.Events), err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := rangeAsDay(cfg, "2026-10-29", "UTC"); err == nil {
		t.Fatal("read failure became empty success")
	}
}

func TestRangeMissingVaultAndAnchoredStorage(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	cfg := &config.Config{VaultDir: vault}
	if err := os.Mkdir(vault, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := rangeAsDay(cfg, "2026-10-29", "UTC"); err != nil {
		t.Fatalf("absent calendar should be empty: %v", err)
	}
	if err := os.Remove(vault); err != nil {
		t.Fatal(err)
	}
	if _, err := list(cfg, false, MaxDaySourceEvents); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bounded list hid missing vault: %v", err)
	}
	if _, err := rangeAsDay(cfg, "2026-10-29", "UTC"); !errors.Is(err, os.ErrNotExist) {
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

func TestRangeDatesAndZones(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	for _, pair := range [][2]string{{"2024-02-01", "2024-03-13"}, {"2026-12-31", "2027-01-02"}, {"0001-01-01", "0001-01-01"}, {"9998-12-31", "9998-12-31"}} {
		got, err := RangeContext(context.Background(), cfg, pair[0], pair[1], "UTC")
		if err != nil {
			t.Fatal(err)
		}
		first, _ := time.Parse(time.DateOnly, pair[0])
		last, _ := time.Parse(time.DateOnly, pair[1])
		if len(got.Days) != int(last.Sub(first).Hours()/24)+1 {
			t.Fatal(got)
		}
		for i, d := range got.Days {
			if d.Date != first.AddDate(0, 0, i).Format(time.DateOnly) || d.Count != 0 || d.Previews == nil {
				t.Fatal(d)
			}
		}
	}
	for _, pair := range [][2]string{{"2024-02-01", "2024-03-14"}, {"2024-02-02", "2024-02-01"}, {"2023-02-29", "2023-03-01"}, {"0000-01-01", "0001-01-01"}, {"9999-01-01", "9999-01-01"}, {"2024-2-01", "2024-02-02"}} {
		if _, err := RangeContext(context.Background(), cfg, pair[0], pair[1], "UTC"); !errors.Is(err, ErrValidation) {
			t.Fatal(pair, err)
		}
	}
	for _, zone := range []string{"", "Local", "utc", "Mars/Test", strings.Repeat("X", 129)} {
		if _, err := RangeContext(context.Background(), cfg, "2024-02-29", "2024-02-29", zone); !errors.Is(err, ErrValidation) {
			t.Fatal(zone, err)
		}
	}
	if _, err := RangeContext(context.Background(), cfg, "2011-12-29", "2011-12-31", "Pacific/Apia"); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RangeContext(ctx, cfg, "2024-02-29", "2024-02-29", "UTC"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(cfg.VaultDir)
	if len(entries) != 0 {
		t.Fatal("read created storage")
	}
}

func TestRangeMatchesDay(t *testing.T) {
	for _, tc := range []struct{ date, zone string }{
		{"2024-02-29", "UTC"}, {"2026-03-08", "America/New_York"}, {"2026-11-01", "America/New_York"}, {"2026-10-04", "Australia/Lord_Howe"}, {"2026-04-05", "Australia/Lord_Howe"},
	} {
		t.Run(tc.date+tc.zone, func(t *testing.T) {
			cfg := &config.Config{VaultDir: t.TempDir()}
			day, err := Day(cfg, tc.date, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			start, _ := time.Parse(time.RFC3339, day.DayStart)
			end, _ := time.Parse(time.RFC3339, day.DayEnd)
			loc, _ := time.LoadLocation(tc.zone)
			start, end = start.In(loc), end.In(loc)
			civil, _ := time.Parse(time.DateOnly, tc.date)
			fields := []Fields{
				{Title: "z", AllDay: true, Start: tc.date, End: civil.AddDate(0, 0, 1).Format(time.DateOnly), Status: "cancelled"},
				{Title: "a", AllDay: true, Start: tc.date, End: civil.AddDate(0, 0, 1).Format(time.DateOnly), Status: "confirmed"},
				{Title: "a", AllDay: true, Start: tc.date, End: civil.AddDate(0, 0, 1).Format(time.DateOnly), Status: "confirmed"},
				{Title: "timed", Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339), TimeZone: tc.zone, Status: "confirmed"},
				{Title: "before", Start: start.Add(-time.Hour).Format(time.RFC3339), End: start.Format(time.RFC3339), TimeZone: tc.zone, Status: "confirmed"},
				{Title: "after", Start: end.Format(time.RFC3339), End: end.Add(time.Hour).Format(time.RFC3339), TimeZone: tc.zone, Status: "confirmed"},
				{Title: "exclusive", AllDay: true, Start: civil.AddDate(0, 0, -1).Format(time.DateOnly), End: tc.date, Status: "confirmed"},
				{Title: "deleted", AllDay: true, Start: tc.date, End: civil.AddDate(0, 0, 1).Format(time.DateOnly), Status: "confirmed", Deleted: true},
			}
			for i, f := range fields {
				deleted := f.Deleted
				f.Deleted = false
				if _, err := Save(cfg, op(i+10), request(f, 0, op(i+100))); err != nil {
					t.Fatal(err)
				}
				if deleted {
					f.Deleted = true
					if _, err := Save(cfg, op(i+10), request(f, 1, op(i+200))); err != nil {
						t.Fatal(err)
					}
				}
				if i == 2 {
					r, err := RangeContext(context.Background(), cfg, tc.date, tc.date, tc.zone)
					if err != nil || r.Days[0].Count != 3 || len(r.Days[0].Previews) != 3 {
						t.Fatal(r, err)
					}
				}
			}
			before := map[string]string{}
			filepath.WalkDir(cfg.VaultDir, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if !d.IsDir() {
					raw, _ := os.ReadFile(p)
					info, _ := d.Info()
					before[p] = string(raw) + info.ModTime().String()
				}
				return nil
			})
			day, err = Day(cfg, tc.date, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			got, err := RangeContext(context.Background(), cfg, tc.date, tc.date, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			if got.Days[0].Count != 4 || got.Days[0].Count != len(day.Events) || len(got.Days[0].Previews) != 3 {
				t.Fatal(got)
			}
			for i, p := range got.Days[0].Previews {
				e := day.Events[i]
				if p != (EventPreview{e.ID, e.Revision, e.Title, e.Status}) {
					t.Fatal(p, e)
				}
			}
			for p, old := range before {
				raw, _ := os.ReadFile(p)
				info, _ := os.Stat(p)
				if old != string(raw)+info.ModTime().String() {
					t.Fatal("projection wrote", p)
				}
			}
		})
	}
}

func TestRangeHostileStorage(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			cfg := &config.Config{VaultDir: t.TempDir()}
			dir := filepath.Join(cfg.VaultDir, "calendar", "events")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, testID+".json")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(filepath.Join(t.TempDir(), "absent"), path)
			case "directory":
				err = os.Mkdir(path, 0755)
			case "oversized":
				err = os.WriteFile(path, []byte(strings.Repeat("x", maxRecordBytes+1)), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := RangeContext(context.Background(), cfg, "2024-02-29", "2024-02-29", "UTC"); err == nil || len(got.Days) != 0 {
				t.Fatal(got, err)
			}
		})
	}
}
func TestRangeMonthZoneBudget(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := RangeContext(ctx, cfg, "2026-03-01", "2026-04-11", "America/New_York")
	if err != nil || len(got.Days) != 42 {
		t.Fatalf("month zone budget: %d %v", len(got.Days), err)
	}
}
