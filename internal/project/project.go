// Package project manages projects and their iteration settings.
package project

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
	"trackstar/internal/iteration"
)

const (
	DefaultIterationLengthDays = 7
	DefaultStartWeekday        = int64(time.Monday)
	DefaultVelocityWindow      = 3
)

type Project struct {
	ID                    int64  `json:"id"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	Slug                  string `json:"slug"`
	IterationLengthDays   int64  `json:"iteration_length_days"`
	IterationStartWeekday int64  `json:"iteration_start_weekday"` // 0 = Sunday … 6 = Saturday
	VelocityWindow        int64  `json:"velocity_window"`
	// EstimateBugsAndChores lets bugs and chores carry points that count
	// towards velocity. Off by default: only features are estimated.
	EstimateBugsAndChores bool `json:"estimate_bugs_and_chores"`
	// CombineIceboxBacklog is a view setting: the board shows the icebox at
	// the bottom of the backlog panel instead of in a panel of its own.
	CombineIceboxBacklog bool      `json:"combine_icebox_backlog"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	// ArchivedAt is set while the project is archived: kept, listed, but
	// read-only for everyone until an owner unarchives it.
	ArchivedAt *time.Time `json:"archived_at"`
}

// Archived reports whether the project is archived.
func (p Project) Archived() bool { return p.ArchivedAt != nil }

// Schedule returns the iteration schedule of the project.
func (p Project) Schedule(loc *time.Location) iteration.Schedule {
	return iteration.Schedule{
		LengthDays:   int(p.IterationLengthDays),
		StartWeekday: time.Weekday(p.IterationStartWeekday),
		Anchor:       p.CreatedAt,
		Location:     loc,
	}
}

func FromRow(r dbgen.Project) Project {
	p := Project{
		ID:                    r.ID,
		Name:                  r.Name,
		Description:           r.Description,
		Slug:                  r.Slug,
		IterationLengthDays:   r.IterationLengthDays,
		IterationStartWeekday: r.IterationStartWeekday,
		VelocityWindow:        r.VelocityWindow,
		EstimateBugsAndChores: r.EstimateBugsAndChores,
		CombineIceboxBacklog:  r.CombineIceboxBacklog,
		CreatedAt:             time.Unix(r.CreatedAt, 0).UTC(),
		UpdatedAt:             time.Unix(r.UpdatedAt, 0).UTC(),
	}
	if r.ArchivedAt.Valid {
		t := time.Unix(r.ArchivedAt.Int64, 0).UTC()
		p.ArchivedAt = &t
	}
	return p
}

// Input carries create/update fields; nil means "default" on create and
// "unchanged" on update.
type Input struct {
	Name                  *string `json:"name"`
	Description           *string `json:"description"`
	IterationLengthDays   *int64  `json:"iteration_length_days"`
	IterationStartWeekday *int64  `json:"iteration_start_weekday"`
	VelocityWindow        *int64  `json:"velocity_window"`
	EstimateBugsAndChores *bool   `json:"estimate_bugs_and_chores"`
	CombineIceboxBacklog  *bool   `json:"combine_icebox_backlog"`
}

type Service struct {
	store database.Store
	now   func() time.Time
}

func NewService(store database.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

// Create makes a project owned by ownerID, so it is visible only to its
// members from the start. An ownerID of 0 leaves the project without members,
// which keeps it open to everyone (as projects created before ownership
// existed are).
func (s *Service) Create(ctx context.Context, ownerID int64, in Input) (Project, error) {
	p := Project{
		IterationLengthDays:   DefaultIterationLengthDays,
		IterationStartWeekday: DefaultStartWeekday,
		VelocityWindow:        DefaultVelocityWindow,
	}
	if err := apply(&p, in); err != nil {
		return Project{}, err
	}

	var row dbgen.Project
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		slug, err := uniqueSlug(ctx, q, p.Name)
		if err != nil {
			return err
		}
		row, err = q.CreateProject(ctx, dbgen.CreateProjectParams{
			Name:                  p.Name,
			Description:           p.Description,
			Slug:                  slug,
			IterationLengthDays:   p.IterationLengthDays,
			IterationStartWeekday: p.IterationStartWeekday,
			VelocityWindow:        p.VelocityWindow,
			EstimateBugsAndChores: p.EstimateBugsAndChores,
			CombineIceboxBacklog:  p.CombineIceboxBacklog,
			Now:                   s.now().Unix(),
		})
		if err != nil || ownerID == 0 {
			return err
		}
		return q.UpsertProjectMember(ctx, dbgen.UpsertProjectMemberParams{ProjectID: row.ID, UserID: ownerID, Role: string(RoleOwner)})
	})
	if err != nil {
		return Project{}, err
	}
	return FromRow(row), nil
}

func (s *Service) Get(ctx context.Context, id int64) (Project, error) {
	return get(ctx, s.store, id)
}

// Resolve looks a project up by numeric id or by slug.
func (s *Service) Resolve(ctx context.Context, ref string) (Project, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.Get(ctx, id)
	}
	row, err := s.store.GetProjectBySlug(ctx, ref)
	if database.IsNotFound(err) {
		return Project{}, apperr.NotFound("project")
	}
	if err != nil {
		return Project{}, err
	}
	return FromRow(row), nil
}

func get(ctx context.Context, q dbgen.Querier, id int64) (Project, error) {
	row, err := q.GetProject(ctx, id)
	if database.IsNotFound(err) {
		return Project{}, apperr.NotFound("project")
	}
	if err != nil {
		return Project{}, err
	}
	return FromRow(row), nil
}

func (s *Service) List(ctx context.Context) ([]Project, error) {
	rows, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(rows))
	for i, r := range rows {
		out[i] = FromRow(r)
	}
	return out, nil
}

func (s *Service) Update(ctx context.Context, id int64, in Input) (Project, error) {
	var row dbgen.Project
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		p, err := get(ctx, q, id)
		if err != nil {
			return err
		}
		wasEstimating := p.EstimateBugsAndChores
		if err := apply(&p, in); err != nil {
			return err
		}
		now := s.now().Unix()
		row, err = q.UpdateProject(ctx, dbgen.UpdateProjectParams{
			ID:                    id,
			Name:                  p.Name,
			Description:           p.Description,
			IterationLengthDays:   p.IterationLengthDays,
			IterationStartWeekday: p.IterationStartWeekday,
			VelocityWindow:        p.VelocityWindow,
			EstimateBugsAndChores: p.EstimateBugsAndChores,
			CombineIceboxBacklog:  p.CombineIceboxBacklog,
			Now:                   now,
		})
		if err != nil {
			return err
		}
		// Points on bugs and chores exist only while the project allows
		// them; switching the option off takes them away again.
		if wasEstimating && !p.EstimateBugsAndChores {
			_, err = q.ClearNonFeatureEstimates(ctx, dbgen.ClearNonFeatureEstimatesParams{ProjectID: id, Now: now})
		}
		return err
	})
	if err != nil {
		return Project{}, err
	}
	return FromRow(row), nil
}

// SetArchived archives or unarchives a project. Archiving is idempotent and
// keeps the original archive time.
func (s *Service) SetArchived(ctx context.Context, id int64, archived bool) (Project, error) {
	var row dbgen.Project
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		p, err := get(ctx, q, id)
		if err != nil {
			return err
		}
		if p.Archived() == archived {
			row, err = q.GetProject(ctx, id)
			return err
		}
		at := sql.NullInt64{}
		if archived {
			at = sql.NullInt64{Int64: s.now().Unix(), Valid: true}
		}
		row, err = q.SetProjectArchived(ctx, dbgen.SetProjectArchivedParams{ID: id, ArchivedAt: at, Now: s.now().Unix()})
		return err
	})
	if err != nil {
		return Project{}, err
	}
	return FromRow(row), nil
}

// Delete removes the project for good, with every story, comment, label,
// epic, task, membership and saved filter in it (foreign keys cascade).
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.store.DeleteProject(ctx, id)
}

func apply(p *Project, in Input) error {
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		p.Description = strings.TrimSpace(*in.Description)
	}
	if in.IterationLengthDays != nil {
		p.IterationLengthDays = *in.IterationLengthDays
	}
	if in.IterationStartWeekday != nil {
		p.IterationStartWeekday = *in.IterationStartWeekday
	}
	if in.VelocityWindow != nil {
		p.VelocityWindow = *in.VelocityWindow
	}
	if in.EstimateBugsAndChores != nil {
		p.EstimateBugsAndChores = *in.EstimateBugsAndChores
	}
	if in.CombineIceboxBacklog != nil {
		p.CombineIceboxBacklog = *in.CombineIceboxBacklog
	}

	switch {
	case p.Name == "":
		return apperr.Invalid("project name is required")
	case len(p.Name) > 100:
		return apperr.Invalid("project name is too long")
	case p.IterationLengthDays < 7 || p.IterationLengthDays > 28 || p.IterationLengthDays%7 != 0:
		return apperr.Invalid("iteration length must be 7, 14, 21 or 28 days")
	case p.IterationStartWeekday < 0 || p.IterationStartWeekday > 6:
		return apperr.Invalid("iteration start weekday must be 0 (Sunday) to 6 (Saturday)")
	case p.VelocityWindow < 1 || p.VelocityWindow > 12:
		return apperr.Invalid("velocity window must be between 1 and 12 iterations")
	}
	return nil
}

// Slugify lower-cases name and collapses everything that is not a letter or
// digit into single dashes.
func trimSpaces(v string) string { return strings.Join(strings.Fields(v), " ") }

func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "project"
	}
	return b.String()
}

func uniqueSlug(ctx context.Context, q dbgen.Querier, name string) (string, error) {
	base := Slugify(name)
	slug := base
	for n := 2; ; n++ {
		_, err := q.GetProjectBySlug(ctx, slug)
		if database.IsNotFound(err) {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}
