package timeline

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestWeightedProgressAndScheduling(t *testing.T) {
	s, _ := fixture(t)
	create(t, s)
	a := task("a", "", "done")
	a.Weight = 3
	a.DoneMinutes = 20
	b := task("b", "", "active")
	b.DoneMinutes = 5
	b.RemainingMinutes = 5
	apply(t, s, req(1, "a", "task.upsert", a))
	got := apply(t, s, req(2, "b", "task.upsert", b))
	if got.Progress.Fraction != 0.875 || got.Progress.DoneMinutes != 25 || got.Progress.RemainingMinutes != 5 {
		t.Fatalf("weighted %+v", got.Progress)
	}
	parent := task("parent", "", "active")
	parent.Mode = "parallel"
	parent.DoneMinutes = 999
	apply(t, s, req(3, "parent", "task.upsert", parent))
	a.ParentID = "parent"
	a.Status = "active"
	a.RemainingMinutes = 10
	apply(t, s, req(4, "move-a", "task.upsert", a))
	b.ParentID = "parent"
	got = apply(t, s, req(5, "move-b", "task.upsert", b))
	if got.Progress.DoneMinutes != 25 || got.Progress.RemainingMinutes != 10 {
		t.Fatalf("parallel %+v", got.Progress)
	}
	parent.Mode = "serial"
	got = apply(t, s, req(6, "serial", "task.upsert", parent))
	if got.Progress.RemainingMinutes != 15 {
		t.Fatalf("serial %+v", got.Progress)
	}
	parent.Status = "retired"
	got = apply(t, s, req(7, "retire", "task.upsert", parent))
	if got.Progress.Fraction != 0 || got.Progress.RemainingMinutes != 0 || got.Progress.Retired != 1 {
		t.Fatalf("retired %+v", got.Progress)
	}
}

func TestSubtreeProgressNestedWeightsAndRetirement(t *testing.T) {
	// root-group's own estimate must not be counted in addition to its children.
	root := task("root-group", "", "active")
	root.Weight, root.DoneMinutes, root.RemainingMinutes = 2, 999, 999
	branch := task("branch", root.ID, "active")
	branch.Mode, branch.Weight = "parallel", 3
	a := task("a", branch.ID, "done")
	a.Weight, a.DoneMinutes, a.RemainingMinutes = 3, 20, 99
	b := task("b", branch.ID, "active")
	b.Weight, b.DoneMinutes, b.RemainingMinutes = 1, 5, 5
	c := task("c", root.ID, "active")
	c.Weight, c.DoneMinutes, c.RemainingMinutes = 1, 0, 10
	d := task("d", "", "active")
	d.Weight, d.DoneMinutes, d.RemainingMinutes = 1, 0, 20
	s := Snapshot{Project: Project{Mode: "serial"}, Tasks: []Task{root, branch, a, b, c, d}}
	project, got := projectProgress(&s)
	check := func(id string, want Progress) {
		t.Helper()
		if got[id] != want {
			t.Fatalf("%s: got %+v want %+v", id, got[id], want)
		}
	}
	check("a", Progress{Fraction: 1, DoneMinutes: 20, Total: 1, Done: 1})
	check("branch", Progress{Fraction: .875, DoneMinutes: 25, RemainingMinutes: 5, Total: 3, Done: 1})
	check("root-group", Progress{Fraction: .65625, DoneMinutes: 25, RemainingMinutes: 15, Total: 5, Done: 1})
	if project != (Progress{Fraction: .4375, DoneMinutes: 25, RemainingMinutes: 35, Total: 6, Done: 1}) {
		t.Fatalf("project: %+v", project)
	}
	s.Mode = "parallel"
	project, got = projectProgress(&s)
	if project.RemainingMinutes != 20 || got[root.ID].RemainingMinutes != 15 {
		t.Fatalf("parallel root: %+v, child %+v", project, got[root.ID])
	}
	// Retirement excludes the whole subtree from ancestors, not its history.
	s.Tasks[1].Status = "retired"
	project, got = projectProgress(&s)
	check("branch", Progress{Total: 3, Done: 1, Retired: 1})
	check("a", Progress{Fraction: 1, DoneMinutes: 20, Total: 1, Done: 1})
	check("root-group", Progress{RemainingMinutes: 10, Total: 5, Done: 1, Retired: 1})
	if project != (Progress{RemainingMinutes: 20, Total: 6, Done: 1, Retired: 1}) {
		t.Fatalf("retired contribution: %+v", project)
	}
	// A parent with only retired children is not reinterpreted as a leaf.
	s.Tasks[4].Status = "retired"
	_, got = projectProgress(&s)
	check("root-group", Progress{Total: 5, Done: 1, Retired: 2})
	if len(got) != len(s.Tasks) {
		t.Fatalf("missing excluded descendants: %+v", got)
	}
}

func TestSubtreeProgressReplayedAndNotAcceptedAsPayload(t *testing.T) {
	store, dir := fixture(t)
	create(t, store)
	empty, err := store.Read(context.Background(), "plan")
	if err != nil {
		t.Fatal(err)
	}
	if empty.TaskProgress == nil || len(empty.TaskProgress) != 0 {
		t.Fatalf("empty projection: %+v", empty.TaskProgress)
	}
	got := apply(t, store, req(1, "a", "task.upsert", task("a", "", "done")))
	reopened, err := NewStore(dir).Read(context.Background(), "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.TaskProgress, reopened.TaskProgress) {
		t.Fatal("projection not replayed")
	}
	bad := req(2, "bad-derived-field", "task.upsert", map[string]any{"id": "a", "title": "A", "status": "done", "task_progress": map[string]any{}})
	if _, err := store.Apply(context.Background(), "plan", bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("derived input accepted: %v", err)
	}
}
