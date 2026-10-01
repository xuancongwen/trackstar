package api

import (
	"net/http"
	"testing"

	"trackstar/internal/story"
)

type overview struct {
	Projects []story.ProjectStats `json:"projects"`
	Activity []story.FeedEntry    `json:"activity"`
}

// TestOverviewShowsOnlyVisibleProjects: the projects page's counts and feed
// follow the same visibility as the project list.
func TestOverviewShowsOnlyVisibleProjects(t *testing.T) {
	_, ts := newServer(t, true)
	newClient(t, ts).must(http.StatusUnauthorized, "GET", "/api/overview", nil, nil)

	admin := newClient(t, ts)
	admin.register("admin@example.com")
	alice := newClient(t, ts)
	alice.register("alice@example.com")
	bob := newClient(t, ts)
	bob.register("bob@example.com")

	// Before anything exists the lists are empty arrays, not null.
	var raw map[string]any
	alice.must(http.StatusOK, "GET", "/api/overview", nil, &raw)
	if p, ok := raw["projects"].([]any); !ok || len(p) != 0 {
		t.Fatalf("empty overview projects = %#v", raw["projects"])
	}
	if a, ok := raw["activity"].([]any); !ok || len(a) != 0 {
		t.Fatalf("empty overview activity = %#v", raw["activity"])
	}

	// Alice's project is members-only; Bob has one of his own.
	alice.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, nil)
	bob.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Borealis"}, nil)
	var st story.Story
	alice.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "Secret plan", "section": "current", "estimate": 1}, &st)
	alice.must(http.StatusOK, "PATCH", "/api/stories/1", map[string]any{"state": "started"}, nil)
	alice.must(http.StatusCreated, "POST", "/api/stories/1/comments", map[string]any{"body": "On it"}, nil)
	bob.must(http.StatusCreated, "POST", "/api/projects/2/stories", map[string]any{"title": "Bob's story"}, nil)

	var mine overview
	alice.must(http.StatusOK, "GET", "/api/overview", nil, &mine)
	if len(mine.Projects) != 1 || mine.Projects[0].ProjectID != 1 || mine.Projects[0].InProgress != 1 || mine.Projects[0].ToAccept != 0 || mine.Projects[0].LastActivityAt == nil {
		t.Fatalf("alice's stats = %+v", mine.Projects)
	}
	// created, owner (starting a story assigns it), state, and the comment.
	// All four happened within the same second, so their order is not fixed.
	kinds := map[string]bool{}
	for _, e := range mine.Activity {
		kinds[e.Kind] = true
		if e.ProjectID != 1 || e.StoryTitle != "Secret plan" {
			t.Fatalf("alice sees another project's activity: %+v", e)
		}
		if e.Kind == story.FeedKindComment && e.Body != "On it" {
			t.Fatalf("comment entry = %+v", e)
		}
	}
	if len(mine.Activity) != 4 || !kinds["created"] || !kinds["state"] || !kinds[story.FeedKindComment] {
		t.Fatalf("alice's feed = %+v", mine.Activity)
	}

	var bobs overview
	bob.must(http.StatusOK, "GET", "/api/overview", nil, &bobs)
	if len(bobs.Projects) != 1 || bobs.Projects[0].ProjectID != 2 {
		t.Fatalf("bob's stats = %+v", bobs.Projects)
	}
	for _, e := range bobs.Activity {
		if e.ProjectID != 2 || e.StoryTitle == "Secret plan" {
			t.Fatalf("bob sees alice's activity: %+v", e)
		}
	}

	// Administrators see everything.
	var all overview
	admin.must(http.StatusOK, "GET", "/api/overview", nil, &all)
	if len(all.Projects) != 2 {
		t.Fatalf("admin's stats = %+v", all.Projects)
	}

	// An archived project keeps its counts but leaves the feed.
	alice.must(http.StatusOK, "POST", "/api/projects/1/archive", nil, nil)
	alice.must(http.StatusOK, "GET", "/api/overview", nil, &mine)
	if len(mine.Projects) != 1 || mine.Projects[0].InProgress != 1 || len(mine.Activity) != 0 {
		t.Fatalf("after archiving: %+v", mine)
	}
}
