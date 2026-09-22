package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
	want := []string{"add_comment", "create_story", "get_story", "list_epics", "list_projects", "list_stories", "list_users", "move_story", "update_story", "velocity"}
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
	p, err := f.deps.Projects.Create(ctx, project.Input{Name: ptr("Apollo")})
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

func TestMembershipIsEnforced(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, _ := f.deps.Projects.Create(ctx, project.Input{Name: ptr("Private")})
	open, _ := f.deps.Projects.Create(ctx, project.Input{Name: ptr("Open")})
	// A project with members is members-only; kim is not one.
	if err := f.deps.Projects.SetMember(ctx, p.ID, f.admin.ID, project.RoleMember); err != nil {
		t.Fatal(err)
	}
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
	if msg := call(t, kim, "get_story", map[string]any{"id": st.ID}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("non-member get: %q", msg)
	}
	if _, err := kim.ReadResource(ctx, &mcp.ReadResourceParams{URI: "trackstar://projects/private/backlog"}); err == nil {
		t.Fatal("non-member resource should fail")
	}

	// A viewer reads but cannot write.
	if err := f.deps.Projects.SetMember(ctx, p.ID, f.kim.ID, project.RoleViewer); err != nil {
		t.Fatal(err)
	}
	var d story.Detail
	mustCall(t, kim, "get_story", map[string]any{"id": st.ID}, &d)
	for name, args := range map[string]map[string]any{
		"create_story": {"project": "private", "title": "x"},
		"update_story": {"id": st.ID, "title": "x"},
		"move_story":   {"id": st.ID, "section": "backlog"},
		"add_comment":  {"story_id": st.ID, "body": "x"},
	} {
		if msg := call(t, kim, name, args, nil); !strings.Contains(msg, "read-only") {
			t.Errorf("viewer %s: %q", name, msg)
		}
	}
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
