package timeline

import "testing"

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
