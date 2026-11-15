package timeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("durable store requires Unix locking")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".track"), 0700); err != nil {
		t.Fatal(err)
	}
	return NewStore(root), root
}
func req(rev uint64, id, kind string, value any) Request {
	raw, _ := json.Marshal(value)
	return Request{Source: "agent", EventID: id, ExpectedRevision: rev, Kind: kind, Payload: raw}
}
func apply(t *testing.T, s *Store, request Request) Snapshot {
	t.Helper()
	got, err := s.Apply(context.Background(), "plan", request)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func create(t *testing.T, s *Store) Snapshot {
	return apply(t, s, req(0, "created", "project.create", Project{Title: "Work", NoteID: 123}))
}
func task(id, parent, status string) Task {
	return Task{ID: id, ParentID: parent, Title: id, Status: status, Owner: "agent", Weight: 1, Mode: "serial"}
}

func TestRoundTripReplayAndIdempotency(t *testing.T) {
	s, root := fixture(t)
	create(t, s)
	request := req(1, "task-1", "task.upsert", task("one", "", "active"))
	first := apply(t, s, request)
	if first.Revision != 2 || first.SchemaVersion != 1 || len(first.Events) != 2 {
		t.Fatalf("snapshot %+v", first)
	}
	replay := apply(t, s, request)
	if replay.Revision != 2 {
		t.Fatal("retry appended")
	}
	request.Payload = json.RawMessage(`{"weight":1,"mode":"serial","owner":"agent","status":"active","title":"one","id":"one","done_minutes":0,"remaining_minutes":0}`)
	if got := apply(t, s, request); got.Revision != 2 {
		t.Fatal("JSON key ordering broke idempotency")
	}
	request.Kind = "project.update"
	if _, err := s.Apply(context.Background(), "plan", request); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("changed duplicate: %v", err)
	}
	fresh, err := NewStore(root).Read(context.Background(), "plan")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Title != "Work" || fresh.NoteID != 123 || fresh.Tasks[0].ID != "one" || len(fresh.Reports) != 0 || fresh.Reports == nil {
		t.Fatalf("read %+v", fresh)
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Revision != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	// An incomplete replacement is not authoritative.
	if err := os.WriteFile(filepath.Join(root, ".track/timelines/.plan-crash.tmp"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Read(context.Background(), "plan"); err != nil || got.Revision != 2 {
		t.Fatalf("crash artifact: %+v %v", got, err)
	}
}

func TestInvalidMutationDoesNotPersist(t *testing.T) {
	s, root := fixture(t)
	create(t, s)
	apply(t, s, req(1, "parent", "task.upsert", task("parent", "", "active")))
	apply(t, s, req(2, "child", "task.upsert", task("child", "parent", "pending")))
	file := filepath.Join(root, ".track/timelines/plan.json")
	before, _ := os.ReadFile(file)
	cases := []Request{
		req(3, "cycle", "task.upsert", task("parent", "child", "active")),
		req(3, "orphan", "task.upsert", task("new", "missing", "pending")),
		req(3, "status", "task.upsert", task("new", "", "invalid")),
		req(3, "self", "task.upsert", task("parent", "parent", "active")),
		req(1, "stale", "project.update", Project{Title: "Overwrite"}),
		req(3, "overwrite", "project.create", Project{Title: "Overwrite"}),
		req(3, "bad-report", "report.add", Report{ID: "r", Author: "agent", Summary: "text", PeriodStart: "2026-11-15T00:00:00Z", PeriodEnd: "2026-11-15T01:00:00Z", EventSequences: []uint64{99}}),
		{Source: "agent", EventID: "unknown", ExpectedRevision: 3, Kind: "project.update", Payload: json.RawMessage(`{"title":"Changed","unknown":true}`)},
	}
	for _, r := range cases {
		if _, err := s.Apply(context.Background(), "plan", r); err == nil {
			t.Fatalf("accepted %+v", r)
		}
		after, _ := os.ReadFile(file)
		if string(after) != string(before) {
			t.Fatalf("invalid write changed journal: %s", r.EventID)
		}
	}
}

func TestReportsDiscussionsAndControlReceipts(t *testing.T) {
	s, _ := fixture(t)
	create(t, s)
	apply(t, s, req(1, "task", "task.upsert", task("one", "", "active")))
	r := Report{ID: "report", TaskID: "one", Author: "human", PeriodStart: "2026-11-15T00:00:00Z", PeriodEnd: "2026-11-15T01:00:00Z", Summary: "Implemented", Uncertainty: "Untested browser", UserAction: "Review", EventSequences: []uint64{1, 2}}
	apply(t, s, req(2, "report", "report.add", r))
	apply(t, s, req(3, "discussion", "discussion.add", Discussion{ID: "comment", TargetType: "report", TargetID: "report", Author: "human", Text: "Please verify"}))
	apply(t, s, req(4, "pause", "control.request", Control{ID: "pause", TaskID: "one", Owner: "agent", Action: "pause"}))
	bad := req(5, "premature", "control.update", ControlUpdate{ID: "pause", Status: "completed"})
	if _, err := s.Apply(context.Background(), "plan", bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("premature completion: %v", err)
	}
	bad = req(5, "wrong-owner", "control.update", ControlUpdate{ID: "pause", Status: "acknowledged"})
	bad.Source = "other"
	if _, err := s.Apply(context.Background(), "plan", bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong owner: %v", err)
	}
	apply(t, s, req(5, "ack", "control.update", ControlUpdate{ID: "pause", Status: "acknowledged"}))
	got := apply(t, s, req(6, "completed", "control.update", ControlUpdate{ID: "pause", Status: "completed", Result: "Worker reports checkpointed"}))
	if got.Controls[0].Status != "completed" || got.Tasks[0].Status != "active" {
		t.Fatal("control receipt must not change observed task state")
	}
	if got.Reports[0].EventSequences[1] != 2 || got.Discussions[0].TargetID != "report" {
		t.Fatal("provenance lost")
	}
	if _, err := s.Apply(context.Background(), "plan", req(7, "late", "control.update", ControlUpdate{ID: "pause", Status: "failed"})); err == nil {
		t.Fatal("terminal receipt rewritten")
	}
}

func TestCorruptOrFutureJournalNeverResets(t *testing.T) {
	s, root := fixture(t)
	create(t, s)
	file := filepath.Join(root, ".track/timelines/plan.json")
	for _, raw := range []string{`{"schema_version":99,"project_id":"plan","events":[]}`, `{`, `{"schema_version":1,"project_id":"other","events":[]}`} {
		if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Read(context.Background(), "plan"); err == nil {
			t.Fatal("corrupt read succeeded")
		}
		if _, err := s.Apply(context.Background(), "plan", req(0, "created", "project.create", Project{Title: "reset"})); err == nil {
			t.Fatal("corrupt write succeeded")
		}
		after, _ := os.ReadFile(file)
		if string(after) != raw {
			t.Fatal("corrupt journal overwritten")
		}
	}
}

func TestBoundsAndSafePaths(t *testing.T) {
	s, root := fixture(t)
	create(t, s)
	if _, err := s.Read(context.Background(), "../escape"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal: %v", err)
	}
	if _, err := s.Apply(context.Background(), "plan", req(1, "large", "project.update", Project{Title: "x", Description: strings.Repeat("a", MaxDocumentBytes)})); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	os.WriteFile(outside, []byte("private"), 0600)
	file := filepath.Join(root, ".track/timelines/plan.json")
	os.Remove(file)
	if err := os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background(), "plan"); err == nil {
		t.Fatal("followed file symlink")
	}
	if _, err := s.Apply(context.Background(), "plan", req(0, "new", "project.create", Project{Title: "x"})); err == nil {
		t.Fatal("overwrote through file symlink")
	}
	raw, _ := os.ReadFile(outside)
	if string(raw) != "private" {
		t.Fatal("outside changed")
	}
}

func TestDirectoryAndLockSymlinkRejected(t *testing.T) {
	s, root := fixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".track/timelines")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(context.Background(), "plan", req(0, "created", "project.create", Project{Title: "x"})); err == nil {
		t.Fatal("followed directory symlink")
	}
	os.Remove(filepath.Join(root, ".track/timelines"))
	os.Mkdir(filepath.Join(root, ".track/timelines"), 0700)
	target := filepath.Join(outside, "lock")
	os.WriteFile(target, nil, 0600)
	os.Symlink(target, filepath.Join(root, ".track/timelines/.plan.lock"))
	if _, err := s.Apply(context.Background(), "plan", req(0, "created", "project.create", Project{Title: "x"})); err == nil {
		t.Fatal("followed lock symlink")
	}
}

func TestConcurrentWritersAndCancellation(t *testing.T) {
	s, _ := fixture(t)
	create(t, s)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Apply(context.Background(), "plan", req(1, fmt.Sprintf("task%d", i), "task.upsert", task(fmt.Sprintf("t%d", i), "", "pending")))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("writers %d %d", success, conflict)
	}
	root, err := s.directory(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := safeOpen(root, ".plan.lock", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := acquire(context.Background(), lock); err != nil {
		t.Fatal(err)
	}
	defer release(lock)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := s.Apply(ctx, "plan", req(2, "blocked", "project.update", Project{Title: "x"})); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock deadline: %v", err)
	}
}

func TestMultiprocessWrite(t *testing.T) {
	if root := os.Getenv("TRACK_TIMELINE_TEST_ROOT"); root != "" {
		_, err := NewStore(root).Apply(context.Background(), "plan", req(1, os.Getenv("TRACK_TIMELINE_TEST_EVENT"), "task.upsert", task(os.Getenv("TRACK_TIMELINE_TEST_EVENT"), "", "pending")))
		if errors.Is(err, ErrConflict) {
			os.Exit(10)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(11)
		}
		os.Exit(0)
	}
	s, root := fixture(t)
	create(t, s)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := []*exec.Cmd{}
	for i := range 2 {
		c := exec.Command(exe, "-test.run=^TestMultiprocessWrite$")
		c.Env = append(os.Environ(), "TRACK_TIMELINE_TEST_ROOT="+root, fmt.Sprintf("TRACK_TIMELINE_TEST_EVENT=p%d", i))
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, c)
	}
	success, conflict := 0, 0
	for _, c := range commands {
		err := c.Wait()
		if err == nil {
			success++
		} else if c.ProcessState.ExitCode() == 10 {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("process writers %d/%d", success, conflict)
	}
	got, err := s.Read(context.Background(), "plan")
	if err != nil || got.Revision != 2 || len(got.Tasks) != 1 {
		t.Fatalf("final %+v %v", got, err)
	}
}

func TestCanonicalPreservesLargeNoteIdentity(t *testing.T) {
	for _, id := range []int64{9007199254740993, 9223372036854775807} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			s, root := fixture(t)
			request := req(0, "created", "project.create", Project{Title: "Exact identity", NoteID: id})
			got := apply(t, s, request)
			if got.NoteID != id {
				t.Fatalf("note id rounded: got %d, want %d", got.NoteID, id)
			}
			reopened, err := NewStore(root).Read(context.Background(), "plan")
			if err != nil || reopened.NoteID != id {
				t.Fatalf("persisted note id: %+v %v", reopened, err)
			}
			if retry := apply(t, s, request); retry.Revision != 1 || retry.NoteID != id {
				t.Fatalf("identical retry: %+v", retry)
			}
			different := req(0, "created", "project.create", Project{Title: "Exact identity", NoteID: id - 1})
			if _, err := s.Apply(context.Background(), "plan", different); !errors.Is(err, ErrDuplicate) {
				t.Fatalf("different note id accepted as identical: %v", err)
			}
		})
	}
}

func TestCanonicalRejectsTrailingJSON(t *testing.T) {
	if _, err := canonical([]byte(`{"title":"one"} {"title":"two"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("multiple JSON values accepted: %v", err)
	}
}
