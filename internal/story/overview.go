package story

import (
	"context"
	"sort"
	"time"

	"trackstar/internal/database/dbgen"
)

// ProjectStats is what the projects page shows about one project.
type ProjectStats struct {
	ProjectID int64 `json:"project_id"`
	// InProgress counts started, finished and rejected stories: work somebody
	// is on. ToAccept counts delivered ones: work waiting for a decision.
	InProgress int64 `json:"in_progress"`
	ToAccept   int64 `json:"to_accept"`
	// LastActivityAt is the latest story change or comment; nil for a
	// project that never had a story.
	LastActivityAt *time.Time `json:"last_activity_at"`
	// Activity is how many story changes and comments each of the last
	// ActivityDays days saw, oldest first; the last entry is today.
	Activity []int64 `json:"activity"`
}

// ActivityDays is how far back ProjectStats.Activity goes.
const ActivityDays = 30

// FeedKindComment marks a FeedEntry that is a comment rather than a change.
const FeedKindComment = "comment"

// FeedSize is how many entries RecentActivity returns at most. The
// RecentActivity and RecentComments queries carry the same number. It is
// deeper than the page shows, because the page folds runs of similar entries.
const FeedSize = 60

// FeedEntry is one line of the cross-project activity feed: an Activity
// with enough of its story to show it out of context, or a comment.
type FeedEntry struct {
	// ID is unique only within its kind family (comments and changes are
	// numbered separately).
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"` // an Activity kind, or "comment"
	ProjectID  int64     `json:"project_id"`
	StoryID    int64     `json:"story_id"`
	StoryTitle string    `json:"story_title"`
	UserID     int64     `json:"user_id"`
	OldValue   string    `json:"old_value"`
	NewValue   string    `json:"new_value"`
	Body       string    `json:"body,omitempty"` // comments only
	CreatedAt  time.Time `json:"created_at"`
}

// ProjectStats summarises each of the given projects. Projects without any
// story are left out. The caller decides which projects the user may see.
func (s *Service) ProjectStats(ctx context.Context, projectIDs []int64) ([]ProjectStats, error) {
	if len(projectIDs) == 0 {
		return []ProjectStats{}, nil
	}
	rows, err := s.store.ProjectStoryStats(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	comments, err := s.store.ProjectLastComment(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	lastComment := make(map[int64]int64, len(comments))
	for _, c := range comments {
		lastComment[c.ProjectID] = c.LastCommentAt
	}
	activity, err := s.activityByDay(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectStats, len(rows))
	for i, r := range rows {
		at := time.Unix(max(r.LastChangedAt, lastComment[r.ProjectID]), 0).UTC()
		days := activity[r.ProjectID]
		if days == nil {
			days = make([]int64, ActivityDays)
		}
		out[i] = ProjectStats{ProjectID: r.ProjectID, InProgress: r.InProgress, ToAccept: r.ToAccept, LastActivityAt: &at, Activity: days}
	}
	return out, nil
}

// activityByDay counts what the feed would show, per project and per day of
// the last ActivityDays days. Days are cut at midnight in the service's time
// zone, 24 hours apiece.
func (s *Service) activityByDay(ctx context.Context, projectIDs []int64) (map[int64][]int64, error) {
	now := s.now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	since := today.AddDate(0, 0, -(ActivityDays - 1)).Unix()

	out := map[int64][]int64{}
	add := func(projectID, day, n int64) {
		if out[projectID] == nil {
			out[projectID] = make([]int64, ActivityDays)
		}
		// A day with a clock change is not 24 hours long, so the last hour of
		// the window can land one slot past the end.
		out[projectID][min(max(day, 0), ActivityDays-1)] += n
	}
	changes, err := s.store.ProjectActivityByDay(ctx, dbgen.ProjectActivityByDayParams{Since: since, ProjectIds: projectIDs})
	if err != nil {
		return nil, err
	}
	for _, r := range changes {
		add(r.ProjectID, r.Day, r.Changes)
	}
	comments, err := s.store.ProjectCommentsByDay(ctx, dbgen.ProjectCommentsByDayParams{Since: since, ProjectIds: projectIDs})
	if err != nil {
		return nil, err
	}
	for _, r := range comments {
		add(r.ProjectID, r.Day, r.Comments)
	}
	return out, nil
}

// RecentActivity returns the newest FeedSize story changes and comments
// across the given projects, newest first.
func (s *Service) RecentActivity(ctx context.Context, projectIDs []int64) ([]FeedEntry, error) {
	if len(projectIDs) == 0 {
		return []FeedEntry{}, nil
	}
	changes, err := s.store.RecentActivity(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	comments, err := s.store.RecentComments(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	out := make([]FeedEntry, 0, len(changes)+len(comments))
	for _, a := range changes {
		out = append(out, FeedEntry{
			ID: a.ID, Kind: a.Kind, ProjectID: a.ProjectID, StoryID: a.StoryID, StoryTitle: a.StoryTitle,
			UserID: a.UserID, OldValue: a.OldValue, NewValue: a.NewValue, CreatedAt: time.Unix(a.CreatedAt, 0).UTC(),
		})
	}
	for _, c := range comments {
		out = append(out, FeedEntry{
			ID: c.ID, Kind: FeedKindComment, ProjectID: c.ProjectID, StoryID: c.StoryID, StoryTitle: c.StoryTitle,
			UserID: c.UserID, Body: c.Body, CreatedAt: time.Unix(c.CreatedAt, 0).UTC(),
		})
	}
	// Each list arrives newest first by id; the stable sort keeps that order
	// within the same second.
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > FeedSize {
		out = out[:FeedSize]
	}
	return out, nil
}
