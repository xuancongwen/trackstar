package story_test

import (
	"testing"
	"time"

	"trackstar/internal/project"
	"trackstar/internal/story"
)

func TestProjectStatsAndRecentActivity(t *testing.T) {
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
	if feed, err := f.svc.RecentActivity(f.ctx, both); err != nil || len(feed) != 0 {
		t.Fatalf("feed of empty projects = %+v, %v", feed, err)
	}
	if stats, err := f.svc.ProjectStats(f.ctx, nil); err != nil || stats == nil || len(stats) != 0 {
		t.Fatalf("stats of no projects = %#v, %v", stats, err)
	}
	if feed, err := f.svc.RecentActivity(f.ctx, nil); err != nil || feed == nil || len(feed) != 0 {
		t.Fatalf("feed of no projects = %#v, %v", feed, err)
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

	feed, err := f.svc.RecentActivity(f.ctx, both)
	if err != nil {
		t.Fatal(err)
	}
	first := feed[0]
	if first.Kind != story.FeedKindComment || first.Body != "Looks good" || first.StoryID != a.ID || first.StoryTitle != "A" || first.ProjectID != f.project.ID || first.UserID != f.user {
		t.Fatalf("newest entry = %+v, want the comment on A", first)
	}
	kinds := map[string]int{}
	for i, e := range feed {
		kinds[e.Kind]++
		if i > 0 && e.CreatedAt.After(feed[i-1].CreatedAt) {
			t.Fatalf("feed is not newest first at %d: %+v", i, feed)
		}
		if e.Kind == "title" || e.Kind == "type" {
			t.Fatalf("an edit made the feed: %+v", e)
		}
		if e.StoryID == d.ID && e.StoryTitle != renamed {
			t.Fatalf("entry carries a stale title: %+v", e)
		}
	}
	// 5 created; states: A 1, B 3, C 4, trashed 1; 1 deleted; 1 comment.
	if kinds["created"] != 5 || kinds["state"] != 9 || kinds["deleted"] != 1 || kinds[story.FeedKindComment] != 1 {
		t.Fatalf("kinds = %v", kinds)
	}
	// Within the same second the later change comes first.
	if feed[1].Kind != "deleted" || feed[1].StoryTitle != "Trashed" {
		t.Fatalf("second entry = %+v, want the deletion", feed[1])
	}

	// Other projects stay out.
	if elsewhere, _ := f.svc.RecentActivity(f.ctx, []int64{quiet.ID}); len(elsewhere) != 0 {
		t.Fatalf("feed leaked across projects: %+v", elsewhere)
	}
	if elsewhere, _ := f.svc.ProjectStats(f.ctx, []int64{quiet.ID}); len(elsewhere) != 0 {
		t.Fatalf("stats leaked across projects: %+v", elsewhere)
	}

	// The feed keeps only the newest FeedSize entries, comments included.
	f.clock.Advance(time.Hour)
	for range story.FeedSize + 5 {
		f.create(t, "Filler", story.SectionIcebox, 0)
	}
	f.clock.Advance(time.Hour)
	if _, err := f.svc.AddComment(f.ctx, b.ID, f.user, "Newest"); err != nil {
		t.Fatal(err)
	}
	feed, _ = f.svc.RecentActivity(f.ctx, both)
	if len(feed) != story.FeedSize || feed[0].Body != "Newest" || feed[1].StoryTitle != "Filler" || feed[story.FeedSize-1].StoryTitle != "Filler" {
		t.Fatalf("limited feed: %d entries, first %+v, last %+v", len(feed), feed[0], feed[len(feed)-1])
	}

	// A comment on a story that was then trashed drops out of the feed.
	if _, err := f.svc.Delete(f.ctx, b.ID, f.user); err != nil {
		t.Fatal(err)
	}
	feed, _ = f.svc.RecentActivity(f.ctx, both)
	for _, e := range feed {
		if e.Kind == story.FeedKindComment {
			t.Fatalf("comment on a trashed story in the feed: %+v", e)
		}
	}
}
