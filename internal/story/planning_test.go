package story_test

import (
	"context"
	"testing"

	"trackstar/internal/apperr"
	"trackstar/internal/story"
)

func TestEpics(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	desc := "Sign-in overhaul"
	name := " Auth "
	e, err := f.svc.CreateEpic(ctx, f.project.ID, story.EpicInput{Name: &name, Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "auth" || e.Description != desc || e.TotalPoints != 0 {
		t.Fatalf("epic = %+v", e)
	}
	if _, err := f.svc.CreateEpic(ctx, f.project.ID, story.EpicInput{Name: &name}); apperr.KindOf(err) != apperr.KindConflict {
		t.Fatalf("duplicate epic: err = %v", err)
	}

	// Stories join by label; progress counts feature points and accepted stories.
	a := f.create(t, "A", story.SectionCurrent, 3)
	b := f.create(t, "B", story.SectionBacklog, 5)
	bug, _ := f.svc.Create(ctx, f.project.ID, f.user, story.CreateInput{Title: "bug", Type: story.TypeBug, Labels: []string{"auth"}})
	labels := []string{"auth", "other"}
	for _, id := range []int64{a.ID, b.ID} {
		if _, err := f.svc.Update(ctx, id, story.Actor{ID: f.user}, story.UpdateInput{Labels: &labels}); err != nil {
			t.Fatal(err)
		}
	}
	f.setState(t, a.ID, story.StateStarted, story.StateFinished, story.StateDelivered, story.StateAccepted)

	epics, err := f.svc.Epics(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(epics) != 1 || epics[0].TotalPoints != 8 || epics[0].AcceptedPoints != 3 || epics[0].StoryCount != 3 || epics[0].AcceptedCount != 1 {
		t.Fatalf("epics = %+v", epics)
	}
	_ = bug

	// Promote an existing plain label; rename an epic renames the label on stories.
	other := "other"
	if _, err := f.svc.CreateEpic(ctx, f.project.ID, story.EpicInput{Name: &other}); err != nil {
		t.Fatal(err)
	}
	renamed := "misc"
	if _, err := f.svc.UpdateEpic(ctx, e.ID, story.EpicInput{Name: &renamed}); err != nil {
		t.Fatal(err)
	}
	d, _ := f.svc.Get(ctx, a.ID)
	if d.Labels[0] != "misc" || d.Labels[1] != "other" {
		t.Fatalf("labels after rename = %v", d.Labels)
	}
	if _, err := f.svc.UpdateEpic(ctx, e.ID, story.EpicInput{Name: &other}); apperr.KindOf(err) != apperr.KindConflict {
		t.Fatalf("rename onto existing label: err = %v", err)
	}

	// Demote keeps the label on stories.
	if err := f.svc.DemoteEpic(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	epics, _ = f.svc.Epics(ctx, f.project.ID)
	if len(epics) != 1 || epics[0].Name != "other" {
		t.Fatalf("epics after demote = %+v", epics)
	}
	d, _ = f.svc.Get(ctx, a.ID)
	if len(d.Labels) != 2 {
		t.Fatalf("labels after demote = %v", d.Labels)
	}
	if err := f.svc.DemoteEpic(ctx, e.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("demote twice: err = %v", err)
	}
}

func TestTasks(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := f.create(t, "S", story.SectionBacklog, 1)

	t1, err := f.svc.AddTask(ctx, s.ID, " write tests ")
	if err != nil {
		t.Fatal(err)
	}
	t2, _ := f.svc.AddTask(ctx, s.ID, "ship")
	t3, _ := f.svc.AddTask(ctx, s.ID, "celebrate")
	if _, err := f.svc.AddTask(ctx, s.ID, "  "); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("blank task: err = %v", err)
	}
	if t1.Description != "write tests" || t1.Position >= t2.Position || t2.Position >= t3.Position {
		t.Fatalf("tasks = %+v %+v %+v", t1, t2, t3)
	}

	done := true
	if _, err := f.svc.UpdateTask(ctx, t2.ID, story.TaskInput{Done: &done}); err != nil {
		t.Fatal(err)
	}
	list, _ := f.svc.List(ctx, f.project.ID, story.ListOptions{})
	if list[0].TaskCount != 3 || list[0].TasksDone != 1 {
		t.Fatalf("task counts in list = %d/%d", list[0].TasksDone, list[0].TaskCount)
	}
	d, _ := f.svc.Get(ctx, s.ID)
	if len(d.Tasks) != 3 || d.TaskCount != 3 || d.TasksDone != 1 {
		t.Fatalf("detail tasks = %+v", d.Tasks)
	}

	// Reorder: move the last task to the top.
	top := 0
	if _, err := f.svc.UpdateTask(ctx, t3.ID, story.TaskInput{Position: &top}); err != nil {
		t.Fatal(err)
	}
	tasks, _ := f.svc.Tasks(ctx, s.ID)
	if tasks[0].ID != t3.ID || tasks[1].ID != t1.ID || tasks[2].ID != t2.ID {
		t.Fatalf("order after reorder = %v %v %v", tasks[0].ID, tasks[1].ID, tasks[2].ID)
	}

	if sid, err := f.svc.DeleteTask(ctx, t1.ID); err != nil || sid != s.ID {
		t.Fatalf("delete task: %d, %v", sid, err)
	}
	if _, err := f.svc.DeleteTask(ctx, t1.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("delete twice: err = %v", err)
	}
}

func TestBlockers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a := f.create(t, "A", story.SectionBacklog, 1)
	b := f.create(t, "B", story.SectionBacklog, 1)
	c := f.create(t, "C", story.SectionBacklog, 1)
	other, _ := f.svc.Create(ctx, 999, f.user, story.CreateInput{Title: "x"}) // fails; just an id that doesn't exist
	_ = other

	set := func(id int64, blockers []int64) error {
		_, err := f.svc.Update(ctx, id, story.Actor{ID: f.user}, story.UpdateInput{BlockedBy: &blockers})
		return err
	}
	if err := set(a.ID, []int64{b.ID, c.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if err := set(a.ID, []int64{a.ID}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("self-block: err = %v", err)
	}
	if err := set(a.ID, []int64{999}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("unknown blocker: err = %v", err)
	}
	if err := set(b.ID, []int64{a.ID}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("cycle: err = %v", err)
	}
	if err := set(c.ID, []int64{b.ID}); err != nil { // chain a→b→? fine: a blocked by b, c blocked by b
		t.Fatal(err)
	}

	list, _ := f.svc.List(ctx, f.project.ID, story.ListOptions{})
	byTitle := map[string]story.Story{}
	for _, s := range list {
		byTitle[s.Title] = s
	}
	if !byTitle["A"].Blocked || len(byTitle["A"].BlockedBy) != 2 || byTitle["B"].Blocked {
		t.Fatalf("A = %+v, B = %+v", byTitle["A"], byTitle["B"])
	}

	// Accepting a blocker unblocks; deleting one removes it.
	f.setState(t, b.ID, story.StateStarted, story.StateFinished, story.StateDelivered, story.StateAccepted)
	d, _ := f.svc.Get(ctx, c.ID)
	if d.Blocked {
		t.Fatalf("C still blocked by accepted B: %+v", d)
	}
	d, _ = f.svc.Get(ctx, a.ID)
	if !d.Blocked || len(d.BlockedBy) != 2 {
		t.Fatalf("A should still be blocked by C: %+v", d)
	}
	if _, err := f.svc.Delete(ctx, c.ID, f.user); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.Get(ctx, a.ID)
	if d.Blocked || len(d.BlockedBy) != 1 {
		t.Fatalf("A after C deleted: %+v", d)
	}
}

func TestMoveMany(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a := f.create(t, "A", story.SectionBacklog, 1)
	b := f.create(t, "B", story.SectionBacklog, 1)
	c := f.create(t, "C", story.SectionBacklog, 1)
	d := f.create(t, "D", story.SectionBacklog, 1)
	i1 := f.create(t, "I1", story.SectionIcebox, 1)

	// Move A and C (in that order) after D.
	moved, err := f.svc.MoveMany(ctx, []int64{a.ID, c.ID}, f.user, story.MoveInput{Section: story.SectionBacklog, PrevID: &d.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 2 {
		t.Fatalf("moved = %d", len(moved))
	}
	assertOrder(t, f.titles(t, story.SectionBacklog), "B", "D", "A", "C")

	// Icebox + backlog stories to the top of current, state changes atomically.
	moved, err = f.svc.MoveMany(ctx, []int64{i1.ID, b.ID}, f.user, story.MoveInput{Section: story.SectionCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if moved[0].State != story.StateUnstarted || moved[1].State != story.StateUnstarted {
		t.Fatalf("states = %s %s", moved[0].State, moved[1].State)
	}
	assertOrder(t, f.titles(t, story.SectionCurrent), "I1", "B")
	assertOrder(t, f.titles(t, story.SectionBacklog), "D", "A", "C")

	// Before a given story.
	if _, err := f.svc.MoveMany(ctx, []int64{d.ID}, f.user, story.MoveInput{Section: story.SectionBacklog, NextID: &c.ID}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, f.titles(t, story.SectionBacklog), "A", "D", "C")

	// All-or-nothing: a started story in the batch aborts the whole move.
	f.setState(t, b.ID, story.StateStarted)
	_, err = f.svc.MoveMany(ctx, []int64{a.ID, b.ID}, f.user, story.MoveInput{Section: story.SectionIcebox})
	if apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("err = %v", err)
	}
	assertOrder(t, f.titles(t, story.SectionBacklog), "A", "D", "C")
	if _, err := f.svc.MoveMany(ctx, []int64{a.ID}, f.user, story.MoveInput{Section: story.SectionBacklog, PrevID: &a.ID}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("target inside batch: err = %v", err)
	}
}
