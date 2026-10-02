package story_test

import (
	"testing"
	"time"

	"trackstar/internal/project"
	"trackstar/internal/story"
)

func TestProjectStats(t *testing.T) {
	f := setup(t)
	actor := story.Actor{ID: f.user}
	other := "Other"
	quiet, err := project.NewService(f.store, f.clock.Now).Create(f.ctx, 0, project.Input{Name: &other})
	if err != nil {
		t.Fatal(err)
	}
	both := []int64{f.project.ID, quiet.ID}

	// Nothing has happened anywhere yet.
	stats, err := f.svc.ProjectStats(f.ctx, both)
	if err != nil || len(stats) != 0 {
		t.Fatalf("stats of empty projects = %+v, %v", stats, err)
	}
	if stats, err := f.svc.ProjectStats(f.ctx, nil); err != nil || stats == nil || len(stats) != 0 {
		t.Fatalf("stats of no projects = %#v, %v", stats, err)
	}

	a := f.create(t, "A", story.SectionCurrent, 1)
	b := f.create(t, "B", story.SectionCurrent, 2)
	c := f.create(t, "C", story.SectionCurrent, 3)
	d := f.create(t, "D", story.SectionBacklog, 1)
	trashed := f.create(t, "Trashed", story.SectionCurrent, 1)
	f.clock.Advance(time.Hour)
	f.setState(t, a.ID, story.StateStarted)
	f.setState(t, b.ID, story.StateStarted, story.StateFinished, story.StateDelivered)
	f.setState(t, c.ID, story.StateStarted, story.StateFinished, story.StateDelivered, story.StateRejected)
	f.setState(t, trashed.ID, story.StateStarted)
	if _, err := f.svc.Delete(f.ctx, trashed.ID, f.user); err != nil {
		t.Fatal(err)
	}
	renamed := "D, renamed"
	if _, err := f.svc.Update(f.ctx, d.ID, actor, story.UpdateInput{Title: &renamed}); err != nil {
		t.Fatal(err)
	}
	changedAt := f.clock.Now()

	// A is started and C rejected: in progress. B is delivered. The trashed
	// story counts for nothing but the time.
	stats, err = f.svc.ProjectStats(f.ctx, both)
	if err != nil || len(stats) != 1 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if s := stats[0]; s.ProjectID != f.project.ID || s.InProgress != 2 || s.ToAccept != 1 || s.LastActivityAt == nil || !s.LastActivityAt.Equal(changedAt) {
		t.Fatalf("stats = %+v, want 2 in progress, 1 to accept, last activity %s", s, changedAt)
	}

	// A comment is activity too, though it leaves the story's updated_at alone.
	f.clock.Advance(time.Hour)
	if _, err := f.svc.AddComment(f.ctx, a.ID, f.user, "Looks good"); err != nil {
		t.Fatal(err)
	}
	stats, _ = f.svc.ProjectStats(f.ctx, both)
	if !stats[0].LastActivityAt.Equal(f.clock.Now()) {
		t.Fatalf("last activity = %s, want the comment's time %s", stats[0].LastActivityAt, f.clock.Now())
	}

	// Other projects stay out.
	if elsewhere, _ := f.svc.ProjectStats(f.ctx, []int64{quiet.ID}); len(elsewhere) != 0 {
		t.Fatalf("stats leaked across projects: %+v", elsewhere)
	}
}

func TestProjectStatsCountActivityPerDay(t *testing.T) {
	f := setup(t) // the clock starts on 2026-01-05 at 09:00 UTC
	ids := []int64{f.project.ID}
	days := func() []int64 {
		t.Helper()
		stats, err := f.svc.ProjectStats(f.ctx, ids)
		if err != nil || len(stats) != 1 || len(stats[0].Activity) != story.ActivityDays {
			t.Fatalf("stats = %+v, %v", stats, err)
		}
		return stats[0].Activity
	}
	sum := func(days []int64) (n int64) {
		for _, d := range days {
			n += d
		}
		return n
	}
	today := story.ActivityDays - 1

	// Day one: two stories created, one started (which also assigns it), one
	// renamed (an edit, not counted), one comment.
	a := f.create(t, "A", story.SectionCurrent, 1)
	f.create(t, "B", story.SectionBacklog, 1)
	f.setState(t, a.ID, story.StateStarted)
	renamed := "A, renamed"
	if _, err := f.svc.Update(f.ctx, a.ID, story.Actor{ID: f.user}, story.UpdateInput{Title: &renamed}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddComment(f.ctx, a.ID, f.user, "Hello"); err != nil {
		t.Fatal(err)
	}
	const first = 5 // 2 created, started, assigned, commented
	if got := days(); got[today] != first || sum(got) != first {
		t.Fatalf("day one = %v, want %d in the last slot only", got, first)
	}

	// Late the same evening still counts as today; after midnight it is yesterday.
	f.clock.Advance(14*time.Hour + 59*time.Minute) // 23:59
	if got := days(); got[today] != first {
		t.Fatalf("23:59 = %v", got)
	}
	f.clock.Advance(2 * time.Minute) // 00:01 on the 6th
	if got := days(); got[today] != 0 || got[today-1] != first {
		t.Fatalf("after midnight = %v, want %d moved to yesterday", got, first)
	}

	// Two days on, a comment lands in the new today.
	f.clock.Advance(48 * time.Hour)
	if _, err := f.svc.AddComment(f.ctx, a.ID, f.user, "Again"); err != nil {
		t.Fatal(err)
	}
	if got := days(); got[today] != 1 || got[today-3] != first || sum(got) != first+1 {
		t.Fatalf("three days later = %v", got)
	}

	// Old activity falls off the far end; the project still has stats.
	f.clock.Advance(time.Duration(story.ActivityDays) * 24 * time.Hour)
	if got := days(); sum(got) != 0 {
		t.Fatalf("after the window = %v, want all zero", got)
	}
}
