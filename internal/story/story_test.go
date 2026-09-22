package story_test

import (
	"context"
	"testing"
	"time"

	"tracker/internal/apperr"
	"tracker/internal/database"
	"tracker/internal/project"
	"tracker/internal/story"
	"tracker/internal/testutil"
)

type fixture struct {
	ctx     context.Context
	store   *database.DB
	svc     *story.Service
	clock   *testutil.Clock
	project project.Project
	user    int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	// Monday 2026-01-05: the first day of iteration 1.
	clock := testutil.NewClock(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	store := database.NewTestDB(t)
	name := "Apollo"
	p, err := project.NewService(store, clock.Now).Create(context.Background(), project.Input{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		ctx:     context.Background(),
		store:   store,
		svc:     story.NewService(store, time.UTC, clock.Now),
		clock:   clock,
		project: p,
		user:    testutil.CreateUser(t, store, "sam@example.com"),
	}
}

func (f *fixture) create(t *testing.T, title string, sec story.Section, estimate int64) story.Story {
	t.Helper()
	s, err := f.svc.Create(f.ctx, f.project.ID, f.user, story.CreateInput{Title: title, Section: sec, Estimate: &estimate})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return s
}

func (f *fixture) setState(t *testing.T, id int64, states ...story.State) story.Story {
	t.Helper()
	var s story.Story
	for _, st := range states {
		var err error
		if s, err = f.svc.Update(f.ctx, id, f.user, story.UpdateInput{State: &st}); err != nil {
			t.Fatalf("transition to %s: %v", st, err)
		}
	}
	return s
}

// titles returns the section's stories in board order.
func (f *fixture) titles(t *testing.T, sec story.Section) []string {
	t.Helper()
	all, err := f.svc.List(f.ctx, f.project.ID, story.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range all {
		if s.Section == sec && s.State != story.StateAccepted {
			out = append(out, s.Title)
		}
	}
	return out
}

func assertOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestCreate(t *testing.T) {
	f := setup(t)

	s, err := f.svc.Create(f.ctx, f.project.ID, f.user, story.CreateInput{Title: "  Add OAuth  ", Labels: []string{"Auth", "auth", " Needs  Design "}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Add OAuth" || s.Type != story.TypeFeature || s.State != story.StateIcebox ||
		s.Section != story.SectionIcebox || s.Estimate != nil || s.RequesterID != f.user {
		t.Fatalf("unexpected story: %+v", s)
	}
	if len(s.Labels) != 2 || s.Labels[0] != "auth" || s.Labels[1] != "needs design" {
		t.Fatalf("labels = %v", s.Labels)
	}

	bad := int64(4)
	for name, in := range map[string]story.CreateInput{
		"empty title":  {Title: " "},
		"bad type":     {Title: "x", Type: "epic"},
		"bad estimate": {Title: "x", Estimate: &bad},
		"bad section":  {Title: "x", Section: story.SectionDone},
	} {
		if _, err := f.svc.Create(f.ctx, f.project.ID, f.user, in); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if _, err := f.svc.Create(f.ctx, 999, f.user, story.CreateInput{Title: "x"}); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("unknown project: err = %v", err)
	}
}

func TestCreatePlacement(t *testing.T) {
	f := setup(t)
	f.create(t, "ice 1", story.SectionIcebox, 1)
	f.create(t, "ice 2", story.SectionIcebox, 1)
	f.create(t, "back 1", story.SectionBacklog, 1)
	f.create(t, "back 2", story.SectionBacklog, 1)

	assertOrder(t, f.titles(t, story.SectionIcebox), "ice 2", "ice 1") // newest idea on top
	assertOrder(t, f.titles(t, story.SectionBacklog), "back 1", "back 2")
}

func TestFullWorkflow(t *testing.T) {
	f := setup(t)
	s := f.create(t, "Feature", story.SectionBacklog, 3)

	s = f.setState(t, s.ID, story.StateStarted)
	if s.Section != story.SectionCurrent {
		t.Fatalf("started story is in %s, want current", s.Section)
	}
	if s.OwnerID == nil || *s.OwnerID != f.user {
		t.Fatalf("starting should assign the actor as owner, got %v", s.OwnerID)
	}

	s = f.setState(t, s.ID, story.StateFinished, story.StateDelivered, story.StateRejected, story.StateStarted,
		story.StateFinished, story.StateDelivered)
	if s.AcceptedAt != nil {
		t.Fatal("accepted_at set before acceptance")
	}

	f.clock.Advance(time.Hour)
	s = f.setState(t, s.ID, story.StateAccepted)
	if s.AcceptedAt == nil || !s.AcceptedAt.Equal(f.clock.Now()) {
		t.Fatalf("accepted_at = %v, want %v", s.AcceptedAt, f.clock.Now())
	}
	if s.Section != story.SectionCurrent {
		t.Fatalf("freshly accepted story is in %s, want current", s.Section)
	}

	// Once the iteration is over the story becomes history.
	f.clock.Advance(7 * 24 * time.Hour)
	d, err := f.svc.Get(f.ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Section != story.SectionDone {
		t.Fatalf("section = %s, want done", d.Section)
	}
	active, _ := f.svc.List(f.ctx, f.project.ID, story.ListOptions{})
	done, _ := f.svc.List(f.ctx, f.project.ID, story.ListOptions{Done: true})
	if len(active) != 0 || len(done) != 1 {
		t.Fatalf("active = %d, done = %d; want 0, 1", len(active), len(done))
	}
}

func TestInvalidTransitions(t *testing.T) {
	f := setup(t)
	s := f.create(t, "Feature", story.SectionBacklog, 2)

	for _, to := range []story.State{story.StateFinished, story.StateDelivered, story.StateAccepted, story.StateRejected, "bogus"} {
		if _, err := f.svc.Update(f.ctx, s.ID, f.user, story.UpdateInput{State: &to}); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("backlog → %s: err = %v, want invalid", to, err)
		}
	}

	f.setState(t, s.ID, story.StateStarted, story.StateFinished, story.StateDelivered, story.StateAccepted)
	for _, to := range []story.State{story.StateStarted, story.StateRejected, story.StateBacklog} {
		if _, err := f.svc.Update(f.ctx, s.ID, f.user, story.UpdateInput{State: &to}); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("accepted → %s: err = %v, want invalid", to, err)
		}
	}
	if _, err := f.svc.Update(f.ctx, s.ID, f.user, story.UpdateInput{Estimate: story.Some(int64(8))}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Errorf("re-estimating an accepted story: err = %v, want invalid", err)
	}
}

func TestEstimateRules(t *testing.T) {
	f := setup(t)
	started := story.StateStarted

	feature, err := f.svc.Create(f.ctx, f.project.ID, f.user, story.CreateInput{Title: "Feature"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Update(f.ctx, feature.ID, f.user, story.UpdateInput{State: &started}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("starting an unestimated feature: err = %v, want invalid", err)
	}
	// Estimating and starting in one request is fine.
	if _, err := f.svc.Update(f.ctx, feature.ID, f.user, story.UpdateInput{State: &started, Estimate: story.Some(int64(5))}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Update(f.ctx, feature.ID, f.user, story.UpdateInput{Estimate: story.Null[int64]()}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("clearing the estimate of a started feature: err = %v, want invalid", err)
	}
	if _, err := f.svc.Update(f.ctx, feature.ID, f.user, story.UpdateInput{Estimate: story.Some(int64(4))}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("estimate 4: err = %v, want invalid", err)
	}

	bug, err := f.svc.Create(f.ctx, f.project.ID, f.user, story.CreateInput{Title: "Bug", Type: story.TypeBug})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Update(f.ctx, bug.ID, f.user, story.UpdateInput{State: &started}); err != nil {
		t.Fatalf("bugs may be started unestimated: %v", err)
	}
}

func TestReorderWithinSection(t *testing.T) {
	f := setup(t)
	a := f.create(t, "A", story.SectionBacklog, 1)
	b := f.create(t, "B", story.SectionBacklog, 1)
	c := f.create(t, "C", story.SectionBacklog, 1)
	d := f.create(t, "D", story.SectionBacklog, 1)

	move := func(id int64, in story.MoveInput) {
		t.Helper()
		in.Section = story.SectionBacklog
		if _, err := f.svc.Move(f.ctx, id, in); err != nil {
			t.Fatal(err)
		}
	}

	move(d.ID, story.MoveInput{PrevID: &a.ID}) // between A and B
	assertOrder(t, f.titles(t, story.SectionBacklog), "A", "D", "B", "C")

	move(c.ID, story.MoveInput{}) // top
	assertOrder(t, f.titles(t, story.SectionBacklog), "C", "A", "D", "B")

	move(c.ID, story.MoveInput{PrevID: &b.ID}) // bottom
	assertOrder(t, f.titles(t, story.SectionBacklog), "A", "D", "B", "C")

	move(b.ID, story.MoveInput{NextID: &d.ID}) // before D
	assertOrder(t, f.titles(t, story.SectionBacklog), "A", "B", "D", "C")

	move(b.ID, story.MoveInput{PrevID: &a.ID}) // no-op move keeps order
	assertOrder(t, f.titles(t, story.SectionBacklog), "A", "B", "D", "C")

	// Only the dragged story is rewritten.
	after, _ := f.svc.Get(f.ctx, a.ID)
	if after.Position != a.Position {
		t.Fatalf("A's position changed from %d to %d", a.Position, after.Position)
	}
}

func TestMoveBetweenSections(t *testing.T) {
	f := setup(t)
	ice := f.create(t, "Ice", story.SectionIcebox, 1)
	b1 := f.create(t, "B1", story.SectionBacklog, 1)
	f.create(t, "B2", story.SectionBacklog, 1)

	res, err := f.svc.Move(f.ctx, ice.ID, story.MoveInput{Section: story.SectionBacklog, PrevID: &b1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Story.State != story.StateBacklog || res.Story.Section != story.SectionBacklog {
		t.Fatalf("state/section = %s/%s, want backlog", res.Story.State, res.Story.Section)
	}
	assertOrder(t, f.titles(t, story.SectionBacklog), "B1", "Ice", "B2")
	assertOrder(t, f.titles(t, story.SectionIcebox))

	res, err = f.svc.Move(f.ctx, ice.ID, story.MoveInput{Section: story.SectionCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if res.Story.State != story.StateUnstarted {
		t.Fatalf("state = %s, want unstarted", res.Story.State)
	}

	// Reordering inside current must not reset progress.
	f.setState(t, ice.ID, story.StateStarted)
	other := f.create(t, "Other", story.SectionCurrent, 1)
	res, err = f.svc.Move(f.ctx, ice.ID, story.MoveInput{Section: story.SectionCurrent, PrevID: &other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Story.State != story.StateStarted {
		t.Fatalf("state = %s, want started", res.Story.State)
	}
	assertOrder(t, f.titles(t, story.SectionCurrent), "Other", "Ice")
}

func TestMoveRejectsInvalidRequests(t *testing.T) {
	f := setup(t)
	ice := f.create(t, "Ice", story.SectionIcebox, 1)
	back := f.create(t, "Back", story.SectionBacklog, 1)
	wip := f.create(t, "WIP", story.SectionCurrent, 1)
	f.setState(t, wip.ID, story.StateStarted)

	cases := map[string]struct {
		id int64
		in story.MoveInput
	}{
		"unknown section":         {ice.ID, story.MoveInput{Section: "nowhere"}},
		"done is not a target":    {ice.ID, story.MoveInput{Section: story.SectionDone}},
		"neighbour in other list": {ice.ID, story.MoveInput{Section: story.SectionBacklog, PrevID: &wip.ID}},
		"neighbour is itself":     {back.ID, story.MoveInput{Section: story.SectionBacklog, PrevID: &back.ID}},
		"started leaves current":  {wip.ID, story.MoveInput{Section: story.SectionBacklog}},
	}
	for name, c := range cases {
		if _, err := f.svc.Move(f.ctx, c.id, c.in); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	// A failed move leaves everything in place.
	assertOrder(t, f.titles(t, story.SectionIcebox), "Ice")
	assertOrder(t, f.titles(t, story.SectionBacklog), "Back")

	f.setState(t, wip.ID, story.StateFinished, story.StateDelivered, story.StateAccepted)
	if _, err := f.svc.Move(f.ctx, wip.ID, story.MoveInput{Section: story.SectionCurrent}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Errorf("moving an accepted story: err = %v, want invalid", err)
	}
}

func TestNormalizationWhenGapIsExhausted(t *testing.T) {
	f := setup(t)
	a := f.create(t, "A", story.SectionBacklog, 1)
	x := f.create(t, "X", story.SectionBacklog, 1)
	y := f.create(t, "Y", story.SectionBacklog, 1)
	f.create(t, "B", story.SectionBacklog, 1)

	// Keep squeezing X and Y alternately into the slot right after A. Every
	// move halves the remaining gap, so a rebalance must happen eventually.
	renormalized := 0
	for i := 0; i < 40; i++ {
		id, want := x.ID, []string{"A", "X", "Y", "B"}
		if i%2 == 1 {
			id, want = y.ID, []string{"A", "Y", "X", "B"}
		}
		res, err := f.svc.Move(f.ctx, id, story.MoveInput{Section: story.SectionBacklog, PrevID: &a.ID})
		if err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		if res.Renormalized {
			renormalized++
		}
		assertOrder(t, f.titles(t, story.SectionBacklog), want...)
	}
	if renormalized == 0 {
		t.Fatal("expected at least one renormalization")
	}
	if renormalized > 4 {
		t.Fatalf("renormalized %d times in 40 moves; gaps are not being restored", renormalized)
	}

	// Positions stay strictly increasing.
	all, _ := f.svc.List(f.ctx, f.project.ID, story.ListOptions{})
	for i := 1; i < len(all); i++ {
		if all[i].Position <= all[i-1].Position {
			t.Fatalf("positions not strictly increasing: %d then %d", all[i-1].Position, all[i].Position)
		}
	}
}

func TestSectionsOrderIndependently(t *testing.T) {
	f := setup(t)
	i1 := f.create(t, "I1", story.SectionIcebox, 1)
	b1 := f.create(t, "B1", story.SectionBacklog, 1)
	c1 := f.create(t, "C1", story.SectionCurrent, 1)
	// Identical starting positions in all three scopes prove independence.
	if i1.Position != b1.Position || b1.Position != c1.Position {
		t.Fatalf("positions = %d, %d, %d; want equal", i1.Position, b1.Position, c1.Position)
	}
}

func TestUpdateFieldsLabelsAndSearch(t *testing.T) {
	f := setup(t)
	other := testutil.CreateUser(t, f.store, "kim@example.com")
	s := f.create(t, "Add OAuth support", story.SectionBacklog, 3)
	f.create(t, "Unrelated", story.SectionBacklog, 1)

	title, desc, typ := "Add SSO support", "Use the OIDC discovery document", story.TypeChore
	labels := []string{"auth"}
	got, err := f.svc.Update(f.ctx, s.ID, f.user, story.UpdateInput{
		Title: &title, Description: &desc, Type: &typ, OwnerID: story.Some(other), Labels: &labels,
		Estimate: story.Null[int64](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != title || got.Type != story.TypeChore || got.Estimate != nil || got.OwnerID == nil || *got.OwnerID != other || len(got.Labels) != 1 {
		t.Fatalf("unexpected story: %+v", got)
	}
	if got.Position != s.Position || got.State != s.State {
		t.Fatal("field edits must not touch ordering or state")
	}

	if _, err := f.svc.Update(f.ctx, s.ID, f.user, story.UpdateInput{OwnerID: story.Some(int64(999))}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Errorf("unknown owner: err = %v, want invalid", err)
	}

	// Removing the last use of a label removes the label.
	empty := []string{}
	if _, err := f.svc.Update(f.ctx, s.ID, f.user, story.UpdateInput{Labels: &empty}); err != nil {
		t.Fatal(err)
	}
	if names, _ := f.svc.Labels(f.ctx, f.project.ID); len(names) != 0 {
		t.Errorf("labels = %v, want none", names)
	}

	for query, want := range map[string]int{"sso": 1, "OIDC": 1, "support": 1, "nothing-matches": 0} {
		found, err := f.svc.List(f.ctx, f.project.ID, story.ListOptions{Query: query})
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != want {
			t.Errorf("search %q: %d results, want %d", query, len(found), want)
		}
	}
}

func TestCommentsAndDelete(t *testing.T) {
	f := setup(t)
	other := testutil.CreateUser(t, f.store, "kim@example.com")
	s := f.create(t, "Story", story.SectionIcebox, 1)

	c, err := f.svc.AddComment(f.ctx, s.ID, f.user, "  looks good  ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "looks good" {
		t.Fatalf("body = %q", c.Body)
	}
	if _, err := f.svc.AddComment(f.ctx, s.ID, f.user, "   "); apperr.KindOf(err) != apperr.KindInvalid {
		t.Errorf("empty comment: err = %v", err)
	}
	if _, err := f.svc.DeleteComment(f.ctx, c.ID, other); apperr.KindOf(err) != apperr.KindForbidden {
		t.Errorf("deleting someone else's comment: err = %v", err)
	}

	d, err := f.svc.Get(f.ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 1 || d.CommentCount != 1 {
		t.Fatalf("comments = %+v", d.Comments)
	}
	list, _ := f.svc.List(f.ctx, f.project.ID, story.ListOptions{})
	if list[0].CommentCount != 1 {
		t.Fatalf("comment_count in list = %d", list[0].CommentCount)
	}

	if err := f.svc.Delete(f.ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(f.ctx, s.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("get after delete: err = %v", err)
	}
	if err := f.svc.Delete(f.ctx, s.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("double delete: err = %v", err)
	}
}
