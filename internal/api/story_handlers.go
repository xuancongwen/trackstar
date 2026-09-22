package api

import (
	"net/http"

	"trackstar/internal/apperr"
	"trackstar/internal/story"
)

func (s *Server) handleListStories(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, false)
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
	p, err := s.projectFromPath(r, true)
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
	p, err := s.projectFromPath(r, false)
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
	id, err := s.idFromPath(r, "id", false, s.Stories.ProjectOfStory)
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
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfStory)
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
	// A blocker change also affects how the blockers' dependants render.
	if in.BlockedBy != nil {
		for _, b := range *in.BlockedBy {
			s.publish(r, "stories", st.ProjectID, b)
		}
	}
}

func (s *Server) handleDeleteStory(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfStory)
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
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfStory)
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
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfStory)
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

// handleMoveStories moves several stories of one project at once.
func (s *Server) handleMoveStories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
		story.MoveInput
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(in.IDs) == 0 {
		s.fail(w, r, apperr.Invalid("ids is required"))
		return
	}
	projectID, err := s.Stories.ProjectOfStory(r.Context(), in.IDs[0])
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.authorize(r.Context(), projectID, true); err != nil {
		s.fail(w, r, err)
		return
	}
	moved, err := s.Stories.MoveMany(r.Context(), in.IDs, currentUser(r.Context()).ID, in.MoveInput)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stories": moved})
	for _, st := range moved {
		s.publish(r, "stories", st.ProjectID, st.ID)
	}
}

// --- comments --------------------------------------------------------------------------

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfStory)
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
	if projectID, err := s.Stories.ProjectOfStory(r.Context(), id); err == nil {
		s.publish(r, "stories", projectID, id)
	}
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfComment)
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
	if projectID, err := s.Stories.ProjectOfStory(r.Context(), storyID); err == nil {
		s.publish(r, "stories", projectID, storyID)
	}
}

// --- tasks -----------------------------------------------------------------------------

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfStory)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Description string `json:"description"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.Stories.AddTask(r.Context(), id, in.Description)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
	if projectID, err := s.Stories.ProjectOfStory(r.Context(), id); err == nil {
		s.publish(r, "stories", projectID, id)
	}
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfTask)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in story.TaskInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.Stories.UpdateTask(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
	if projectID, err := s.Stories.ProjectOfStory(r.Context(), t.StoryID); err == nil {
		s.publish(r, "stories", projectID, t.StoryID)
	}
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfTask)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	storyID, err := s.Stories.DeleteTask(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if projectID, err := s.Stories.ProjectOfStory(r.Context(), storyID); err == nil {
		s.publish(r, "stories", projectID, storyID)
	}
}

// --- epics -----------------------------------------------------------------------------

func (s *Server) handleListEpics(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	epics, err := s.Stories.Epics(r.Context(), p.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, epics)
}

func (s *Server) handleCreateEpic(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in story.EpicInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.Stories.CreateEpic(r.Context(), p.ID, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
	s.publish(r, "project", p.ID, 0)
}

func (s *Server) handleUpdateEpic(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfEpic)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in story.EpicInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.Stories.UpdateEpic(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
	s.publish(r, "project", e.ProjectID, 0)
}

func (s *Server) handleDeleteEpic(w http.ResponseWriter, r *http.Request) {
	id, err := s.idFromPath(r, "id", true, s.Stories.ProjectOfEpic)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	projectID, _ := s.Stories.ProjectOfEpic(r.Context(), id)
	if err := s.Stories.DemoteEpic(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	s.publish(r, "project", projectID, 0)
}
