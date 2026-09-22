package api

import (
	"context"
	"net/http"

	"trackstar/internal/apperr"
	"trackstar/internal/project"
)

// authorize checks the caller's access to a project. A project the caller may
// not even read is reported as not found, so membership does not leak which
// projects exist.
func (s *Server) authorize(ctx context.Context, projectID int64, write bool) error {
	u := currentUser(ctx)
	access, err := s.Projects.AccessFor(ctx, projectID, u.ID, u.IsAdmin)
	if err != nil {
		return err
	}
	if !access.Read {
		return apperr.NotFound("project")
	}
	if write && !access.Write {
		return apperr.Forbidden("you have read-only access to this project")
	}
	return nil
}

// projectFromPath resolves {project} (numeric id or slug) and checks access.
func (s *Server) projectFromPath(r *http.Request, write bool) (project.Project, error) {
	p, err := s.Projects.Resolve(r.Context(), r.PathValue("project"))
	if err != nil {
		return project.Project{}, err
	}
	return p, s.authorize(r.Context(), p.ID, write)
}

// idFromPath reads {name} and checks access to the project that owns it,
// using lookup to map the id to its project.
func (s *Server) idFromPath(r *http.Request, name string, write bool, lookup func(context.Context, int64) (int64, error)) (int64, error) {
	id, err := pathID(r, name)
	if err != nil {
		return 0, err
	}
	projectID, err := lookup(r.Context(), id)
	if err != nil {
		return 0, err
	}
	return id, s.authorize(r.Context(), projectID, write)
}
