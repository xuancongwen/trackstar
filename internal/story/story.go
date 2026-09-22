// Package story implements stories, their workflow and their ordering.
package story

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"

	"tracker/internal/apperr"
	"tracker/internal/database"
	"tracker/internal/database/dbgen"
	"tracker/internal/project"
)

type Story struct {
	ID           int64      `json:"id"`
	ProjectID    int64      `json:"project_id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Type         Type       `json:"type"`
	State        State      `json:"state"`
	Section      Section    `json:"section"`
	Estimate     *int64     `json:"estimate"`
	Position     int64      `json:"position"`
	RequesterID  int64      `json:"requester_id"`
	OwnerID      *int64     `json:"owner_id"`
	Labels       []string   `json:"labels"`
	CommentCount int64      `json:"comment_count"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	AcceptedAt   *time.Time `json:"accepted_at"`
}

type Comment struct {
	ID        int64     `json:"id"`
	StoryID   int64     `json:"story_id"`
	UserID    int64     `json:"user_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Detail is a story together with its comments.
type Detail struct {
	Story
	Comments []Comment `json:"comments"`
}

type CreateInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        Type     `json:"type"`
	Estimate    *int64   `json:"estimate"`
	Section     Section  `json:"section"` // icebox (default), backlog or current
	OwnerID     *int64   `json:"owner_id"`
	Labels      []string `json:"labels"`
}

// UpdateInput is a partial update; unset fields stay untouched.
type UpdateInput struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	Type        *Type      `json:"type"`
	State       *State     `json:"state"`
	Estimate    Opt[int64] `json:"estimate"`
	OwnerID     Opt[int64] `json:"owner_id"`
	RequesterID *int64     `json:"requester_id"`
	Labels      *[]string  `json:"labels"`
}

// MoveInput places a story in a section relative to a neighbour: after
// PrevID when given, otherwise before NextID, otherwise at the top.
type MoveInput struct {
	Section Section `json:"section"`
	PrevID  *int64  `json:"prev_id"`
	NextID  *int64  `json:"next_id"`
}

type MoveResult struct {
	Story Story `json:"story"`
	// Renormalized tells clients that other stories in the section received
	// new positions, so cached positions should be refreshed.
	Renormalized bool `json:"renormalized"`
}

type ListOptions struct {
	Query string // search in title and description; includes done stories
	Done  bool   // list stories accepted before the current iteration
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
	if loc == nil {
		loc = time.UTC
	}
	return &Service{store: store, loc: loc, now: now}
}

func (s *Service) Create(ctx context.Context, projectID, actorID int64, in CreateInput) (Story, error) {
	title := strings.TrimSpace(in.Title)
	if in.Type == "" {
		in.Type = TypeFeature
	}
	if in.Section == "" {
		in.Section = SectionIcebox
	}
	switch {
	case title == "":
		return Story{}, apperr.Invalid("title is required")
	case len(title) > 500:
		return Story{}, apperr.Invalid("title is too long")
	case !in.Type.Valid():
		return Story{}, apperr.Invalid("type must be feature, bug or chore")
	case in.Estimate != nil && !validEstimate(*in.Estimate):
		return Story{}, apperr.Invalid("estimate must be one of 0, 1, 2, 3, 5, 8")
	case in.Section != SectionIcebox && in.Section != SectionBacklog && in.Section != SectionCurrent:
		return Story{}, apperr.Invalid("section must be icebox, backlog or current")
	}

	var out Story
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "project")
		}
		if err := requireUser(ctx, q, in.OwnerID, "owner"); err != nil {
			return err
		}
		// New ideas land on top of the icebox where they get noticed; adding
		// to backlog/current must not jump the queue, so those go last.
		where := placement{bottom: in.Section != SectionIcebox}
		pos, _, err := place(ctx, q, projectID, in.Section, 0, where)
		if err != nil {
			return err
		}
		row, err := q.CreateStory(ctx, dbgen.CreateStoryParams{
			ProjectID:   projectID,
			Title:       title,
			Description: in.Description,
			Type:        string(in.Type),
			State:       string(entryState(in.Section)),
			Estimate:    nullInt(in.Estimate),
			Position:    pos,
			RequesterID: actorID,
			OwnerID:     nullInt(in.OwnerID),
			Now:         s.now().Unix(),
		})
		if err != nil {
			return err
		}
		if err := setLabels(ctx, q, projectID, row.ID, in.Labels); err != nil {
			return err
		}
		out, err = s.load(ctx, q, row)
		return err
	})
	return out, err
}

func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	row, err := s.store.GetStory(ctx, id)
	if err != nil {
		return Detail{}, notFound(err, "story")
	}
	st, err := s.load(ctx, s.store, row)
	if err != nil {
		return Detail{}, err
	}
	rows, err := s.store.ListComments(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Story: st, Comments: make([]Comment, len(rows))}
	for i, c := range rows {
		d.Comments[i] = commentFromRow(c)
	}
	d.CommentCount = int64(len(rows))
	return d, nil
}

func (s *Service) List(ctx context.Context, projectID int64, opts ListOptions) ([]Story, error) {
	currentStart, err := s.currentIterationStart(ctx, s.store, projectID)
	if err != nil {
		return nil, err
	}

	var rows []dbgen.Story
	switch query := strings.ToLower(strings.TrimSpace(opts.Query)); {
	case query != "":
		rows, err = s.store.SearchStories(ctx, dbgen.SearchStoriesParams{ProjectID: projectID, Pattern: "%" + query + "%"})
	case opts.Done:
		rows, err = s.store.ListAcceptedStories(ctx, dbgen.ListAcceptedStoriesParams{
			ProjectID:      projectID,
			AcceptedFrom:   sql.NullInt64{Int64: 0, Valid: true},
			AcceptedBefore: sql.NullInt64{Int64: currentStart.Unix(), Valid: true},
		})
	default:
		rows, err = s.store.ListActiveStories(ctx, dbgen.ListActiveStoriesParams{
			ProjectID:     projectID,
			AcceptedSince: sql.NullInt64{Int64: currentStart.Unix(), Valid: true},
		})
	}
	if err != nil {
		return nil, err
	}

	labelRows, err := s.store.ListProjectStoryLabels(ctx, projectID)
	if err != nil {
		return nil, err
	}
	labels := map[int64][]string{}
	for _, l := range labelRows {
		labels[l.StoryID] = append(labels[l.StoryID], l.Name)
	}
	countRows, err := s.store.CountCommentsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int64{}
	for _, c := range countRows {
		counts[c.StoryID] = c.Total
	}

	out := make([]Story, len(rows))
	for i, r := range rows {
		out[i] = fromRow(r, currentStart)
		if l := labels[r.ID]; l != nil {
			out[i].Labels = l
		}
		out[i].CommentCount = counts[r.ID]
	}
	return out, nil
}

func (s *Service) Update(ctx context.Context, id, actorID int64, in UpdateInput) (Story, error) {
	var out Story
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		row, err := q.GetStory(ctx, id)
		if err != nil {
			return notFound(err, "story")
		}
		accepted := State(row.State) == StateAccepted

		if in.Title != nil {
			row.Title = strings.TrimSpace(*in.Title)
			if row.Title == "" || len(row.Title) > 500 {
				return apperr.Invalid("title must be 1 to 500 characters")
			}
		}
		if in.Description != nil {
			row.Description = *in.Description
		}
		if in.Type != nil && string(*in.Type) != row.Type {
			if !in.Type.Valid() {
				return apperr.Invalid("type must be feature, bug or chore")
			}
			if accepted {
				return apperr.Invalid("an accepted story's type cannot change")
			}
			row.Type = string(*in.Type)
		}
		if in.Estimate.Set && !equalNullInt(row.Estimate, in.Estimate.Value) {
			if in.Estimate.Value != nil && !validEstimate(*in.Estimate.Value) {
				return apperr.Invalid("estimate must be one of 0, 1, 2, 3, 5, 8")
			}
			if accepted {
				return apperr.Invalid("an accepted story's estimate cannot change")
			}
			row.Estimate = nullInt(in.Estimate.Value)
		}
		if in.OwnerID.Set {
			if err := requireUser(ctx, q, in.OwnerID.Value, "owner"); err != nil {
				return err
			}
			row.OwnerID = nullInt(in.OwnerID.Value)
		}
		if in.RequesterID != nil {
			if err := requireUser(ctx, q, in.RequesterID, "requester"); err != nil {
				return err
			}
			row.RequesterID = *in.RequesterID
		}

		if in.State != nil && string(*in.State) != row.State {
			from, to := State(row.State), *in.State
			if !to.Valid() {
				return apperr.Invalid("unknown state %q", to)
			}
			if !CanTransition(from, to) {
				return apperr.Invalid("a story cannot go from %s to %s", from, to)
			}
			if SectionOf(to) != SectionOf(from) {
				pos, _, err := place(ctx, q, row.ProjectID, SectionOf(to), row.ID, placement{bottom: true})
				if err != nil {
					return err
				}
				row.Position = pos
			}
			if to == StateStarted && !row.OwnerID.Valid {
				row.OwnerID = sql.NullInt64{Int64: actorID, Valid: true}
			}
			if to == StateAccepted {
				row.AcceptedAt = sql.NullInt64{Int64: s.now().Unix(), Valid: true}
			}
			row.State = string(to)
		}

		if Type(row.Type) == TypeFeature && !row.Estimate.Valid && (inProgress(State(row.State)) || State(row.State) == StateAccepted) {
			return apperr.Invalid("a feature needs an estimate before it can be started")
		}

		if in.Labels != nil {
			if err := setLabels(ctx, q, row.ProjectID, row.ID, *in.Labels); err != nil {
				return err
			}
		}
		out, err = s.save(ctx, q, row)
		return err
	})
	return out, err
}

// Move reorders a story and/or drags it into another section. State and
// position change in one transaction.
func (s *Service) Move(ctx context.Context, id int64, in MoveInput) (MoveResult, error) {
	if in.Section != SectionIcebox && in.Section != SectionBacklog && in.Section != SectionCurrent {
		return MoveResult{}, apperr.Invalid("section must be icebox, backlog or current")
	}
	var res MoveResult
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		row, err := q.GetStory(ctx, id)
		if err != nil {
			return notFound(err, "story")
		}
		state := State(row.State)
		switch {
		case state == StateAccepted:
			return apperr.Invalid("accepted stories cannot be moved")
		case inProgress(state) && in.Section != SectionCurrent:
			return apperr.Invalid("a %s story stays in the current iteration", state)
		}
		if SectionOf(state) != in.Section {
			row.State = string(entryState(in.Section))
		}
		pos, renormalized, err := place(ctx, q, row.ProjectID, in.Section, row.ID, placement{prevID: in.PrevID, nextID: in.NextID})
		if err != nil {
			return err
		}
		row.Position = pos
		res.Renormalized = renormalized
		res.Story, err = s.save(ctx, q, row)
		return err
	})
	return res, err
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.store.InTx(ctx, func(q dbgen.Querier) error {
		row, err := q.GetStory(ctx, id)
		if err != nil {
			return notFound(err, "story")
		}
		if err := q.DeleteStory(ctx, id); err != nil {
			return err
		}
		return q.DeleteUnusedLabels(ctx, row.ProjectID)
	})
}

func (s *Service) AddComment(ctx context.Context, storyID, actorID int64, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, apperr.Invalid("comment body is required")
	}
	if _, err := s.store.GetStory(ctx, storyID); err != nil {
		return Comment{}, notFound(err, "story")
	}
	row, err := s.store.CreateComment(ctx, dbgen.CreateCommentParams{
		StoryID: storyID, UserID: actorID, Body: body, Now: s.now().Unix(),
	})
	if err != nil {
		return Comment{}, err
	}
	return commentFromRow(row), nil
}

// DeleteComment removes a comment; only its author may do so. It returns the
// id of the story the comment belonged to.
func (s *Service) DeleteComment(ctx context.Context, commentID, actorID int64) (int64, error) {
	c, err := s.store.GetComment(ctx, commentID)
	if err != nil {
		return 0, notFound(err, "comment")
	}
	if c.UserID != actorID {
		return 0, apperr.Forbidden("only the author can delete a comment")
	}
	return c.StoryID, s.store.DeleteComment(ctx, commentID)
}

func (s *Service) Labels(ctx context.Context, projectID int64) ([]string, error) {
	rows, err := s.store.ListLabels(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out, nil
}

// --- ordering ---------------------------------------------------------------

type placement struct {
	prevID, nextID *int64
	bottom         bool
}

// place computes the position for story movingID inside a section. It reads
// the section's real neighbours inside the caller's transaction rather than
// trusting client-side positions, and rebalances the section when the gap
// between the neighbours is used up.
func place(ctx context.Context, q dbgen.Querier, projectID int64, sec Section, movingID int64, where placement) (pos int64, renormalized bool, err error) {
	rows, err := q.ListSectionPositions(ctx, dbgen.ListSectionPositionsParams{ProjectID: projectID, States: orderedStates(sec)})
	if err != nil {
		return 0, false, err
	}
	rows = slices.DeleteFunc(rows, func(r dbgen.ListSectionPositionsRow) bool { return r.ID == movingID })
	indexOf := func(id int64) int {
		return slices.IndexFunc(rows, func(r dbgen.ListSectionPositionsRow) bool { return r.ID == id })
	}

	index := 0
	switch {
	case where.prevID != nil:
		i := indexOf(*where.prevID)
		if i < 0 {
			return 0, false, apperr.Invalid("prev_id %d is not in the %s section", *where.prevID, sec)
		}
		index = i + 1
	case where.nextID != nil:
		i := indexOf(*where.nextID)
		if i < 0 {
			return 0, false, apperr.Invalid("next_id %d is not in the %s section", *where.nextID, sec)
		}
		index = i
	case where.bottom:
		index = len(rows)
	}

	neighbours := func() (prev, next *int64) {
		if index > 0 {
			prev = &rows[index-1].Position
		}
		if index < len(rows) {
			next = &rows[index].Position
		}
		return prev, next
	}

	if pos, ok := Between(neighbours()); ok {
		return pos, false, nil
	}
	for i, p := range Normalize(len(rows)) {
		if err := q.SetStoryPosition(ctx, dbgen.SetStoryPositionParams{ID: rows[i].ID, Position: p}); err != nil {
			return 0, false, err
		}
		rows[i].Position = p
	}
	pos, _ = Between(neighbours())
	return pos, true, nil
}

// --- helpers ----------------------------------------------------------------

func (s *Service) save(ctx context.Context, q dbgen.Querier, row dbgen.Story) (Story, error) {
	updated, err := q.UpdateStory(ctx, dbgen.UpdateStoryParams{
		ID:          row.ID,
		Title:       row.Title,
		Description: row.Description,
		Type:        row.Type,
		State:       row.State,
		Estimate:    row.Estimate,
		Position:    row.Position,
		RequesterID: row.RequesterID,
		OwnerID:     row.OwnerID,
		AcceptedAt:  row.AcceptedAt,
		Now:         s.now().Unix(),
	})
	if err != nil {
		return Story{}, err
	}
	return s.load(ctx, q, updated)
}

// load turns a row into a Story with labels and section filled in.
func (s *Service) load(ctx context.Context, q dbgen.Querier, row dbgen.Story) (Story, error) {
	currentStart, err := s.currentIterationStart(ctx, q, row.ProjectID)
	if err != nil {
		return Story{}, err
	}
	st := fromRow(row, currentStart)
	labels, err := q.ListStoryLabels(ctx, row.ID)
	if err != nil {
		return Story{}, err
	}
	st.Labels = labels
	return st, nil
}

func (s *Service) currentIterationStart(ctx context.Context, q dbgen.Querier, projectID int64) (time.Time, error) {
	row, err := q.GetProject(ctx, projectID)
	if err != nil {
		return time.Time{}, notFound(err, "project")
	}
	return project.FromRow(row).Schedule(s.loc).At(s.now()).StartAt, nil
}

func fromRow(r dbgen.Story, currentStart time.Time) Story {
	st := Story{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Title:       r.Title,
		Description: r.Description,
		Type:        Type(r.Type),
		State:       State(r.State),
		Section:     SectionOf(State(r.State)),
		Position:    r.Position,
		RequesterID: r.RequesterID,
		Labels:      []string{},
		CreatedAt:   time.Unix(r.CreatedAt, 0).UTC(),
		UpdatedAt:   time.Unix(r.UpdatedAt, 0).UTC(),
	}
	if r.Estimate.Valid {
		st.Estimate = &r.Estimate.Int64
	}
	if r.OwnerID.Valid {
		st.OwnerID = &r.OwnerID.Int64
	}
	if r.AcceptedAt.Valid {
		at := time.Unix(r.AcceptedAt.Int64, 0).UTC()
		st.AcceptedAt = &at
		if at.Before(currentStart) {
			st.Section = SectionDone
		}
	}
	return st
}

func commentFromRow(r dbgen.Comment) Comment {
	return Comment{
		ID: r.ID, StoryID: r.StoryID, UserID: r.UserID, Body: r.Body,
		CreatedAt: time.Unix(r.CreatedAt, 0).UTC(), UpdatedAt: time.Unix(r.UpdatedAt, 0).UTC(),
	}
}

func setLabels(ctx context.Context, q dbgen.Querier, projectID, storyID int64, names []string) error {
	seen := map[string]bool{}
	var clean []string
	for _, n := range names {
		n = strings.ToLower(strings.Join(strings.Fields(n), " "))
		if n == "" || seen[n] {
			continue
		}
		if len(n) > 50 {
			return apperr.Invalid("label %q is too long", n)
		}
		seen[n] = true
		clean = append(clean, n)
	}
	if len(clean) > 20 {
		return apperr.Invalid("a story can have at most 20 labels")
	}

	if err := q.ClearStoryLabels(ctx, storyID); err != nil {
		return err
	}
	for _, n := range clean {
		label, err := q.GetLabelByName(ctx, dbgen.GetLabelByNameParams{ProjectID: projectID, Name: n})
		if database.IsNotFound(err) {
			label, err = q.CreateLabel(ctx, dbgen.CreateLabelParams{ProjectID: projectID, Name: n})
		}
		if err != nil {
			return err
		}
		if err := q.AddStoryLabel(ctx, dbgen.AddStoryLabelParams{StoryID: storyID, LabelID: label.ID}); err != nil {
			return err
		}
	}
	return q.DeleteUnusedLabels(ctx, projectID)
}

func requireUser(ctx context.Context, q dbgen.Querier, id *int64, role string) error {
	if id == nil {
		return nil
	}
	if _, err := q.GetUser(ctx, *id); err != nil {
		if database.IsNotFound(err) {
			return apperr.Invalid("%s %d does not exist", role, *id)
		}
		return err
	}
	return nil
}

func notFound(err error, what string) error {
	if database.IsNotFound(err) {
		return apperr.NotFound(what)
	}
	return err
}

func nullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func equalNullInt(a sql.NullInt64, b *int64) bool {
	if b == nil {
		return !a.Valid
	}
	return a.Valid && a.Int64 == *b
}
