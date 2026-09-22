// Package velocity derives iteration history and velocity from accepted
// stories. All of the arithmetic happens here in Go; the database only
// returns the accepted stories for a time range.
package velocity

import (
	"context"
	"database/sql"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
	"trackstar/internal/iteration"
	"trackstar/internal/project"
	"trackstar/internal/story"
)

// DefaultVelocity is assumed until a project has completed an iteration, so
// backlog projections are useful from day one.
const DefaultVelocity = 10

type IterationPoints struct {
	Number int `json:"number"`
	Points int `json:"points"`
}

type Result struct {
	// Velocity is the average rounded down, as planning should be conservative.
	Velocity int     `json:"velocity"`
	Average  float64 `json:"average"`
	Window   int     `json:"window"`
	// Estimated is true while there is no completed iteration to measure.
	Estimated  bool              `json:"estimated"`
	Iterations []IterationPoints `json:"iterations"`
}

// IterationSummary is one row of the iteration history.
type IterationSummary struct {
	iteration.Iteration
	Current         bool `json:"current"`
	Points          int  `json:"points"`
	AcceptedStories int  `json:"accepted_stories"`
}

type Service struct {
	store database.Store
	loc   *time.Location
	now   func() time.Time
}

func NewService(store database.Store, loc *time.Location, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, loc: loc, now: now}
}

// Velocity averages accepted feature points over the last N completed
// iterations (N = the project's velocity window). A young project with fewer
// than N completed iterations is averaged over the ones it has.
func (s *Service) Velocity(ctx context.Context, projectID int64) (Result, error) {
	p, sched, err := s.schedule(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	current := sched.At(s.now())
	window := int(p.VelocityWindow)
	res := Result{Window: window, Iterations: []IterationPoints{}}

	first := max(1, current.Number-window)
	completed := sched.Range(first, current.Number-1)
	if len(completed) == 0 {
		res.Velocity, res.Average, res.Estimated = DefaultVelocity, DefaultVelocity, true
		return res, nil
	}

	buckets, err := s.buckets(ctx, projectID, sched, completed[0].StartAt, current.StartAt)
	if err != nil {
		return Result{}, err
	}
	total := 0
	for _, it := range completed {
		points := buckets[it.Number].points
		total += points
		res.Iterations = append(res.Iterations, IterationPoints{Number: it.Number, Points: points})
	}
	res.Velocity = total / len(completed)
	res.Average = float64(total) / float64(len(completed))
	return res, nil
}

// Iterations lists every iteration from the first up to the current one.
func (s *Service) Iterations(ctx context.Context, projectID int64) ([]IterationSummary, error) {
	_, sched, err := s.schedule(ctx, projectID)
	if err != nil {
		return nil, err
	}
	current := sched.At(s.now())
	buckets, err := s.buckets(ctx, projectID, sched, sched.Number(1).StartAt, current.EndAt)
	if err != nil {
		return nil, err
	}
	out := make([]IterationSummary, 0, current.Number)
	for _, it := range sched.Range(1, current.Number) {
		b := buckets[it.Number]
		out = append(out, IterationSummary{
			Iteration:       it,
			Current:         it.Number == current.Number,
			Points:          b.points,
			AcceptedStories: b.stories,
		})
	}
	return out, nil
}

type bucket struct{ points, stories int }

// buckets groups stories accepted in [from, to) by iteration number. Only
// features contribute points; bugs and chores are counted as stories.
func (s *Service) buckets(ctx context.Context, projectID int64, sched iteration.Schedule, from, to time.Time) (map[int]bucket, error) {
	rows, err := s.store.ListAcceptedStories(ctx, dbgen.ListAcceptedStoriesParams{
		ProjectID:      projectID,
		AcceptedFrom:   sql.NullInt64{Int64: from.Unix(), Valid: true},
		AcceptedBefore: sql.NullInt64{Int64: to.Unix(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	out := map[int]bucket{}
	for _, r := range rows {
		n := sched.At(time.Unix(r.AcceptedAt.Int64, 0)).Number
		b := out[n]
		b.stories++
		if story.Type(r.Type) == story.TypeFeature && r.Estimate.Valid {
			b.points += int(r.Estimate.Int64)
		}
		out[n] = b
	}
	return out, nil
}

func (s *Service) schedule(ctx context.Context, projectID int64) (project.Project, iteration.Schedule, error) {
	row, err := s.store.GetProject(ctx, projectID)
	if database.IsNotFound(err) {
		return project.Project{}, iteration.Schedule{}, apperr.NotFound("project")
	}
	if err != nil {
		return project.Project{}, iteration.Schedule{}, err
	}
	p := project.FromRow(row)
	return p, p.Schedule(s.loc), nil
}
