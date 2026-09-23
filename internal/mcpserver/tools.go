package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"trackstar/internal/apperr"
	"trackstar/internal/project"
	"trackstar/internal/story"
	"trackstar/internal/velocity"
)

func ptr[T any](v T) *T { return &v }

var (
	readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	additive = &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}
	// updates change existing data but never delete it (the activity log
	// keeps the old value); calling twice with the same input is a no-op.
	updates = &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)}
)

// ProjectRef names a project by numeric id or slug. Agents send either a
// string or a number; both are accepted and the schema says so.
type ProjectRef string

func (p *ProjectRef) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*p = ProjectRef(s)
		return nil
	}
	*p = ProjectRef(strings.TrimSpace(string(data)))
	return nil
}

var typeSchemas = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[ProjectRef](): {Types: []string{"string", "integer"}, Description: "project id or slug"},
}

// tool registers a typed handler with a schema inferred from In, honouring
// typeSchemas (the SDK's default inference does not take options).
func tool[In, Out any](srv *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	schema, err := jsonschema.For[In](&jsonschema.ForOptions{TypeSchemas: typeSchemas})
	if err != nil {
		panic(err) // a programming error in an input struct, caught by the tests
	}
	t.InputSchema = schema
	mcp.AddTool(srv, t, h)
}

func (s *server) addTools(srv *mcp.Server) {
	tool(srv, &mcp.Tool{Name: "list_projects", Description: "List the projects you can see, with their iteration settings. Archived projects (read-only, archived_at set) are left out unless include_archived is true.", Annotations: readOnly}, s.listProjects)
	tool(srv, &mcp.Tool{Name: "list_users", Description: "List user accounts (id, name, email) so owner_id and requester_id can be resolved to people.", Annotations: readOnly}, s.listUsers)
	tool(srv, &mcp.Tool{Name: "list_stories", Description: "List a project's stories in board order, optionally one section only or matching a search. Live stories (icebox, backlog, current) come back together unless section is given; section \"done\" returns stories accepted in earlier iterations.", Annotations: readOnly}, s.listStories)
	tool(srv, &mcp.Tool{Name: "get_story", Description: "Get one story with its comments, tasks and activity history.", Annotations: readOnly}, s.getStory)
	tool(srv, &mcp.Tool{Name: "create_story", Description: "Create a story in a project. New stories go to the bottom of the icebox unless section is given.", Annotations: additive}, s.createStory)
	tool(srv, &mcp.Tool{Name: "update_story", Description: "Change a story's fields or advance its workflow state. Omitted fields are left alone.", Annotations: updates}, s.updateStory)
	tool(srv, &mcp.Tool{Name: "move_story", Description: "Move a story to a section and position: after prev_id, before next_id, or to the top of the section when neither is given.", Annotations: updates}, s.moveStory)
	tool(srv, &mcp.Tool{Name: "add_comment", Description: "Add a comment to a story.", Annotations: additive}, s.addComment)
	tool(srv, &mcp.Tool{Name: "list_epics", Description: "List a project's epics with their progress (accepted vs total points and stories).", Annotations: readOnly}, s.listEpics)
	tool(srv, &mcp.Tool{Name: "velocity", Description: "A project's velocity and its iteration history (points accepted per iteration, including the current one).", Annotations: readOnly}, s.velocity)
}

// --- projects and users ----------------------------------------------------------------

type listProjectsIn struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"also list archived (read-only) projects"`
}

type projectsOut struct {
	Projects []project.Project `json:"projects"`
}

func (s *server) listProjects(ctx context.Context, _ *mcp.CallToolRequest, in listProjectsIn) (*mcp.CallToolResult, projectsOut, error) {
	u, err := userFrom(ctx)
	if err != nil {
		return nil, projectsOut{}, err
	}
	projects, err := s.deps.Projects.ListVisible(ctx, u.ID, u.IsAdmin)
	if err != nil {
		return nil, projectsOut{}, s.fail(err)
	}
	if !in.IncludeArchived {
		active := projects[:0]
		for _, p := range projects {
			if !p.Archived() {
				active = append(active, p)
			}
		}
		projects = active
	}
	return nil, projectsOut{Projects: projects}, nil
}

type userOut struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsActive    bool   `json:"is_active"`
}

type usersOut struct {
	Users []userOut `json:"users"`
	Me    int64     `json:"me" jsonschema:"the id of the user this token acts as"`
}

func (s *server) listUsers(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, usersOut, error) {
	me, err := userFrom(ctx)
	if err != nil {
		return nil, usersOut{}, err
	}
	users, err := s.deps.Users.List(ctx)
	if err != nil {
		return nil, usersOut{}, s.fail(err)
	}
	out := usersOut{Users: make([]userOut, len(users)), Me: me.ID}
	for i, u := range users {
		out.Users[i] = userOut{ID: u.ID, DisplayName: u.DisplayName, Email: u.Email, IsActive: u.IsActive}
	}
	return nil, out, nil
}

// --- stories ----------------------------------------------------------------------------

type projectRef struct {
	Project ProjectRef `json:"project"`
}

type listStoriesIn struct {
	projectRef
	Section string `json:"section,omitempty" jsonschema:"icebox, backlog, current or done; omit for all live stories"`
	Query   string `json:"query,omitempty" jsonschema:"substring to search in titles and descriptions (includes done stories)"`
}

type storiesOut struct {
	Stories []story.Story `json:"stories"`
}

func (s *server) listStories(ctx context.Context, _ *mcp.CallToolRequest, in listStoriesIn) (*mcp.CallToolResult, storiesOut, error) {
	p, err := s.resolveProject(ctx, string(in.Project), false)
	if err != nil {
		return nil, storiesOut{}, s.fail(err)
	}
	section := story.Section(in.Section)
	switch section {
	case "", story.SectionIcebox, story.SectionBacklog, story.SectionCurrent, story.SectionDone:
	default:
		return nil, storiesOut{}, apperr.Invalid("section must be icebox, backlog, current or done")
	}
	stories, err := s.deps.Stories.List(ctx, p.ID, story.ListOptions{Query: in.Query, Done: section == story.SectionDone})
	if err != nil {
		return nil, storiesOut{}, s.fail(err)
	}
	if section != "" && section != story.SectionDone {
		stories = slices.DeleteFunc(stories, func(st story.Story) bool { return st.Section != section })
	}
	return nil, storiesOut{Stories: stories}, nil
}

type storyID struct {
	ID int64 `json:"id" jsonschema:"story id"`
}

func (s *server) getStory(ctx context.Context, _ *mcp.CallToolRequest, in storyID) (*mcp.CallToolResult, story.Detail, error) {
	if _, err := s.storyProject(ctx, in.ID, false); err != nil {
		return nil, story.Detail{}, s.fail(err)
	}
	d, err := s.deps.Stories.Get(ctx, in.ID)
	if err != nil {
		return nil, story.Detail{}, s.fail(err)
	}
	return nil, d, nil
}

type createStoryIn struct {
	projectRef
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type,omitempty" jsonschema:"feature (default), bug or chore"`
	Estimate    *int64   `json:"estimate,omitempty" jsonschema:"points; features only, unless the project's estimate_bugs_and_chores is on"`
	Section     string   `json:"section,omitempty" jsonschema:"icebox (default), backlog or current"`
	OwnerID     *int64   `json:"owner_id,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

func (s *server) createStory(ctx context.Context, _ *mcp.CallToolRequest, in createStoryIn) (*mcp.CallToolResult, story.Story, error) {
	p, err := s.resolveProject(ctx, string(in.Project), true)
	if err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	u, _ := userFrom(ctx) // resolveProject already required it
	st, err := s.deps.Stories.Create(ctx, p.ID, u.ID, story.CreateInput{
		Title: in.Title, Description: in.Description, Type: story.Type(in.Type), Estimate: in.Estimate,
		Section: story.Section(in.Section), OwnerID: in.OwnerID, Labels: in.Labels,
	})
	if err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	s.publish(st.ProjectID, st.ID)
	return nil, st, nil
}

type updateStoryIn struct {
	ID            int64     `json:"id" jsonschema:"story id"`
	Title         *string   `json:"title,omitempty"`
	Description   *string   `json:"description,omitempty"`
	Type          *string   `json:"type,omitempty" jsonschema:"feature, bug or chore"`
	State         *string   `json:"state,omitempty" jsonschema:"icebox, backlog, unstarted, started, finished, delivered, accepted or rejected"`
	Estimate      *int64    `json:"estimate,omitempty" jsonschema:"points; use clear_estimate to remove"`
	ClearEstimate bool      `json:"clear_estimate,omitempty"`
	OwnerID       *int64    `json:"owner_id,omitempty" jsonschema:"use clear_owner to unassign"`
	ClearOwner    bool      `json:"clear_owner,omitempty"`
	RequesterID   *int64    `json:"requester_id,omitempty"`
	Labels        *[]string `json:"labels,omitempty" jsonschema:"the complete label list (replaces the current one)"`
	BlockedBy     *[]int64  `json:"blocked_by,omitempty" jsonschema:"ids of stories this one waits on (replaces the current list)"`
}

func (s *server) updateStory(ctx context.Context, _ *mcp.CallToolRequest, in updateStoryIn) (*mcp.CallToolResult, story.Story, error) {
	if _, err := s.storyProject(ctx, in.ID, true); err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	upd := story.UpdateInput{Title: in.Title, Description: in.Description, RequesterID: in.RequesterID, Labels: in.Labels, BlockedBy: in.BlockedBy}
	if in.Type != nil {
		upd.Type = ptr(story.Type(*in.Type))
	}
	if in.State != nil {
		upd.State = ptr(story.State(*in.State))
	}
	switch {
	case in.ClearEstimate:
		upd.Estimate = story.Null[int64]()
	case in.Estimate != nil:
		upd.Estimate = story.Some(*in.Estimate)
	}
	switch {
	case in.ClearOwner:
		upd.OwnerID = story.Null[int64]()
	case in.OwnerID != nil:
		upd.OwnerID = story.Some(*in.OwnerID)
	}
	u, _ := userFrom(ctx) // storyProject already required it
	st, err := s.deps.Stories.Update(ctx, in.ID, story.Actor{ID: u.ID, IsAdmin: u.IsAdmin}, upd)
	if err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	s.publish(st.ProjectID, st.ID)
	if in.BlockedBy != nil {
		for _, b := range *in.BlockedBy {
			s.publish(st.ProjectID, b)
		}
	}
	return nil, st, nil
}

type moveStoryIn struct {
	ID      int64  `json:"id" jsonschema:"story id"`
	Section string `json:"section" jsonschema:"icebox, backlog or current"`
	PrevID  *int64 `json:"prev_id,omitempty" jsonschema:"place directly after this story"`
	NextID  *int64 `json:"next_id,omitempty" jsonschema:"place directly before this story"`
}

func (s *server) moveStory(ctx context.Context, _ *mcp.CallToolRequest, in moveStoryIn) (*mcp.CallToolResult, story.MoveResult, error) {
	if _, err := s.storyProject(ctx, in.ID, true); err != nil {
		return nil, story.MoveResult{}, s.fail(err)
	}
	u, _ := userFrom(ctx) // storyProject already required it
	res, err := s.deps.Stories.Move(ctx, in.ID, u.ID, story.MoveInput{Section: story.Section(in.Section), PrevID: in.PrevID, NextID: in.NextID})
	if err != nil {
		return nil, story.MoveResult{}, s.fail(err)
	}
	s.publish(res.Story.ProjectID, res.Story.ID)
	return nil, res, nil
}

type addCommentIn struct {
	StoryID int64  `json:"story_id"`
	Body    string `json:"body"`
}

func (s *server) addComment(ctx context.Context, _ *mcp.CallToolRequest, in addCommentIn) (*mcp.CallToolResult, story.Comment, error) {
	projectID, err := s.storyProject(ctx, in.StoryID, true)
	if err != nil {
		return nil, story.Comment{}, s.fail(err)
	}
	u, _ := userFrom(ctx) // storyProject already required it
	c, err := s.deps.Stories.AddComment(ctx, in.StoryID, u.ID, in.Body)
	if err != nil {
		return nil, story.Comment{}, s.fail(err)
	}
	s.publish(projectID, in.StoryID)
	return nil, c, nil
}

// --- epics and velocity -------------------------------------------------------------------

type epicsOut struct {
	Epics []story.Epic `json:"epics"`
}

func (s *server) listEpics(ctx context.Context, _ *mcp.CallToolRequest, in projectRef) (*mcp.CallToolResult, epicsOut, error) {
	p, err := s.resolveProject(ctx, string(in.Project), false)
	if err != nil {
		return nil, epicsOut{}, s.fail(err)
	}
	epics, err := s.deps.Stories.Epics(ctx, p.ID)
	if err != nil {
		return nil, epicsOut{}, s.fail(err)
	}
	return nil, epicsOut{Epics: epics}, nil
}

type velocityOut struct {
	velocity.Result
	History []velocity.IterationSummary `json:"history"`
}

func (s *server) velocity(ctx context.Context, _ *mcp.CallToolRequest, in projectRef) (*mcp.CallToolResult, velocityOut, error) {
	p, err := s.resolveProject(ctx, string(in.Project), false)
	if err != nil {
		return nil, velocityOut{}, s.fail(err)
	}
	res, err := s.deps.Velocity.Velocity(ctx, p.ID)
	if err != nil {
		return nil, velocityOut{}, s.fail(err)
	}
	history, err := s.deps.Velocity.Iterations(ctx, p.ID)
	if err != nil {
		return nil, velocityOut{}, s.fail(err)
	}
	return nil, velocityOut{Result: res, History: history}, nil
}
