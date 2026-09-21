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
		Query: r.URL.Query().Get("q"),
		Done:  r.URL.Query().Get("section") == string(story.SectionDone),
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
	st, err := s.Stories.Update(r.Context(), id, currentUser(r.Context()).ID, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDeleteStory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Stories.Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	res, err := s.Stories.Move(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Stories.DeleteComment(r.Context(), id, currentUser(r.Context()).ID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
