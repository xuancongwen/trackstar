package velocity_test

import (
	"context"
	"testing"
	"time"

	"trackstar/internal/database"
	"trackstar/internal/project"
	"trackstar/internal/story"
	"trackstar/internal/testutil"
	"trackstar/internal/velocity"
)

type fixture struct {
	ctx      context.Context
	clock    *testutil.Clock
	stories  *story.Service
	projects *project.Service
	svc      *velocity.Service
	project  int64
	user     int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	clock := testutil.NewClock(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)) // a Monday
	store := database.NewTestDB(t)
	name := "Apollo"
	projects := project.NewService(store, clock.Now)
	p, err := projects.Create(context.Background(), 0, project.Input{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		ctx:      context.Background(),
		clock:    clock,
		stories:  story.NewService(store, time.UTC, clock.Now),
		projects: projects,
		svc:      velocity.NewService(store, time.UTC, clock.Now),
		project:  p.ID,
		user:     testutil.CreateUser(t, store, "sam@example.com"),
	}
}

// accept creates a story and walks it through the workflow at the current
// time. A negative points value leaves the story unestimated.
func (f *fixture) accept(t *testing.T, typ story.Type, points int64) {
	t.Helper()
	in := story.CreateInput{Title: "s", Type: typ, Section: story.SectionCurrent}
	if points >= 0 {
		in.Estimate = &points
	}
	s, err := f.stories.Create(f.ctx, f.project, f.user, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []story.State{story.StateStarted, story.StateFinished, story.StateDelivered, story.StateAccepted} {
		if _, err := f.stories.Update(f.ctx, s.ID, story.Actor{ID: f.user}, story.UpdateInput{State: &st}); err != nil {
			t.Fatal(err)
		}
	}
}

const week = 7 * 24 * time.Hour

func TestVelocityIsEstimatedBeforeFirstCompletedIteration(t *testing.T) {
	f := setup(t)
	f.accept(t, story.TypeFeature, 8) // current iteration does not count

	got, err := f.svc.Velocity(f.ctx, f.project)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Estimated || got.Velocity != velocity.DefaultVelocity || len(got.Iterations) != 0 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestVelocityAveragesCompletedIterations(t *testing.T) {
	f := setup(t)

	// Iteration 1: 10 feature points, plus a bug and an unfinished story that must not count.
	f.accept(t, story.TypeFeature, 5)
	f.accept(t, story.TypeFeature, 5)
	f.accept(t, story.TypeBug, -1)
	five := int64(5)
	if _, err := f.stories.Create(f.ctx, f.project, f.user, story.CreateInput{Title: "open", Estimate: &five, Section: story.SectionCurrent}); err != nil {
		t.Fatal(err)
	}

	f.clock.Advance(week) // iteration 2: 12 points
	f.accept(t, story.TypeFeature, 8)
	f.accept(t, story.TypeFeature, 3)
	f.accept(t, story.TypeFeature, 1)

	f.clock.Advance(week) // iteration 3: nothing accepted

	f.clock.Advance(week) // iteration 4: 11 points
	f.accept(t, story.TypeFeature, 8)
	f.accept(t, story.TypeFeature, 3)

	f.clock.Advance(week) // iteration 5 (current): in-flight points are ignored
	f.accept(t, story.TypeFeature, 8)

	got, err := f.svc.Velocity(f.ctx, f.project)
	if err != nil {
		t.Fatal(err)
	}
	// Window of 3 → iterations 2, 3, 4 → (12 + 0 + 11) / 3 = 7.67 → 7.
	want := []velocity.IterationPoints{{Number: 2, Points: 12}, {Number: 3, Points: 0}, {Number: 4, Points: 11}}
	if got.Estimated || got.Window != 3 || got.Velocity != 7 || len(got.Iterations) != len(want) {
		t.Fatalf("unexpected result: %+v", got)
	}
	for i := range want {
		if got.Iterations[i] != want[i] {
			t.Fatalf("iterations = %+v, want %+v", got.Iterations, want)
		}
	}

	its, err := f.svc.Iterations(f.ctx, f.project)
	if err != nil {
		t.Fatal(err)
	}
	if len(its) != 5 || !its[4].Current || its[0].Current {
		t.Fatalf("iterations = %+v", its)
	}
	if its[0].Points != 10 || its[0].AcceptedStories != 3 || its[4].Points != 8 {
		t.Fatalf("iteration summaries = %+v", its)
	}
	if !its[0].StartAt.Equal(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) || !its[0].EndAt.Equal(its[1].StartAt) {
		t.Fatalf("iteration 1 range = %v – %v", its[0].StartAt, its[0].EndAt)
	}
}

func TestBugAndChorePointsCountWhenTheProjectAllowsThem(t *testing.T) {
	f := setup(t)
	on := true
	if _, err := f.projects.Update(f.ctx, f.project, project.Input{EstimateBugsAndChores: &on}); err != nil {
		t.Fatal(err)
	}
	f.accept(t, story.TypeFeature, 5)
	f.accept(t, story.TypeBug, 3)
	f.accept(t, story.TypeChore, 2)
	f.clock.Advance(week)

	got, err := f.svc.Velocity(f.ctx, f.project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Velocity != 10 {
		t.Fatalf("velocity = %d, want 10 (features, bugs and chores)", got.Velocity)
	}

	// Switching the option off takes the points away again, history included.
	off := false
	if _, err := f.projects.Update(f.ctx, f.project, project.Input{EstimateBugsAndChores: &off}); err != nil {
		t.Fatal(err)
	}
	if got, err = f.svc.Velocity(f.ctx, f.project); err != nil || got.Velocity != 5 {
		t.Fatalf("velocity after disabling = %d, %v; want 5", got.Velocity, err)
	}
}

func TestVelocityWithFewerIterationsThanWindow(t *testing.T) {
	f := setup(t)
	f.accept(t, story.TypeFeature, 8)
	f.clock.Advance(week)

	got, err := f.svc.Velocity(f.ctx, f.project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Estimated || got.Velocity != 8 || len(got.Iterations) != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
}
