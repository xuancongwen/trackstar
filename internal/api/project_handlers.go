package api

import (
	"net/http"

	"tracker/internal/apperr"
	"tracker/internal/project"
)

// projectFromPath resolves {project}, which may be a numeric id or a slug.
func (s *Server) projectFromPath(r *http.Request) (project.Project, error) {
	return s.Projects.Resolve(r.Context(), r.PathValue("project"))
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Projects.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in project.Input
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.Projects.Create(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in project.Input
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err = s.Projects.Update(r.Context(), p.ID, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if !currentUser(r.Context()).IsAdmin {
		s.fail(w, r, apperr.Forbidden("only an administrator can delete a project"))
		return
	}
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Projects.Delete(r.Context(), p.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListIterations(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	its, err := s.Velocity.Iterations(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, its)
}

func (s *Server) handleVelocity(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v, err := s.Velocity.Velocity(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
