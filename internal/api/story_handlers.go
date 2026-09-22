package api

import (
	"net/http"

	"tracker/internal/story"
)

func (s *Server) handleListStories(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	stories, err := s.Stories.List(r.Context(), p.ID, story.ListOptions{
		Query:   r.URL.Query().Get("q"),
		Done:    r.URL.Query().Get("section") == string(story.SectionDone),
		Deleted: r.URL.Query().Get("section") == "deleted",
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stories)
}

func (s *Server) handleCreateStory(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in story.CreateInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.Stories.Create(r.Context(), p.ID, currentUser(r.Context()).ID, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, st)
	s.publish(r, "stories", st.ProjectID, st.ID)
}

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	labels, err := s.Stories.Labels(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) handleGetStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.Stories.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in story.UpdateInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	u := currentUser(r.Context())
	st, err := s.Stories.Update(r.Context(), id, story.Actor{ID: u.ID, IsAdmin: u.IsAdmin}, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
	s.publish(r, "stories", st.ProjectID, st.ID)
}

func (s *Server) handleDeleteStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.Stories.Delete(r.Context(), id, currentUser(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st) // the trashed story, so the client can offer undo
	s.publish(r, "stories", st.ProjectID, id)
}

func (s *Server) handleRestoreStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.Stories.Restore(r.Context(), id, currentUser(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
	s.publish(r, "stories", st.ProjectID, id)
}

func (s *Server) handleMoveStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in story.MoveInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Stories.Move(r.Context(), id, currentUser(r.Context()).ID, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
	s.publish(r, "stories", res.Story.ProjectID, res.Story.ID)
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Stories.AddComment(r.Context(), id, currentUser(r.Context()).ID, in.Body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
	if d, err := s.Stories.Get(r.Context(), id); err == nil {
		s.publish(r, "stories", d.ProjectID, id)
	}
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	storyID, err := s.Stories.DeleteComment(r.Context(), id, currentUser(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if d, err := s.Stories.Get(r.Context(), storyID); err == nil {
		s.publish(r, "stories", d.ProjectID, storyID)
	}
}
