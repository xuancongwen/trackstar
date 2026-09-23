package api

import (
	"context"
	"net/http"

	"trackstar/internal/apperr"
	"trackstar/internal/project"
)

// level is how much of a project a handler needs.
type level int

const (
	readAccess   level = iota // members (and everyone, on an open project)
	writeAccess               // same, plus changing stories
	manageAccess              // owners and administrators: members, settings, deletion
)

// authorize checks the caller's access to a project. A project the caller may
// not even read is reported as not found, so membership does not leak which
// projects exist.
func (s *Server) authorize(ctx context.Context, projectID int64, need level) error {
	u := currentUser(ctx)
	access, err := s.Projects.AccessFor(ctx, projectID, u.ID, u.IsAdmin)
	if err != nil {
		return err
	}
	switch {
	case !access.Read:
		return apperr.NotFound("project")
	case need >= writeAccess && !access.Write:
		return apperr.Forbidden("you have read-only access to this project")
	case need >= manageAccess && !access.Manage:
		return apperr.Forbidden("only a project owner or an administrator can do this")
	}
	return nil
}

// projectFromPath resolves {project} (numeric id or slug) and checks access.
func (s *Server) projectFromPath(r *http.Request, need level) (project.Project, error) {
	p, err := s.Projects.Resolve(r.Context(), r.PathValue("project"))
	if err != nil {
		return project.Project{}, err
	}
	return p, s.authorize(r.Context(), p.ID, need)
}

// idFromPath reads {name} and checks access to the project that owns it,
// using lookup to map the id to its project.
func (s *Server) idFromPath(r *http.Request, name string, need level, lookup func(context.Context, int64) (int64, error)) (int64, error) {
	id, err := pathID(r, name)
	if err != nil {
		return 0, err
	}
	projectID, err := lookup(r.Context(), id)
	if err != nil {
		return 0, err
	}
	return id, s.authorize(r.Context(), projectID, need)
}
