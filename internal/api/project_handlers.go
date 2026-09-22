package api

import (
	"net/http"

	"trackstar/internal/apperr"
	"trackstar/internal/project"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	projects, err := s.Projects.ListVisible(r.Context(), u.ID, u.IsAdmin)
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
	p, err := s.projectFromPath(r, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u := currentUser(r.Context())
	access, err := s.Projects.AccessFor(r.Context(), p.ID, u.ID, u.IsAdmin)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		project.Project
		CanWrite bool `json:"can_write"`
	}{p, access.Write})
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, true)
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
	s.publish(r, "project", p.ID, 0)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if !currentUser(r.Context()).IsAdmin {
		s.fail(w, r, apperr.Forbidden("only an administrator can delete a project"))
		return
	}
	p, err := s.projectFromPath(r, true)
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
	p, err := s.projectFromPath(r, false)
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
	p, err := s.projectFromPath(r, false)
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

// --- members -------------------------------------------------------------------------

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	members, err := s.Projects.Members(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleSetMember(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	userID, err := pathID(r, "user")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Role project.Role `json:"role"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Projects.SetMember(r.Context(), p.ID, userID, in.Role); err != nil {
		s.fail(w, r, err)
		return
	}
	members, err := s.Projects.Members(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
	s.publish(r, "project", p.ID, 0)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	userID, err := pathID(r, "user")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Projects.RemoveMember(r.Context(), p.ID, userID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.publish(r, "project", p.ID, 0)
}

// --- saved filters (per user, per project) ------------------------------------------------

func (s *Server) handleListFilters(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filters, err := s.Projects.SavedFilters(r.Context(), currentUser(r.Context()).ID, p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, filters)
}

func (s *Server) handleSaveFilter(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, false) // viewers may save their own filters
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Name  string `json:"name"`
		Query string `json:"query"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	f, err := s.Projects.SaveFilter(r.Context(), currentUser(r.Context()).ID, p.ID, in.Name, in.Query)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Projects.DeleteFilter(r.Context(), currentUser(r.Context()).ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
