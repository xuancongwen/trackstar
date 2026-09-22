package project

import (
	"context"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
)

// Role of a project member. A project with no members is open to everyone.
type Role string

const (
	RoleMember Role = "member" // read and write
	RoleViewer Role = "viewer" // read only
)

func (r Role) Valid() bool { return r == RoleMember || r == RoleViewer }

type Member struct {
	UserID int64 `json:"user_id"`
	Role   Role  `json:"role"`
}

// Access is what a user may do in a project.
type Access struct {
	Read  bool
	Write bool
}

// AccessFor resolves a user's access. Administrators always have full access;
// otherwise a project without members is open to all, and with members only
// they may see it (viewers read-only).
func (s *Service) AccessFor(ctx context.Context, projectID, userID int64, isAdmin bool) (Access, error) {
	if isAdmin {
		return Access{Read: true, Write: true}, nil
	}
	n, err := s.store.CountProjectMembers(ctx, projectID)
	if err != nil {
		return Access{}, err
	}
	if n == 0 {
		return Access{Read: true, Write: true}, nil
	}
	role, err := s.store.GetProjectMember(ctx, dbgen.GetProjectMemberParams{ProjectID: projectID, UserID: userID})
	if database.IsNotFound(err) {
		return Access{}, nil
	}
	if err != nil {
		return Access{}, err
	}
	return Access{Read: true, Write: Role(role) == RoleMember}, nil
}

// ListVisible returns the projects a user may see.
func (s *Service) ListVisible(ctx context.Context, userID int64, isAdmin bool) ([]Project, error) {
	if isAdmin {
		return s.List(ctx)
	}
	rows, err := s.store.ListProjectsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(rows))
	for i, r := range rows {
		out[i] = FromRow(r)
	}
	return out, nil
}

func (s *Service) Members(ctx context.Context, projectID int64) ([]Member, error) {
	rows, err := s.store.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(rows))
	for i, r := range rows {
		out[i] = Member{UserID: r.UserID, Role: Role(r.Role)}
	}
	return out, nil
}

// SetMember adds or changes a member. The caller must be a writer of the
// project (checked by the API). A member cannot demote themselves to viewer
// if that would leave the project without any member able to write.
func (s *Service) SetMember(ctx context.Context, projectID, userID int64, role Role) error {
	if !role.Valid() {
		return apperr.Invalid("role must be member or viewer")
	}
	return s.store.InTx(ctx, func(q dbgen.Querier) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return notFoundErr(err, "project")
		}
		u, err := q.GetUser(ctx, userID)
		if err != nil {
			return notFoundErr(err, "user")
		}
		if !u.IsActive {
			return apperr.Invalid("this user is deactivated")
		}
		if role == RoleViewer {
			if err := ensureAnotherWriter(ctx, q, projectID, userID); err != nil {
				return err
			}
		}
		return q.UpsertProjectMember(ctx, dbgen.UpsertProjectMemberParams{ProjectID: projectID, UserID: userID, Role: string(role)})
	})
}

func (s *Service) RemoveMember(ctx context.Context, projectID, userID int64) error {
	return s.store.InTx(ctx, func(q dbgen.Querier) error {
		if err := ensureAnotherWriter(ctx, q, projectID, userID); err != nil {
			return err
		}
		return q.DeleteProjectMember(ctx, dbgen.DeleteProjectMemberParams{ProjectID: projectID, UserID: userID})
	})
}

// ensureAnotherWriter refuses a change that would leave a members-only
// project with nobody but administrators able to write to it — unless the
// change empties the member list entirely, which reopens the project.
func ensureAnotherWriter(ctx context.Context, q dbgen.Querier, projectID, exceptUser int64) error {
	members, err := q.ListProjectMembers(ctx, projectID)
	if err != nil {
		return err
	}
	if len(members) <= 1 {
		return nil // last member leaving reopens the project
	}
	for _, m := range members {
		if m.UserID != exceptUser && Role(m.Role) == RoleMember {
			return nil
		}
	}
	return apperr.Invalid("this would leave the project without a member who can write")
}

func notFoundErr(err error, what string) error {
	if database.IsNotFound(err) {
		return apperr.NotFound(what)
	}
	return err
}

// --- saved filters ---------------------------------------------------------------

type SavedFilter struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) SavedFilters(ctx context.Context, userID, projectID int64) ([]SavedFilter, error) {
	rows, err := s.store.ListSavedFilters(ctx, dbgen.ListSavedFiltersParams{UserID: userID, ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	out := make([]SavedFilter, len(rows))
	for i, r := range rows {
		out[i] = SavedFilter{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Query: r.Query, CreatedAt: time.Unix(r.CreatedAt, 0).UTC()}
	}
	return out, nil
}

func (s *Service) SaveFilter(ctx context.Context, userID, projectID int64, name, query string) (SavedFilter, error) {
	name = trimSpaces(name)
	query = trimSpaces(query)
	switch {
	case name == "" || len(name) > 50:
		return SavedFilter{}, apperr.Invalid("filter name must be 1 to 50 characters")
	case query == "" || len(query) > 500:
		return SavedFilter{}, apperr.Invalid("filter query must be 1 to 500 characters")
	}
	var row dbgen.SavedFilter
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		existing, err := q.ListSavedFilters(ctx, dbgen.ListSavedFiltersParams{UserID: userID, ProjectID: projectID})
		if err != nil {
			return err
		}
		for _, e := range existing {
			if e.Name == name { // replace: same name overwrites
				if _, err := q.DeleteSavedFilter(ctx, dbgen.DeleteSavedFilterParams{ID: e.ID, UserID: userID}); err != nil {
					return err
				}
			}
		}
		if len(existing) >= 30 {
			return apperr.Invalid("at most 30 saved filters per project")
		}
		row, err = q.CreateSavedFilter(ctx, dbgen.CreateSavedFilterParams{UserID: userID, ProjectID: projectID, Name: name, Query: query, Now: s.now().Unix()})
		return err
	})
	if err != nil {
		return SavedFilter{}, err
	}
	return SavedFilter{ID: row.ID, ProjectID: row.ProjectID, Name: row.Name, Query: row.Query, CreatedAt: time.Unix(row.CreatedAt, 0).UTC()}, nil
}

func (s *Service) DeleteFilter(ctx context.Context, userID, id int64) error {
	n, err := s.store.DeleteSavedFilter(ctx, dbgen.DeleteSavedFilterParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("filter")
	}
	return nil
}
