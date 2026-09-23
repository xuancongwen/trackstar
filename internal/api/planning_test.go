package api

import (
	"context"
	"net/http"
	"strings"
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
	srv, ts := newServer(t, true)
	admin := newClient(t, ts)
	admin.register("admin@example.com")
	alice := newClient(t, ts)
	alice.register("alice@example.com") // id 2
	bob := newClient(t, ts)
	bob.register("bob@example.com") // id 3

	// The creator owns the project: it is hidden from everyone else.
	alice.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Secret"}, nil)
	alice.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "S", "section": "backlog"}, nil)
	var members []project.Member
	alice.must(http.StatusOK, "GET", "/api/projects/1/members", nil, &members)
	if len(members) != 1 || members[0].UserID != 2 || members[0].Role != project.RoleOwner {
		t.Fatalf("members after create = %+v", members)
	}
	var p struct {
		CanWrite  bool `json:"can_write"`
		CanManage bool `json:"can_manage"`
	}
	alice.must(http.StatusOK, "GET", "/api/projects/1", nil, &p)
	if !p.CanWrite || !p.CanManage {
		t.Fatalf("owner project = %+v", p)
	}
	bob.must(http.StatusNotFound, "GET", "/api/projects/1", nil, nil)
	bob.must(http.StatusNotFound, "GET", "/api/projects/1/stories", nil, nil)
	bob.must(http.StatusNotFound, "GET", "/api/stories/1", nil, nil)
	bob.must(http.StatusNotFound, "PATCH", "/api/stories/1", map[string]any{"title": "x"}, nil)
	bob.must(http.StatusNotFound, "GET", "/api/projects/1/events", nil, nil)
	bob.must(http.StatusNotFound, "PUT", "/api/projects/1/members/3", map[string]any{"role": "member"}, nil)
	var visible []project.Project
	bob.must(http.StatusOK, "GET", "/api/projects", nil, &visible)
	if len(visible) != 0 {
		t.Fatalf("bob sees %d projects", len(visible))
	}
	admin.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, nil) // admins always

	// Member: read and write, but not manage.
	alice.must(http.StatusOK, "PUT", "/api/projects/1/members/3", map[string]any{"role": "member"}, &members)
	bob.must(http.StatusOK, "GET", "/api/projects/1", nil, &p)
	if !p.CanWrite || p.CanManage {
		t.Fatalf("member project = %+v", p)
	}
	bob.must(http.StatusOK, "PATCH", "/api/stories/1", map[string]any{"title": "x"}, nil)
	bob.must(http.StatusCreated, "POST", "/api/projects/1/filters", map[string]any{"name": "mine", "query": "owner:me"}, nil)
	bob.must(http.StatusForbidden, "PATCH", "/api/projects/1", map[string]any{"name": "Renamed"}, nil)
	bob.must(http.StatusForbidden, "PUT", "/api/projects/1/members/3", map[string]any{"role": "owner"}, nil)
	bob.must(http.StatusForbidden, "DELETE", "/api/projects/1/members/2", nil, nil)
	bob.must(http.StatusForbidden, "DELETE", "/api/projects/1", nil, nil)
	alice.must(http.StatusUnprocessableEntity, "PUT", "/api/projects/1/members/3", map[string]any{"role": "viewer"}, nil)

	// The only owner can neither step down nor leave until there is another.
	alice.must(http.StatusUnprocessableEntity, "PUT", "/api/projects/1/members/2", map[string]any{"role": "member"}, nil)
	alice.must(http.StatusUnprocessableEntity, "DELETE", "/api/projects/1/members/2", nil, nil)
	alice.must(http.StatusOK, "PUT", "/api/projects/1/members/3", map[string]any{"role": "owner"}, &members)
	alice.must(http.StatusNoContent, "DELETE", "/api/projects/1/members/2", nil, nil)
	alice.must(http.StatusNotFound, "GET", "/api/projects/1", nil, nil)
	bob.must(http.StatusOK, "PATCH", "/api/projects/1", map[string]any{"name": "Bob's now"}, nil)

	// An owner may delete the project. A project without members (from before
	// ownership existed) is open to everyone but managed by administrators only.
	bob.must(http.StatusNoContent, "DELETE", "/api/projects/1", nil, nil)
	legacy := "Legacy"
	open, err := srv.Projects.Create(context.Background(), 0, project.Input{Name: &legacy})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/projects/" + open.Slug
	alice.must(http.StatusOK, "GET", path, nil, &p)
	if !p.CanWrite || p.CanManage {
		t.Fatalf("open project = %+v", p)
	}
	alice.must(http.StatusForbidden, "PUT", path+"/members/2", map[string]any{"role": "owner"}, nil)
	admin.must(http.StatusOK, "PUT", path+"/members/2", map[string]any{"role": "owner"}, &members)
	bob.must(http.StatusNotFound, "GET", path, nil, nil)
}

func TestArchiveAndDeleteProject(t *testing.T) {
	_, ts := newServer(t, true)
	admin := newClient(t, ts)
	admin.register("admin@example.com")
	alice := newClient(t, ts)
	alice.register("alice@example.com") // id 2
	bob := newClient(t, ts)
	bob.register("bob@example.com") // id 3

	var p project.Project
	alice.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, &p)
	alice.must(http.StatusOK, "PUT", "/api/projects/1/members/3", map[string]any{"role": "member"}, nil)
	var st story.Story
	alice.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "S", "section": "backlog"}, &st)
	alice.must(http.StatusCreated, "POST", "/api/stories/1/comments", map[string]any{"body": "hi"}, nil)
	alice.must(http.StatusCreated, "POST", "/api/projects/1/epics", map[string]any{"name": "E"}, nil)
	if p.ArchivedAt != nil {
		t.Fatalf("new project archived: %+v", p)
	}

	// Members cannot archive; owners can. Archiving is idempotent.
	bob.must(http.StatusForbidden, "POST", "/api/projects/1/archive", nil, nil)
	var view struct {
		project.Project
		CanWrite  bool `json:"can_write"`
		CanManage bool `json:"can_manage"`
	}
	alice.must(http.StatusOK, "POST", "/api/projects/1/archive", nil, &view)
	if view.ArchivedAt == nil || view.CanWrite || !view.CanManage {
		t.Fatalf("archived view = %+v", view)
	}
	alice.must(http.StatusOK, "POST", "/api/projects/1/archive", nil, &view)

	// Read-only for everyone, admins included; reads keep working.
	for _, c := range []*client{alice, bob, admin} {
		c.must(http.StatusOK, "GET", "/api/projects/apollo/stories", nil, nil)
		c.must(http.StatusOK, "GET", "/api/stories/1", nil, nil)
		c.must(http.StatusForbidden, "POST", "/api/projects/1/stories", map[string]any{"title": "no"}, nil)
		c.must(http.StatusForbidden, "PATCH", "/api/stories/1", map[string]any{"title": "no"}, nil)
		c.must(http.StatusForbidden, "DELETE", "/api/stories/1", nil, nil)
		c.must(http.StatusForbidden, "POST", "/api/stories/1/comments", map[string]any{"body": "no"}, nil)
		c.must(http.StatusForbidden, "POST", "/api/projects/1/epics", map[string]any{"name": "no"}, nil)
		c.must(http.StatusForbidden, "POST", "/api/stories/move", map[string]any{"ids": []int64{1}, "section": "current"}, nil)
	}
	var msg map[string]string
	bob.do("PATCH", "/api/stories/1", map[string]any{"title": "no"}, &msg)
	if !strings.Contains(msg["error"], "archived") {
		t.Fatalf("error = %q, want to mention archiving", msg["error"])
	}
	bob.must(http.StatusOK, "GET", "/api/projects/1", nil, &view)
	if view.CanWrite || view.CanManage || view.ArchivedAt == nil {
		t.Fatalf("member's archived view = %+v", view)
	}
	// Owners still manage: members, settings, and the archive itself.
	alice.must(http.StatusOK, "PATCH", "/api/projects/1", map[string]any{"description": "shelved"}, nil)
	alice.must(http.StatusOK, "PUT", "/api/projects/1/members/3", map[string]any{"role": "owner"}, nil)
	var list []project.Project
	bob.must(http.StatusOK, "GET", "/api/projects", nil, &list)
	if len(list) != 1 || list[0].ArchivedAt == nil {
		t.Fatalf("list = %+v", list)
	}

	alice.must(http.StatusOK, "DELETE", "/api/projects/1/archive", nil, &view)
	if view.ArchivedAt != nil || !view.CanWrite {
		t.Fatalf("unarchived view = %+v", view)
	}
	bob.must(http.StatusOK, "PATCH", "/api/stories/1", map[string]any{"title": "yes"}, nil)

	// Deleting removes the project and everything in it.
	alice.must(http.StatusNoContent, "DELETE", "/api/projects/1", nil, nil)
	alice.must(http.StatusNotFound, "GET", "/api/projects/1", nil, nil)
	alice.must(http.StatusNotFound, "GET", "/api/projects/apollo", nil, nil)
	admin.must(http.StatusNotFound, "GET", "/api/stories/1", nil, nil)
	admin.must(http.StatusNotFound, "GET", "/api/projects/1/members", nil, nil)
	admin.must(http.StatusOK, "GET", "/api/projects", nil, &list)
	if len(list) != 0 {
		t.Fatalf("projects after delete = %+v", list)
	}
	alice.must(http.StatusNotFound, "DELETE", "/api/projects/1", nil, nil)
}
