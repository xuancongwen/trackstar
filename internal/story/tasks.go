package story

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database/dbgen"
)

var notFoundErr = sql.ErrNoRows

type Task struct {
	ID          int64     `json:"id"`
	StoryID     int64     `json:"story_id"`
	Description string    `json:"description"`
	Done        bool      `json:"done"`
	Position    int64     `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

type TaskInput struct {
	Description *string `json:"description"`
	Done        *bool   `json:"done"`
	// Position reorders within the story's task list (0-based index).
	Position *int `json:"position"`
}

func taskFromRow(r dbgen.Task) Task {
	return Task{ID: r.ID, StoryID: r.StoryID, Description: r.Description, Done: r.Done, Position: r.Position, CreatedAt: time.Unix(r.CreatedAt, 0).UTC()}
}

func (s *Service) Tasks(ctx context.Context, storyID int64) ([]Task, error) {
	rows, err := s.store.ListTasks(ctx, storyID)
	if err != nil {
		return nil, err
	}
	out := make([]Task, len(rows))
	for i, r := range rows {
		out[i] = taskFromRow(r)
	}
	return out, nil
}

// AddTask appends a task to a story's checklist.
func (s *Service) AddTask(ctx context.Context, storyID int64, description string) (Task, error) {
	description = strings.TrimSpace(description)
	if description == "" || len(description) > 500 {
		return Task{}, apperr.Invalid("task description must be 1 to 500 characters")
	}
	var row dbgen.Task
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		st, err := q.GetStory(ctx, storyID)
		if err != nil || st.DeletedAt.Valid {
			return notFound(errOrNotFound(err), "story")
		}
		maxPos, err := q.MaxTaskPosition(ctx, storyID)
		if err != nil {
			return err
		}
		now := s.now().Unix()
		row, err = q.CreateTask(ctx, dbgen.CreateTaskParams{StoryID: storyID, Description: description, Position: toInt64(maxPos) + 1, Now: now})
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return taskFromRow(row), nil
}

// UpdateTask edits, completes or reorders a task. StoryID is returned so the
// caller can publish the change.
func (s *Service) UpdateTask(ctx context.Context, id int64, in TaskInput) (Task, error) {
	var row dbgen.Task
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		t, err := q.GetTask(ctx, id)
		if err != nil {
			return notFound(err, "task")
		}
		if in.Description != nil {
			t.Description = strings.TrimSpace(*in.Description)
			if t.Description == "" || len(t.Description) > 500 {
				return apperr.Invalid("task description must be 1 to 500 characters")
			}
		}
		if in.Done != nil {
			t.Done = *in.Done
		}
		if in.Position != nil {
			// Renumber the whole list; task lists are short.
			all, err := q.ListTasks(ctx, t.StoryID)
			if err != nil {
				return err
			}
			all = slices.DeleteFunc(all, func(x dbgen.Task) bool { return x.ID == id })
			idx := max(0, min(*in.Position, len(all)))
			all = slices.Insert(all, idx, t)
			for i, x := range all {
				pos := int64(i + 1)
				if x.ID == id {
					t.Position = pos
					continue
				}
				if x.Position != pos {
					if _, err := q.UpdateTask(ctx, dbgen.UpdateTaskParams{ID: x.ID, Description: x.Description, Done: x.Done, Position: pos, Now: x.UpdatedAt}); err != nil {
						return err
					}
				}
			}
		}
		row, err = q.UpdateTask(ctx, dbgen.UpdateTaskParams{ID: id, Description: t.Description, Done: t.Done, Position: t.Position, Now: s.now().Unix()})
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return taskFromRow(row), nil
}

// DeleteTask removes a task and returns the story it belonged to.
func (s *Service) DeleteTask(ctx context.Context, id int64) (int64, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return 0, notFound(err, "task")
	}
	return t.StoryID, s.store.DeleteTask(ctx, id)
}

// setBlockers replaces the "blocked by" list. Blockers must be live stories
// of the same project, not the story itself, and must not create a cycle.
func setBlockers(ctx context.Context, q dbgen.Querier, story dbgen.Story, ids []int64) error {
	seen := map[int64]bool{}
	var clean []int64
	for _, id := range ids {
		if id == story.ID {
			return apperr.Invalid("a story cannot block itself")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		b, err := q.GetStory(ctx, id)
		if err != nil || b.DeletedAt.Valid || b.ProjectID != story.ProjectID {
			return apperr.Invalid("blocker #%d is not a story in this project", id)
		}
		if reaches(ctx, q, id, story.ID, 0) {
			return apperr.Invalid("#%d is already blocked by this story", id)
		}
		clean = append(clean, id)
	}
	if len(clean) > 20 {
		return apperr.Invalid("a story can list at most 20 blockers")
	}
	if err := q.ClearStoryBlockers(ctx, story.ID); err != nil {
		return err
	}
	for _, id := range clean {
		if err := q.AddStoryBlocker(ctx, dbgen.AddStoryBlockerParams{StoryID: story.ID, BlockerID: id}); err != nil {
			return err
		}
	}
	return nil
}

// reaches reports whether `from` is (transitively) blocked by `target`.
func reaches(ctx context.Context, q dbgen.Querier, from, target int64, depth int) bool {
	if depth > 50 {
		return true // treat runaway chains as cycles
	}
	blockers, err := q.ListStoryBlockers(ctx, from)
	if err != nil {
		return false
	}
	for _, b := range blockers {
		if b == target || reaches(ctx, q, b, target, depth+1) {
			return true
		}
	}
	return false
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case sql.NullInt64:
		return x.Int64
	case sql.NullFloat64:
		return int64(x.Float64)
	}
	return 0
}
