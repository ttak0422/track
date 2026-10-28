package calendarevent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

const testID = "00000000-0000-4000-8000-000000000001"
const testOp = "00000000-0000-4000-8000-000000000002"

func allDay() Fields {
	return Fields{Title: "Review", AllDay: true, Start: "2026-10-29", End: "2026-10-30", Status: "confirmed"}
}
func request(f Fields, rev int64, op string) WriteRequest {
	return WriteRequest{Fields: f, ExpectedRevision: rev, OperationID: op}
}
func op(n int) string { return fmt.Sprintf("10000000-0000-4000-8000-%012d", n) }

func TestValidation(t *testing.T) {
	for name, mutate := range map[string]func(*Fields){
		"empty title":        func(f *Fields) { f.Title = " " },
		"control title":      func(f *Fields) { f.Title = "A\x00B" },
		"long title":         func(f *Fields) { f.Title = strings.Repeat("a", 501) },
		"invalid leap day":   func(f *Fields) { f.Start = "2026-02-29" },
		"same exclusive end": func(f *Fields) { f.End = f.Start },
		"reversed range":     func(f *Fields) { f.End = "2026-10-28" },
		"all day zone":       func(f *Fields) { f.TimeZone = "Asia/Tokyo" },
		"unknown status":     func(f *Fields) { f.Status = "done" },
		"script URL":         func(f *Fields) { f.URL = "javascript:alert(1)" },
		"URL missing host":   func(f *Fields) { f.URL = "http://:80" },
		"URL credentials":    func(f *Fields) { f.URL = "https://person:secret@example.com" },
		"foreign note":       func(f *Fields) { f.NoteID = "other~100" },
		"nonpositive note":   func(f *Fields) { f.NoteID = "0" },
	} {
		t.Run(name, func(t *testing.T) {
			f := allDay()
			mutate(&f)
			if _, err := Normalize(f); !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v", err)
			}
		})
	}
	f := allDay()
	f.Start = "2024-02-29"
	f.End = "2024-03-03"
	f.Description = "first\nsecond\tline"
	f.URL = "https://example.com/earnings"
	f.NoteID = "123"
	if _, err := Normalize(f); err != nil {
		t.Fatal(err)
	}
}

func TestTimedDSTAndOffsets(t *testing.T) {
	for _, tc := range []struct{ zone, start, end, want string }{
		{"America/New_York", "2026-03-08T02:30", "2026-03-08T04:00", "does not exist"},
		{"America/New_York", "2026-11-01T01:30", "2026-11-01T03:00", "ambiguous"},
		{"Pacific/Apia", "2011-12-30T12:00", "2011-12-31T13:00", "does not exist"},
		{"Asia/Tokyo", "2026-10-29T09:00:00Z", "2026-10-29T10:00:00Z", "offset must match"},
		{"Local", "2026-10-29T09:00", "2026-10-29T10:00", "explicit IANA"},
		{"Unknown/Zone", "2026-10-29T09:00", "2026-10-29T10:00", "unknown IANA"},
	} {
		t.Run(tc.zone+tc.start, func(t *testing.T) {
			_, err := Normalize(Fields{Title: "Timed", TimeZone: tc.zone, Start: tc.start, End: tc.end})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	for _, start := range []string{"2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"} {
		f, err := Normalize(Fields{Title: "Fold", TimeZone: "America/New_York", Start: start, End: "2026-11-01T02:30:00-05:00"})
		if err != nil || f.Start != start {
			t.Fatalf("%+v %v", f, err)
		}
	}
	f, err := Normalize(Fields{Title: "Cross midnight", TimeZone: "Asia/Tokyo", Start: "2026-12-31T23:30", End: "2027-01-01T00:30"})
	if err != nil || f.Start != "2026-12-31T23:30:00+09:00" || f.End != "2027-01-01T00:30:00+09:00" {
		t.Fatalf("%+v %v", f, err)
	}
}

func TestLifecycleConflictAndRetry(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	if events, err := List(cfg, false); err != nil || len(events) != 0 {
		t.Fatalf("%v %v", events, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.VaultDir, "calendar")); !os.IsNotExist(err) {
		t.Fatal("read created storage")
	}
	f := allDay()
	first, err := Save(cfg, testID, request(f, 0, testOp))
	if err != nil || first.Revision != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	retry, err := Save(cfg, testID, request(f, 0, testOp))
	if err != nil || retry != first {
		t.Fatalf("retry %+v %v", retry, err)
	}
	f.Title = "Edited"
	if _, err := Save(cfg, testID, request(f, 0, testOp)); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused op: %v", err)
	}
	if _, err := Save(cfg, testID, request(f, 0, op(3))); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale create: %v", err)
	}
	second, err := Save(cfg, testID, request(f, 1, op(3)))
	if err != nil || second.Revision != 2 {
		t.Fatalf("%+v %v", second, err)
	}
	f.Deleted = true
	deleted, err := Save(cfg, testID, request(f, 2, op(4)))
	if err != nil || !deleted.Deleted {
		t.Fatalf("%+v %v", deleted, err)
	}
	if events, err := List(cfg, false); err != nil || len(events) != 0 {
		t.Fatalf("%v %v", events, err)
	}
	if events, err := List(cfg, true); err != nil || len(events) != 1 {
		t.Fatalf("%v %v", events, err)
	}
	f.Title = "Losing original"
	if _, err := Save(cfg, testID, request(f, 3, op(5))); !errors.Is(err, ErrConflict) {
		t.Fatalf("editing trash: %v", err)
	}
	f = second.Fields
	restored, err := Save(cfg, testID, request(f, 3, op(6)))
	if err != nil || restored.Deleted || restored.Revision != 4 {
		t.Fatalf("%+v %v", restored, err)
	}
	loaded, err := Get(cfg, testID)
	if err != nil || loaded != restored {
		t.Fatalf("reopen %+v %v", loaded, err)
	}
	other := &config.Config{VaultDir: t.TempDir()}
	if _, err := Get(other, testID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign event: %v", err)
	}
}

func TestCorruptionAndSymlinks(t *testing.T) {
	for _, what := range []string{"file", "calendar", "events", "lock", "json"} {
		t.Run(what, func(t *testing.T) {
			cfg := &config.Config{VaultDir: t.TempDir()}
			outside := t.TempDir()
			if _, err := Save(cfg, testID, request(allDay(), 0, testOp)); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(cfg.VaultDir, "calendar", "events")
			switch what {
			case "file":
				p := filepath.Join(root, testID+".json")
				os.Remove(p)
				if err := os.Symlink(filepath.Join(outside, "event.json"), p); err != nil {
					t.Fatal(err)
				}
			case "calendar":
				p := filepath.Join(cfg.VaultDir, "calendar")
				os.Rename(p, p+".saved")
				if err := os.Symlink(outside, p); err != nil {
					t.Fatal(err)
				}
			case "events":
				os.Rename(root, root+".saved")
				if err := os.Symlink(outside, root); err != nil {
					t.Fatal(err)
				}
			case "lock":
				p := filepath.Join(root, ".save.lock")
				os.Remove(p)
				if err := os.Symlink(filepath.Join(outside, "lock"), p); err != nil {
					t.Fatal(err)
				}
			case "json":
				if err := os.WriteFile(filepath.Join(root, testID+".json"), []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Save(cfg, testID, request(allDay(), 1, op(9))); err == nil {
				t.Fatal("accepted invalid storage")
			}
			if what != "lock" {
				if _, err := List(cfg, true); err == nil {
					t.Fatal("hid invalid storage as empty")
				}
			}
			if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
				t.Fatalf("wrote outside vault: %v %v", entries, err)
			}
		})
	}
}

func TestConcurrentCASAndAtomicReads(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	if _, err := Save(cfg, testID, request(allDay(), 0, testOp)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f := allDay()
			f.Title = fmt.Sprint(i)
			_, err := Save(cfg, testID, request(f, 1, op(i+10)))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("%d concurrent writers succeeded", success)
	}
	reads := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if _, err := Get(cfg, testID); err != nil {
				reads <- err
				return
			}
		}
		reads <- nil
	}()
	for i := int64(2); i < 20; i++ {
		current, err := Get(cfg, testID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Save(cfg, testID, request(current.Fields, i, op(int(i)+30))); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-reads; err != nil {
		t.Fatalf("reader saw partial write: %v", err)
	}
}

func TestInvalidSavedRecordIsNeverOverwritten(t *testing.T) {
	for _, key := range []string{"version", "revision", "id", "status", "updated_at", "last_request_hash", "title"} {
		t.Run(key, func(t *testing.T) {
			cfg := &config.Config{VaultDir: t.TempDir()}
			if _, err := Save(cfg, testID, request(allDay(), 0, testOp)); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.VaultDir, "calendar", "events", testID+".json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]any
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatal(err)
			}
			switch key {
			case "version":
				stored[key] = 99
			case "revision":
				stored[key] = 0
			default:
				stored[key] = "invalid"
			}
			raw, err = json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Get(cfg, testID); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("read: %v", err)
			}
			if _, err := Save(cfg, testID, request(allDay(), 1, op(200))); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("save: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(raw) {
				t.Fatalf("corrupt record changed: %v", err)
			}
		})
	}
}

func TestCalendarEventProcessHelper(t *testing.T) {
	path := os.Getenv("TRACK_EVENT_HELPER_VAULT")
	if path == "" {
		return
	}
	f := allDay()
	f.Title = os.Getenv("TRACK_EVENT_HELPER_OP")
	_, err := Save(&config.Config{VaultDir: path}, testID, request(f, 1, os.Getenv("TRACK_EVENT_HELPER_OP")))
	if errors.Is(err, ErrConflict) {
		os.Exit(3)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	os.Exit(0)
}

func TestCrossProcessCAS(t *testing.T) {
	cfg := &config.Config{VaultDir: t.TempDir()}
	if _, err := Save(cfg, testID, request(allDay(), 0, testOp)); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var commands []*exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestCalendarEventProcessHelper$")
		cmd.Env = append(os.Environ(), "TRACK_EVENT_HELPER_VAULT="+cfg.VaultDir, "TRACK_EVENT_HELPER_OP="+op(i+90))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	winners := 0
	for _, cmd := range commands {
		err := cmd.Wait()
		if err == nil {
			winners++
			continue
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d process writers won", winners)
	}
}
