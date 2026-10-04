package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/bcrypt"

	"trackstar/internal/auth"
	"trackstar/internal/database"
	"trackstar/internal/events"
	"trackstar/internal/project"
	"trackstar/internal/story"
	"trackstar/internal/user"
	"trackstar/internal/velocity"
)

type fixture struct {
	deps  Deps
	auth  *auth.Service
	admin user.User
	kim   user.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := database.NewTestDB(t)
	ctx := context.Background()
	a := auth.NewService(db, auth.Options{Secret: []byte("0123456789abcdef0123456789abcdef"), AllowRegistration: true, BcryptCost: bcrypt.MinCost})
	admin, err := a.Register(ctx, auth.RegisterInput{Email: "sam@example.com", Password: "correct horse", DisplayName: "Sam"})
	if err != nil {
		t.Fatal(err)
	}
	kim, err := a.Register(ctx, auth.RegisterInput{Email: "kim@example.com", Password: "another pass", DisplayName: "Kim"})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		deps: Deps{
			Projects: project.NewService(db, nil),
			Stories:  story.NewService(db, time.UTC, nil),
			Velocity: velocity.NewService(db, time.UTC, nil),
			Users:    user.NewService(db, nil),
			Events:   events.NewHub(),
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			Version:  "test",
		},
		auth: a, admin: admin, kim: kim,
	}
}

// connect returns a client session talking to a server acting as u.
func (f *fixture) connect(t *testing.T, u user.User) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	// The in-memory transport, like the HTTP one, carries the connect
	// context's values into every handler.
	if _, err := New(f.deps).Connect(WithUser(ctx, u), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured result into out. It fails
// the test on a protocol error; a tool error is returned as a string.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		var msgs []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msgs = append(msgs, tc.Text)
			}
		}
		return strings.Join(msgs, "; ")
	}
	if out != nil {
		data, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, data, err)
		}
	}
	return ""
}

func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	if msg := call(t, cs, name, args, out); msg != "" {
		t.Fatalf("%s(%v): tool error: %s", name, args, msg)
	}
}

func TestToolsAreListedWithSchemas(t *testing.T) {
	f := newFixture(t)
	cs := f.connect(t, f.admin)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
	}
	want := []string{"add_comment", "create_project", "create_stories", "create_story", "get_story", "list_epics", "list_projects", "list_stories", "list_users", "move_stories", "move_story", "update_stories", "update_story", "velocity"}
	got := strings.Join(sorted(names), ",")
	if got != strings.Join(want, ",") {
		t.Fatalf("tools = %s", got)
	}
	// No delete, restore or account management.
	for _, n := range names {
		if strings.Contains(n, "delete") || strings.Contains(n, "token") || strings.Contains(n, "password") {
			t.Errorf("unexpected tool %s", n)
		}
	}
	if cs.InitializeResult().Instructions == "" {
		t.Error("no instructions")
	}
}

func TestWorkflowThroughTools(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.deps.Projects.Create(ctx, 0, project.Input{Name: ptr("Apollo")})
	if err != nil {
		t.Fatal(err)
	}
	evs, unsubscribe := f.deps.Events.Subscribe(p.ID)
	defer unsubscribe()
	cs := f.connect(t, f.admin)

	var projects projectsOut
	mustCall(t, cs, "list_projects", nil, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].Slug != "apollo" {
		t.Fatalf("projects = %+v", projects)
	}
	var users usersOut
	mustCall(t, cs, "list_users", nil, &users)
	if len(users.Users) != 2 || users.Me != f.admin.ID {
		t.Fatalf("users = %+v", users)
	}

	// Create by slug, in the backlog, with an estimate; it is attributed to
	// the token's owner and every board sees a change event.
	var a, b story.Story
	mustCall(t, cs, "create_story", map[string]any{"project": "apollo", "title": "Login page", "estimate": 3, "section": "backlog", "labels": []string{"auth"}}, &a)
	if a.RequesterID != f.admin.ID || a.Section != story.SectionBacklog || *a.Estimate != 3 || a.Labels[0] != "auth" {
		t.Fatalf("a = %+v", a)
	}
	select {
	case ev := <-evs:
		if ev.Client != clientID || ev.StoryID != a.ID {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no change event published")
	}
	mustCall(t, cs, "create_story", map[string]any{"project": p.ID, "title": "Fix crash", "type": "bug"}, &b) // numeric id works too
	if b.Section != story.SectionIcebox || b.Type != story.TypeBug {
		t.Fatalf("b = %+v", b)
	}

	// Validation errors are tool errors with the service's message, not
	// protocol errors.
	if msg := call(t, cs, "create_story", map[string]any{"project": "apollo", "title": ""}, nil); !strings.Contains(msg, "title") {
		t.Fatalf("empty title: %q", msg)
	}
	if msg := call(t, cs, "create_story", map[string]any{"project": "nope", "title": "x"}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("unknown project: %q", msg)
	}
	if msg := call(t, cs, "list_stories", map[string]any{"project": "apollo", "section": "trash"}, nil); !strings.Contains(msg, "section") {
		t.Fatalf("bad section: %q", msg)
	}

	// Move b to the top of the backlog, then a to the current iteration and
	// through the workflow.
	var mv story.MoveResult
	mustCall(t, cs, "move_story", map[string]any{"id": b.ID, "section": "backlog"}, &mv)
	var list storiesOut
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": "backlog"}, &list)
	if len(list.Stories) != 2 || list.Stories[0].ID != b.ID || list.Stories[1].ID != a.ID {
		t.Fatalf("backlog = %+v", ids(list.Stories))
	}
	mustCall(t, cs, "move_story", map[string]any{"id": a.ID, "section": "current"}, &mv)
	if mv.Story.State != story.StateUnstarted {
		t.Fatalf("after move: %+v", mv.Story)
	}
	var upd story.Story
	mustCall(t, cs, "update_story", map[string]any{"id": a.ID, "state": "started", "owner_id": f.kim.ID}, &upd)
	if upd.State != story.StateStarted || *upd.OwnerID != f.kim.ID {
		t.Fatalf("started = %+v", upd)
	}
	if msg := call(t, cs, "move_story", map[string]any{"id": a.ID, "section": "icebox"}, nil); !strings.Contains(msg, "current iteration") {
		t.Fatalf("move started story out: %q", msg)
	}
	mustCall(t, cs, "update_story", map[string]any{"id": a.ID, "clear_owner": true, "clear_estimate": true, "type": "chore"}, &upd)
	if upd.OwnerID != nil || upd.Estimate != nil || upd.Type != story.TypeChore {
		t.Fatalf("cleared = %+v", upd)
	}
	mustCall(t, cs, "update_story", map[string]any{"id": b.ID, "blocked_by": []int64{a.ID}}, &upd)
	if !upd.Blocked {
		t.Fatalf("blocked = %+v", upd)
	}

	var c story.Comment
	mustCall(t, cs, "add_comment", map[string]any{"story_id": a.ID, "body": "On it."}, &c)
	var d story.Detail
	mustCall(t, cs, "get_story", map[string]any{"id": a.ID}, &d)
	if len(d.Comments) != 1 || d.Comments[0].UserID != f.admin.ID || len(d.Activity) < 3 {
		t.Fatalf("detail = %+v", d)
	}
	mustCall(t, cs, "update_story", map[string]any{"id": a.ID, "labels": []string{"seen"}}, &upd)
	if upd.CommentCount != 1 {
		t.Fatalf("comment_count after update = %d", upd.CommentCount)
	}
	if msg := call(t, cs, "get_story", map[string]any{"id": 999}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("missing story: %q", msg)
	}

	// Search spans sections; the default list is the three live sections.
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "query": "crash"}, &list)
	if len(list.Stories) != 1 || list.Stories[0].ID != b.ID {
		t.Fatalf("search = %+v", ids(list.Stories))
	}
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo"}, &list)
	if len(list.Stories) != 2 {
		t.Fatalf("all = %+v", ids(list.Stories))
	}

	var epics epicsOut
	mustCall(t, cs, "list_epics", map[string]any{"project": "apollo"}, &epics)
	if len(epics.Epics) != 0 {
		t.Fatalf("epics = %+v", epics)
	}
	var vel velocityOut
	mustCall(t, cs, "velocity", map[string]any{"project": "apollo"}, &vel)
	if !vel.Estimated || len(vel.History) == 0 || !vel.History[len(vel.History)-1].Current {
		t.Fatalf("velocity = %+v", vel)
	}

	// Resources.
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "trackstar://projects/apollo/current"})
	if err != nil {
		t.Fatal(err)
	}
	var section struct {
		Section string        `json:"section"`
		Stories []story.Story `json:"stories"`
	}
	if err := json.Unmarshal([]byte(rr.Contents[0].Text), &section); err != nil {
		t.Fatal(err)
	}
	if section.Section != "current" || len(section.Stories) != 1 || section.Stories[0].ID != a.ID {
		t.Fatalf("current resource = %+v", section)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "trackstar://projects/nope/backlog"}); err == nil {
		t.Fatal("unknown project resource should fail")
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "trackstar://projects/apollo/trash"}); err == nil {
		t.Fatal("unknown section resource should fail")
	}
	templates, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) != 2 {
		t.Fatalf("templates = %+v, %v", templates, err)
	}
}

func TestMoveStories(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.deps.Projects.Create(ctx, f.admin.ID, project.Input{Name: ptr("Apollo")}) // kim is not a member
	other, _ := f.deps.Projects.Create(ctx, 0, project.Input{Name: ptr("Other")})
	mk := func(projectID int64, title string, sec story.Section) story.Story {
		t.Helper()
		st, err := f.deps.Stories.Create(ctx, projectID, f.admin.ID, story.CreateInput{Title: title, Section: sec, Type: story.TypeChore})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	anchor := mk(p.ID, "Anchor", story.SectionBacklog)
	tail := mk(p.ID, "Tail", story.SectionBacklog)
	a := mk(p.ID, "A", story.SectionIcebox)
	b := mk(p.ID, "B", story.SectionIcebox)
	c := mk(p.ID, "C", story.SectionIcebox)
	started := mk(p.ID, "Started", story.SectionCurrent)
	if _, err := f.deps.Stories.Update(ctx, started.ID, story.Actor{ID: f.admin.ID}, story.UpdateInput{State: ptr(story.StateStarted)}); err != nil {
		t.Fatal(err)
	}
	trashed := mk(p.ID, "Trashed", story.SectionIcebox)
	if _, err := f.deps.Stories.Delete(ctx, trashed.ID, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	foreign := mk(other.ID, "Foreign", story.SectionIcebox)

	evs, unsubscribe := f.deps.Events.Subscribe(p.ID)
	defer unsubscribe()
	cs := f.connect(t, f.admin)

	// The movable stories land after the anchor in the order given, each
	// after the previous one moved; the refused ones are reported and skip
	// nothing else.
	var out bulkOut
	mustCall(t, cs, "move_stories", map[string]any{
		"ids":     []int64{c.ID, started.ID, a.ID, trashed.ID, 999, foreign.ID, b.ID, a.ID},
		"section": "backlog", "prev_id": anchor.ID,
	}, &out)
	if out.Succeeded != 3 || out.Failed != 5 || len(out.Results) != 8 {
		t.Fatalf("out = %+v", out)
	}
	for i, wantErr := range []string{"", "current iteration", "", "trash", "not found", "another project", "", "listed twice"} {
		r := out.Results[i]
		if wantErr == "" {
			if !r.OK || r.Section != story.SectionBacklog || r.State != story.StateBacklog || r.Title == "" {
				t.Errorf("result %d = %+v", i, r)
			}
		} else if r.OK || !strings.Contains(r.Error, wantErr) {
			t.Errorf("result %d = %+v, want error containing %q", i, r, wantErr)
		}
	}
	var list storiesOut
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": "backlog"}, &list)
	if got, want := ids(list.Stories), []int64{anchor.ID, c.ID, a.ID, b.ID, tail.ID}; !slices.Equal(got, want) {
		t.Fatalf("backlog = %v, want %v", got, want)
	}

	// One event for the whole call, naming the stories that moved.
	select {
	case ev := <-evs:
		if ev.Client != clientID || !slices.Equal(eventIDs(ev), []int64{c.ID, a.ID, b.ID}) {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no change event published")
	}
	select {
	case ev := <-evs:
		t.Fatalf("second event %+v", ev)
	default:
	}

	// With neither neighbour the first goes to the top; next_id places the
	// first before that story.
	mustCall(t, cs, "move_stories", map[string]any{"ids": []int64{b.ID, a.ID}, "section": "icebox"}, &out)
	mustCall(t, cs, "move_stories", map[string]any{"ids": []int64{c.ID}, "section": "icebox", "next_id": a.ID}, &out)
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": "icebox"}, &list)
	if got, want := ids(list.Stories), []int64{b.ID, c.ID, a.ID}; !slices.Equal(got, want) {
		t.Fatalf("icebox = %v, want %v", got, want)
	}

	// Whole-call errors change nothing.
	if msg := call(t, cs, "move_stories", map[string]any{"ids": []int64{a.ID, b.ID}, "section": "backlog", "prev_id": a.ID}, nil); !strings.Contains(msg, "drop target") {
		t.Fatalf("drop target among moved: %q", msg)
	}
	if msg := call(t, cs, "move_stories", map[string]any{"ids": []int64{}, "section": "backlog"}, nil); !strings.Contains(msg, "between 1 and 50") {
		t.Fatalf("no ids: %q", msg)
	}
	tooMany := make([]int64, 51)
	for i := range tooMany {
		tooMany[i] = a.ID
	}
	if msg := call(t, cs, "move_stories", map[string]any{"ids": tooMany, "section": "backlog"}, nil); !strings.Contains(msg, "between 1 and 50") {
		t.Fatalf("51 ids: %q", msg)
	}
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": "icebox"}, &list)
	if got, want := ids(list.Stories), []int64{b.ID, c.ID, a.ID}; !slices.Equal(got, want) {
		t.Fatalf("icebox after refused calls = %v, want %v", got, want)
	}

	// Kim is not a member: every story reads as not found, nothing moves.
	if err := call(t, f.connect(t, f.kim), "move_stories", map[string]any{"ids": []int64{a.ID}, "section": "backlog"}, &out); err != "" || out.Failed != 1 || !strings.Contains(out.Results[0].Error, "not found") {
		t.Fatalf("non-member: %q %+v", err, out)
	}
}

// TestNewStoryPlacement: the tool descriptions say where a new story lands;
// check that against create_story.
func TestNewStoryPlacement(t *testing.T) {
	for _, want := range []string{"top of the icebox", "bottom of backlog or current"} {
		if !strings.Contains(newStoryPlacement, want) {
			t.Fatalf("newStoryPlacement = %q, want it to say %q", newStoryPlacement, want)
		}
	}
	f := newFixture(t)
	ctx := context.Background()
	f.deps.Projects.Create(ctx, f.admin.ID, project.Input{Name: ptr("Apollo")})
	cs := f.connect(t, f.admin)
	for _, sec := range []string{"icebox", "backlog", "current"} {
		var first, second story.Story
		args := map[string]any{"project": "apollo", "title": sec + " 1"}
		if sec != "icebox" {
			args["section"] = sec
		}
		mustCall(t, cs, "create_story", args, &first)
		args["title"] = sec + " 2"
		mustCall(t, cs, "create_story", args, &second)
		var list storiesOut
		mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": sec}, &list)
		want := []int64{first.ID, second.ID} // bottom
		if sec == "icebox" {
			want = []int64{second.ID, first.ID} // top
		}
		if got := ids(list.Stories); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", sec, got, want)
		}
	}
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if strings.HasPrefix(tool.Name, "create_stor") && !strings.Contains(tool.Description, newStoryPlacement) {
			t.Errorf("%s description does not state the placement: %q", tool.Name, tool.Description)
		}
	}
}

func TestCreateStories(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.deps.Projects.Create(ctx, f.admin.ID, project.Input{Name: ptr("Apollo")}) // kim is not a member
	existing, _ := f.deps.Stories.Create(ctx, p.ID, f.admin.ID, story.CreateInput{Title: "Existing idea"})
	evs, unsubscribe := f.deps.Events.Subscribe(p.ID)
	defer unsubscribe()
	cs := f.connect(t, f.admin)

	// A plan in the icebox: it lands on top in the order given. Item 2 fails
	// validation, so item 4, which waits on it, fails too; item 5 points
	// forward. The rest are created and linked.
	var out bulkOut
	mustCall(t, cs, "create_stories", map[string]any{"project": "apollo", "items": []map[string]any{
		{"title": "Design", "estimate": 1, "labels": []string{"plan"}},
		{"title": "Build", "estimate": 3, "blocked_by_items": []int{0}, "blocked_by": []int64{existing.ID}},
		{"title": ""},
		{"title": "Fix it", "type": "bug", "blocked_by_items": []int{0, 1}},
		{"title": "Ship", "blocked_by_items": []int{2}},
		{"title": "Ahead", "blocked_by_items": []int{6}},
	}}, &out)
	if out.Succeeded != 3 || out.Failed != 3 || len(out.Results) != 6 {
		t.Fatalf("out = %+v", out)
	}
	for i, wantErr := range []string{"", "", "title", "", "item 2 was not created", "not an earlier item"} {
		r := out.Results[i]
		if wantErr == "" {
			if !r.OK || r.ID == 0 || r.Section != story.SectionIcebox || r.Title == "" {
				t.Errorf("result %d = %+v", i, r)
			}
		} else if r.OK || r.ID != 0 || !strings.Contains(r.Error, wantErr) {
			t.Errorf("result %d = %+v, want error containing %q", i, r, wantErr)
		}
	}
	design, build, fix := out.Results[0].ID, out.Results[1].ID, out.Results[3].ID
	var list storiesOut
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": "icebox"}, &list)
	if got, want := ids(list.Stories), []int64{design, build, fix, existing.ID}; !slices.Equal(got, want) {
		t.Fatalf("icebox = %v, want %v", got, want)
	}
	var d story.Detail
	mustCall(t, cs, "get_story", map[string]any{"id": build}, &d)
	if !slices.Equal(d.BlockedBy, []int64{existing.ID, design}) || d.RequesterID != f.admin.ID || len(d.Activity) == 0 {
		t.Fatalf("build = %+v", d.Story)
	}
	mustCall(t, cs, "get_story", map[string]any{"id": fix}, &d)
	if !slices.Equal(d.BlockedBy, []int64{design, build}) || d.Type != story.TypeBug {
		t.Fatalf("fix = %+v", d.Story)
	}
	select {
	case ev := <-evs:
		if ev.Client != clientID || !slices.Equal(eventIDs(ev), []int64{design, build, fix}) {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no change event published")
	}
	select {
	case ev := <-evs:
		t.Fatalf("second event %+v", ev)
	default:
	}

	// Backlog and current: after what is there, in the order given.
	for _, sec := range []string{"backlog", "current"} {
		mustCall(t, cs, "create_stories", map[string]any{"project": p.ID, "section": sec, "items": []map[string]any{{"title": sec + " 1"}, {"title": sec + " 2"}}}, nil)
		mustCall(t, cs, "create_stories", map[string]any{"project": p.ID, "section": sec, "items": []map[string]any{{"title": sec + " 3"}, {"title": sec + " 4"}}}, nil)
		mustCall(t, cs, "list_stories", map[string]any{"project": "apollo", "section": sec}, &list)
		var titles []string
		for _, st := range list.Stories {
			titles = append(titles, st.Title)
		}
		if got, want := strings.Join(titles, ","), fmt.Sprintf("%[1]s 1,%[1]s 2,%[1]s 3,%[1]s 4", sec); got != want {
			t.Errorf("%s = %s, want %s", sec, got, want)
		}
	}

	// create_story takes blocked_by too.
	var single story.Story
	mustCall(t, cs, "create_story", map[string]any{"project": "apollo", "title": "Follow-up", "blocked_by": []int64{design}}, &single)
	if !slices.Equal(single.BlockedBy, []int64{design}) {
		t.Fatalf("single = %+v", single)
	}

	// Whole-call errors create nothing.
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo"}, &list)
	before := len(list.Stories)
	one := []map[string]any{{"title": "x"}}
	if msg := call(t, cs, "create_stories", map[string]any{"project": "apollo", "items": []map[string]any{}}, nil); !strings.Contains(msg, "between 1 and 50") {
		t.Fatalf("no items: %q", msg)
	}
	tooMany := make([]map[string]any, 51)
	for i := range tooMany {
		tooMany[i] = map[string]any{"title": "x"}
	}
	if msg := call(t, cs, "create_stories", map[string]any{"project": "apollo", "items": tooMany}, nil); !strings.Contains(msg, "between 1 and 50") {
		t.Fatalf("51 items: %q", msg)
	}
	if msg := call(t, cs, "create_stories", map[string]any{"project": "apollo", "section": "done", "items": one}, nil); !strings.Contains(msg, "section") {
		t.Fatalf("bad section: %q", msg)
	}
	if msg := call(t, f.connect(t, f.kim), "create_stories", map[string]any{"project": "apollo", "items": one}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("non-member: %q", msg)
	}
	if _, err := f.deps.Projects.SetArchived(ctx, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if msg := call(t, cs, "create_stories", map[string]any{"project": "apollo", "items": one}, nil); !strings.Contains(msg, "archived") {
		t.Fatalf("archived: %q", msg)
	}
	mustCall(t, cs, "list_stories", map[string]any{"project": "apollo"}, &list)
	if len(list.Stories) != before {
		t.Fatalf("refused calls created stories: %d, want %d", len(list.Stories), before)
	}
}

func TestUpdateStories(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.deps.Projects.Create(ctx, f.admin.ID, project.Input{Name: ptr("Apollo")}) // kim is not a member
	q, _ := f.deps.Projects.Create(ctx, f.admin.ID, project.Input{Name: ptr("Gemini")})
	mk := func(projectID int64, title string) story.Story {
		t.Helper()
		st, err := f.deps.Stories.Create(ctx, projectID, f.admin.ID, story.CreateInput{Title: title, Section: story.SectionBacklog})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	a, b, c := mk(p.ID, "A"), mk(p.ID, "B"), mk(p.ID, "C")
	g := mk(q.ID, "G")
	trashed := mk(p.ID, "Trashed")
	if _, err := f.deps.Stories.Delete(ctx, trashed.ID, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	evP, unsubP := f.deps.Events.Subscribe(p.ID)
	defer unsubP()
	evQ, unsubQ := f.deps.Events.Subscribe(q.ID)
	defer unsubQ()
	cs := f.connect(t, f.admin)

	// a waits on c, which is updated later in the same call; items from two
	// projects; the refused ones leave the rest alone.
	var out bulkOut
	mustCall(t, cs, "update_stories", map[string]any{"items": []map[string]any{
		{"id": a.ID, "estimate": 2, "labels": []string{"plan"}, "blocked_by": []int64{c.ID}},
		{"id": trashed.ID, "title": "no"},
		{"id": b.ID, "estimate": 4},
		{"id": g.ID, "title": "G renamed", "owner_id": f.kim.ID},
		{"id": 999, "title": "no"},
		{"id": c.ID, "type": "chore", "state": "started"},
	}}, &out)
	if out.Succeeded != 3 || out.Failed != 3 {
		t.Fatalf("out = %+v", out)
	}
	for i, wantErr := range []string{"", "trash", "estimate", "", "not found", ""} {
		r := out.Results[i]
		if wantErr == "" {
			if !r.OK || r.Title == "" || r.State == "" {
				t.Errorf("result %d = %+v", i, r)
			}
		} else if r.OK || !strings.Contains(r.Error, wantErr) {
			t.Errorf("result %d = %+v, want error containing %q", i, r, wantErr)
		}
	}
	if r := out.Results[5]; r.ID != c.ID || r.State != story.StateStarted || r.Section != story.SectionCurrent {
		t.Fatalf("c = %+v", r)
	}
	var d story.Detail
	mustCall(t, cs, "get_story", map[string]any{"id": a.ID}, &d)
	if *d.Estimate != 2 || !slices.Equal(d.Labels, []string{"plan"}) || !slices.Equal(d.BlockedBy, []int64{c.ID}) {
		t.Fatalf("a = %+v", d.Story)
	}
	mustCall(t, cs, "get_story", map[string]any{"id": b.ID}, &d)
	if d.Estimate != nil {
		t.Fatalf("refused item b changed: %+v", d.Story)
	}
	mustCall(t, cs, "get_story", map[string]any{"id": g.ID}, &d)
	if d.Title != "G renamed" || *d.OwnerID != f.kim.ID {
		t.Fatalf("g = %+v", d.Story)
	}

	// One event per project, naming the changed stories and new blockers.
	for _, tc := range []struct {
		ch   <-chan events.Event
		want []int64
	}{{evP, []int64{a.ID, c.ID}}, {evQ, []int64{g.ID}}} {
		select {
		case ev := <-tc.ch:
			if ev.Client != clientID || !slices.Equal(eventIDs(ev), tc.want) {
				t.Fatalf("event = %+v, want ids %v", ev, tc.want)
			}
		case <-time.After(time.Second):
			t.Fatal("no change event published")
		}
		select {
		case ev := <-tc.ch:
			t.Fatalf("second event %+v", ev)
		default:
		}
	}

	// Whole-call errors and access.
	if msg := call(t, cs, "update_stories", map[string]any{"items": []map[string]any{}}, nil); !strings.Contains(msg, "between 1 and 50") {
		t.Fatalf("no items: %q", msg)
	}
	tooMany := make([]map[string]any, 51)
	for i := range tooMany {
		tooMany[i] = map[string]any{"id": b.ID, "title": "x"}
	}
	if msg := call(t, cs, "update_stories", map[string]any{"items": tooMany}, nil); !strings.Contains(msg, "between 1 and 50") {
		t.Fatalf("51 items: %q", msg)
	}
	mustCall(t, cs, "get_story", map[string]any{"id": b.ID}, &d)
	if d.Title != "B" {
		t.Fatalf("refused call changed b: %+v", d.Story)
	}
	if msg := call(t, f.connect(t, f.kim), "update_stories", map[string]any{"items": []map[string]any{{"id": b.ID, "title": "x"}}}, &out); msg != "" || out.Failed != 1 || !strings.Contains(out.Results[0].Error, "not found") {
		t.Fatalf("non-member: %q %+v", msg, out)
	}
}

func TestCreateProject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	kim := f.connect(t, f.kim)

	var p project.Project
	mustCall(t, kim, "create_project", map[string]any{
		"name": "  Gemini ", "description": "Second programme", "iteration_length_days": 14,
		"iteration_start_weekday": 3, "estimate_bugs_and_chores": true,
	}, &p)
	if p.ID == 0 || p.Name != "Gemini" || p.Slug != "gemini" || p.Description != "Second programme" ||
		p.IterationLengthDays != 14 || p.IterationStartWeekday != 3 || !p.EstimateBugsAndChores ||
		p.VelocityWindow != project.DefaultVelocityWindow || p.CombineIceboxBacklog {
		t.Fatalf("created %+v", p)
	}

	// The caller owns it: it is in their list, they can file stories in it,
	// and it is not open to anyone else.
	members, err := f.deps.Projects.Members(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].UserID != f.kim.ID || members[0].Role != project.RoleOwner {
		t.Fatalf("members = %+v", members)
	}
	var projects projectsOut
	mustCall(t, kim, "list_projects", nil, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].ID != p.ID {
		t.Fatalf("kim sees %+v", projects)
	}
	var st story.Story
	mustCall(t, kim, "create_story", map[string]any{"project": "gemini", "title": "First"}, &st)
	if st.ProjectID != p.ID || st.RequesterID != f.kim.ID {
		t.Fatalf("story %+v", st)
	}
	other, err := f.auth.Register(ctx, auth.RegisterInput{Email: "lee@example.com", Password: "third password", DisplayName: "Lee"})
	if err != nil {
		t.Fatal(err)
	}
	if msg := call(t, f.connect(t, other), "list_stories", map[string]any{"project": "gemini"}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("non-member list: %q", msg)
	}

	// Defaults, a taken name and the service's validation.
	var dup project.Project
	mustCall(t, kim, "create_project", map[string]any{"name": "Gemini"}, &dup)
	if dup.Slug != "gemini-2" || dup.IterationLengthDays != project.DefaultIterationLengthDays || dup.IterationStartWeekday != project.DefaultStartWeekday {
		t.Fatalf("second Gemini %+v", dup)
	}
	if msg := call(t, kim, "create_project", map[string]any{"name": " "}, nil); !strings.Contains(msg, "name is required") {
		t.Fatalf("blank name: %q", msg)
	}
	if msg := call(t, kim, "create_project", map[string]any{"name": "X", "iteration_length_days": 10}, nil); !strings.Contains(msg, "iteration length") {
		t.Fatalf("bad iteration length: %q", msg)
	}
}

func TestMembershipIsEnforced(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// A project belongs to its creator; kim is not a member. A project with no
	// members at all is open to everyone.
	p, _ := f.deps.Projects.Create(ctx, f.admin.ID, project.Input{Name: ptr("Private")})
	open, _ := f.deps.Projects.Create(ctx, 0, project.Input{Name: ptr("Open")})
	st, _ := f.deps.Stories.Create(ctx, p.ID, f.admin.ID, story.CreateInput{Title: "Secret"})

	kim := f.connect(t, f.kim)
	var projects projectsOut
	mustCall(t, kim, "list_projects", nil, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].ID != open.ID {
		t.Fatalf("kim sees %+v", projects)
	}
	if msg := call(t, kim, "list_stories", map[string]any{"project": "private"}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("non-member list: %q", msg)
	}

	// Archived projects are hidden from list_projects unless asked for, still
	// readable, and refuse writes.
	if _, err := f.deps.Projects.SetArchived(ctx, open.ID, true); err != nil {
		t.Fatal(err)
	}
	mustCall(t, kim, "list_projects", nil, &projects)
	if len(projects.Projects) != 0 {
		t.Fatalf("archived project listed: %+v", projects)
	}
	mustCall(t, kim, "list_projects", map[string]any{"include_archived": true}, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].ArchivedAt == nil {
		t.Fatalf("include_archived: %+v", projects)
	}
	mustCall(t, kim, "list_stories", map[string]any{"project": "open"}, nil)
	if msg := call(t, kim, "create_story", map[string]any{"project": "open", "title": "no"}, nil); !strings.Contains(msg, "archived") {
		t.Fatalf("write to archived project: %q", msg)
	}
	if _, err := f.deps.Projects.SetArchived(ctx, open.ID, false); err != nil {
		t.Fatal(err)
	}
	if msg := call(t, kim, "get_story", map[string]any{"id": st.ID}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("non-member get: %q", msg)
	}
	if _, err := kim.ReadResource(ctx, &mcp.ReadResourceParams{URI: "trackstar://projects/private/backlog"}); err == nil {
		t.Fatal("non-member resource should fail")
	}

	// Once a member, kim reads and writes.
	if err := f.deps.Projects.SetMember(ctx, p.ID, f.kim.ID, project.RoleMember); err != nil {
		t.Fatal(err)
	}
	var d story.Detail
	mustCall(t, kim, "get_story", map[string]any{"id": st.ID}, &d)
	mustCall(t, kim, "update_story", map[string]any{"id": st.ID, "title": "edited by kim"}, &d)
	if d.Title != "edited by kim" {
		t.Fatalf("member update: %+v", d)
	}
}

// eventIDs reads the stories an event names, from story_id or story_ids.
func eventIDs(ev events.Event) []int64 {
	if ev.StoryID != 0 {
		return []int64{ev.StoryID}
	}
	return ev.StoryIDs
}

func ids(stories []story.Story) []int64 {
	out := make([]int64, len(stories))
	for i, s := range stories {
		out[i] = s.ID
	}
	return out
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestRequiresUser: a session whose context carries no user gets a clean
// tool error rather than acting as nobody.
func TestRequiresUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(f.deps).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if msg := call(t, cs, "list_projects", nil, nil); !strings.Contains(msg, "not signed in") {
		t.Fatalf("no user: %q", msg)
	}
	if msg := call(t, cs, "list_stories", map[string]any{"project": "x"}, nil); !strings.Contains(msg, "not signed in") && !strings.Contains(msg, "not found") {
		t.Fatalf("no user: %q", msg)
	}
}
