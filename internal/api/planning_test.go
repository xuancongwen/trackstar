package api

import (
	"net/http"
	"testing"

	"trackstar/internal/project"
	"trackstar/internal/story"
)

func TestEpicsTasksAndBulkMoveOverHTTP(t *testing.T) {
	_, ts := newServer(t, true)
	c := newClient(t, ts)
	c.register("sam@example.com")
	c.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, nil)

	var e story.Epic
	c.must(http.StatusCreated, "POST", "/api/projects/1/epics", map[string]any{"name": "Auth", "description": "sign-in"}, &e)
	c.must(http.StatusConflict, "POST", "/api/projects/1/epics", map[string]any{"name": "auth"}, nil)

	var a, b, d story.Story
	c.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "A", "estimate": 3, "section": "backlog", "labels": []string{"auth"}}, &a)
	c.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "B", "estimate": 5, "section": "backlog"}, &b)
	c.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "D", "estimate": 1, "section": "icebox"}, &d)

	var epics []story.Epic
	c.must(http.StatusOK, "GET", "/api/projects/1/epics", nil, &epics)
	if len(epics) != 1 || epics[0].TotalPoints != 3 || epics[0].StoryCount != 1 {
		t.Fatalf("epics = %+v", epics)
	}
	c.must(http.StatusOK, "PATCH", "/api/epics/1", map[string]any{"description": "all things sign-in"}, &e)
	if e.Description != "all things sign-in" {
		t.Fatalf("epic = %+v", e)
	}

	// tasks
	var task story.Task
	c.must(http.StatusCreated, "POST", "/api/stories/1/tasks", map[string]any{"description": "write it"}, &task)
	c.must(http.StatusOK, "PATCH", "/api/tasks/1", map[string]any{"done": true}, &task)
	var detail story.Detail
	c.must(http.StatusOK, "GET", "/api/stories/1", nil, &detail)
	if len(detail.Tasks) != 1 || !detail.Tasks[0].Done || detail.TasksDone != 1 {
		t.Fatalf("detail = %+v", detail)
	}

	// blockers via PATCH
	c.must(http.StatusOK, "PATCH", "/api/stories/1", map[string]any{"blocked_by": []int64{b.ID}}, &a)
	if !a.Blocked || len(a.BlockedBy) != 1 {
		t.Fatalf("a = %+v", a)
	}
	c.must(http.StatusUnprocessableEntity, "PATCH", "/api/stories/2", map[string]any{"blocked_by": []int64{a.ID}}, nil) // cycle

	// bulk move: D and A (that order) after B in backlog
	var moved struct{ Stories []story.Story }
	c.must(http.StatusOK, "POST", "/api/stories/move", map[string]any{"ids": []int64{d.ID, a.ID}, "section": "backlog", "prev_id": b.ID}, &moved)
	if len(moved.Stories) != 2 || moved.Stories[0].State != story.StateBacklog {
		t.Fatalf("moved = %+v", moved.Stories)
	}
	var list []story.Story
	c.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, &list)
	var titles []string
	for _, s := range list {
		if s.Section == story.SectionBacklog {
			titles = append(titles, s.Title)
		}
	}
	if len(titles) != 3 || titles[0] != "B" || titles[1] != "D" || titles[2] != "A" {
		t.Fatalf("backlog order = %v", titles)
	}
	c.must(http.StatusUnprocessableEntity, "POST", "/api/stories/move", map[string]any{"ids": []int64{}, "section": "backlog"}, nil)

	// saved filters
	var f project.SavedFilter
	c.must(http.StatusCreated, "POST", "/api/projects/1/filters", map[string]any{"name": "Bugs", "query": "type:bug"}, &f)
	var filters []project.SavedFilter
	c.must(http.StatusOK, "GET", "/api/projects/1/filters", nil, &filters)
	if len(filters) != 1 {
		t.Fatalf("filters = %+v", filters)
	}
	c.must(http.StatusNoContent, "DELETE", "/api/filters/1", nil, nil)

	c.must(http.StatusNoContent, "DELETE", "/api/epics/1", nil, nil)
	c.must(http.StatusOK, "GET", "/api/projects/1/epics", nil, &epics)
	if len(epics) != 0 {
		t.Fatalf("epics after demote = %+v", epics)
	}
}

func TestMembershipIsEnforced(t *testing.T) {
	_, ts := newServer(t, true)
	admin := newClient(t, ts)
	admin.register("admin@example.com")
	alice := newClient(t, ts)
	alice.register("alice@example.com") // id 2
	bob := newClient(t, ts)
	bob.register("bob@example.com") // id 3

	alice.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Secret"}, nil)
	alice.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "S", "section": "backlog"}, nil)

	// Open project: everyone can read and write.
	bob.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, nil)

	// Alice adds herself as a member → project closes to non-members.
	var members []project.Member
	alice.must(http.StatusOK, "PUT", "/api/projects/1/members/2", map[string]any{"role": "member"}, &members)
	bob.must(http.StatusNotFound, "GET", "/api/projects/1", nil, nil)
	bob.must(http.StatusNotFound, "GET", "/api/projects/1/stories", nil, nil)
	bob.must(http.StatusNotFound, "GET", "/api/stories/1", nil, nil)
	bob.must(http.StatusNotFound, "PATCH", "/api/stories/1", map[string]any{"title": "x"}, nil)
	bob.must(http.StatusNotFound, "GET", "/api/projects/1/events", nil, nil)
	var visible []project.Project
	bob.must(http.StatusOK, "GET", "/api/projects", nil, &visible)
	if len(visible) != 0 {
		t.Fatalf("bob sees %d projects", len(visible))
	}
	admin.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, nil) // admins always

	// Viewer: read but not write.
	alice.must(http.StatusOK, "PUT", "/api/projects/1/members/3", map[string]any{"role": "viewer"}, &members)
	bob.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, nil)
	bob.must(http.StatusForbidden, "PATCH", "/api/stories/1", map[string]any{"title": "x"}, nil)
	bob.must(http.StatusForbidden, "POST", "/api/projects/1/stories", map[string]any{"title": "x"}, nil)
	bob.must(http.StatusForbidden, "POST", "/api/stories/1/comments", map[string]any{"body": "x"}, nil)
	bob.must(http.StatusForbidden, "POST", "/api/stories/1/move", map[string]any{"section": "icebox"}, nil)
	bob.must(http.StatusForbidden, "PUT", "/api/projects/1/members/3", map[string]any{"role": "member"}, nil)
	bob.must(http.StatusCreated, "POST", "/api/projects/1/filters", map[string]any{"name": "mine", "query": "owner:me"}, nil) // own filters are fine

	// Last writer cannot leave; removing everyone reopens.
	alice.must(http.StatusUnprocessableEntity, "DELETE", "/api/projects/1/members/2", nil, nil)
	alice.must(http.StatusNoContent, "DELETE", "/api/projects/1/members/3", nil, nil)
	alice.must(http.StatusNoContent, "DELETE", "/api/projects/1/members/2", nil, nil)
	bob.must(http.StatusOK, "PATCH", "/api/stories/1", map[string]any{"title": "open again"}, nil)
}
