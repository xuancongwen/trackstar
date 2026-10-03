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
	tool(srv, &mcp.Tool{Name: "create_stories", Description: "Create up to 50 stories in one project and section, keeping the order given: the first goes where create_story would put it (top of the icebox, bottom of backlog or current) and each next one right after the previous one created. Use it to file a plan. Items can wait on earlier items of the same call (blocked_by_items). Each item succeeds or fails on its own; the result reports each.", Annotations: additive}, s.createStories)
	tool(srv, &mcp.Tool{Name: "update_story", Description: "Change a story's fields or advance its workflow state. Omitted fields are left alone.", Annotations: updates}, s.updateStory)
	tool(srv, &mcp.Tool{Name: "update_stories", Description: "Change up to 50 stories in one call, from any projects you can write to. Each item takes update_story's fields; omitted fields are left alone. Each item succeeds or fails on its own; the result reports each.", Annotations: updates}, s.updateStories)
	tool(srv, &mcp.Tool{Name: "move_story", Description: "Move a story to a section and position: after prev_id, before next_id, or to the top of the section when neither is given.", Annotations: updates}, s.moveStory)
	tool(srv, &mcp.Tool{Name: "move_stories", Description: "Move up to 50 stories of one project to a section, in the order given: the first goes after prev_id, before next_id, or to the top of the section, and each next one right after the previous one moved. A story that cannot be moved is reported in its result and the others still move.", Annotations: updates}, s.moveStories)
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
	BlockedBy   []int64  `json:"blocked_by,omitempty" jsonschema:"ids of stories this one waits on"`
}

func (s *server) createStory(ctx context.Context, _ *mcp.CallToolRequest, in createStoryIn) (*mcp.CallToolResult, story.Story, error) {
	p, err := s.resolveProject(ctx, string(in.Project), true)
	if err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	u, _ := userFrom(ctx) // resolveProject already required it
	st, err := s.deps.Stories.Create(ctx, p.ID, u.ID, story.CreateInput{
		Title: in.Title, Description: in.Description, Type: story.Type(in.Type), Estimate: in.Estimate,
		Section: story.Section(in.Section), OwnerID: in.OwnerID, Labels: in.Labels, BlockedBy: in.BlockedBy,
	})
	if err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	s.publish(st.ProjectID, st.ID)
	for _, b := range in.BlockedBy {
		s.publish(st.ProjectID, b)
	}
	return nil, st, nil
}

// itemBlockers resolves item i's blocked_by_items to the ids created for
// those earlier items and appends them to its blocked_by.
func itemBlockers(i int, it createItem, created []int64) ([]int64, error) {
	ids := slices.Clone(it.BlockedBy)
	for _, j := range it.BlockedByItems {
		if j < 0 || j >= i {
			return nil, apperr.Invalid("blocked_by_items: %d is not an earlier item", j)
		}
		if created[j] == 0 {
			return nil, apperr.Invalid("blocked_by_items: item %d was not created", j)
		}
		ids = append(ids, created[j])
	}
	return ids, nil
}

type createItem struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Type           string   `json:"type,omitempty" jsonschema:"feature (default), bug or chore"`
	Estimate       *int64   `json:"estimate,omitempty" jsonschema:"points; features only, unless the project's estimate_bugs_and_chores is on"`
	OwnerID        *int64   `json:"owner_id,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	BlockedBy      []int64  `json:"blocked_by,omitempty" jsonschema:"ids of existing stories this one waits on"`
	BlockedByItems []int    `json:"blocked_by_items,omitempty" jsonschema:"0-based indexes of earlier items in this call that this one waits on"`
}

type createStoriesIn struct {
	projectRef
	Section string       `json:"section,omitempty" jsonschema:"icebox (default), backlog or current; the same for every item"`
	Items   []createItem `json:"items" jsonschema:"the stories, in the order they should end up"`
}

// createStories creates each item on its own (one transaction per story), so
// one that fails does not hold back the rest. Links go to earlier items only,
// which are already created (or known to have failed) when an item's turn
// comes.
func (s *server) createStories(ctx context.Context, _ *mcp.CallToolRequest, in createStoriesIn) (*mcp.CallToolResult, bulkOut, error) {
	if err := checkBulkSize(len(in.Items)); err != nil {
		return nil, bulkOut{}, err
	}
	sec := story.Section(in.Section)
	if sec == "" {
		sec = story.SectionIcebox
	}
	if sec != story.SectionIcebox && sec != story.SectionBacklog && sec != story.SectionCurrent {
		return nil, bulkOut{}, apperr.Invalid("section must be icebox, backlog or current")
	}
	p, err := s.resolveProject(ctx, string(in.Project), true)
	if err != nil {
		return nil, bulkOut{}, s.fail(err)
	}
	u, _ := userFrom(ctx) // resolveProject already required it
	out := bulkOut{Results: make([]itemResult, 0, len(in.Items))}
	created := make([]int64, len(in.Items)) // 0: not created
	var ids []int64
	var after *int64
	for i, it := range in.Items {
		blockedBy, err := itemBlockers(i, it, created)
		if err != nil {
			out.add(s.itemFailed(0, err))
			continue
		}
		st, err := s.deps.Stories.Create(ctx, p.ID, u.ID, story.CreateInput{
			Title: it.Title, Description: it.Description, Type: story.Type(it.Type), Estimate: it.Estimate,
			Section: sec, OwnerID: it.OwnerID, Labels: it.Labels, BlockedBy: blockedBy, After: after,
		})
		if err != nil {
			out.add(s.itemFailed(0, err))
			continue
		}
		out.add(itemDone(st))
		created[i] = st.ID
		ids = append(ids, st.ID)
		after = &created[i]
	}
	s.publishMany(p.ID, ids)
	return nil, out, nil
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

func (in updateStoryIn) input() story.UpdateInput {
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
	return upd
}

func (s *server) updateStory(ctx context.Context, _ *mcp.CallToolRequest, in updateStoryIn) (*mcp.CallToolResult, story.Story, error) {
	if _, err := s.storyProject(ctx, in.ID, true); err != nil {
		return nil, story.Story{}, s.fail(err)
	}
	u, _ := userFrom(ctx) // storyProject already required it
	st, err := s.deps.Stories.Update(ctx, in.ID, story.Actor{ID: u.ID, IsAdmin: u.IsAdmin}, in.input())
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

// maxBulk is how many items one bulk tool call may carry.
const maxBulk = 50

// itemResult reports one item of a bulk call. Results come back in input
// order; a failed item carries the error and leaves the others alone.
type itemResult struct {
	ID      int64         `json:"id,omitempty"`
	OK      bool          `json:"ok"`
	Title   string        `json:"title,omitempty"`
	Section story.Section `json:"section,omitempty"`
	State   story.State   `json:"state,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type bulkOut struct {
	Results   []itemResult `json:"results"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
}

func (o *bulkOut) add(r itemResult) {
	o.Results = append(o.Results, r)
	if r.OK {
		o.Succeeded++
	} else {
		o.Failed++
	}
}

func (s *server) itemFailed(id int64, err error) itemResult {
	return itemResult{ID: id, Error: s.fail(err).Error()}
}

func itemDone(st story.Story) itemResult {
	return itemResult{ID: st.ID, OK: true, Title: st.Title, Section: st.Section, State: st.State}
}

func checkBulkSize(n int) error {
	if n == 0 || n > maxBulk {
		return apperr.Invalid("give between 1 and %d items", maxBulk)
	}
	return nil
}

type updateStoriesIn struct {
	Items []updateStoryIn `json:"items" jsonschema:"one entry per story, with update_story's fields"`
}

// updateStories applies each item on its own (one transaction per story), so
// one that fails does not hold back the rest. Stories may come from several
// projects; each project gets one event naming its changed stories and their
// new blockers.
func (s *server) updateStories(ctx context.Context, _ *mcp.CallToolRequest, in updateStoriesIn) (*mcp.CallToolResult, bulkOut, error) {
	if err := checkBulkSize(len(in.Items)); err != nil {
		return nil, bulkOut{}, err
	}
	u, err := userFrom(ctx)
	if err != nil {
		return nil, bulkOut{}, err
	}
	out := bulkOut{Results: make([]itemResult, 0, len(in.Items))}
	var projects []int64
	changed := map[int64][]int64{}
	for _, it := range in.Items {
		if _, err := s.storyProject(ctx, it.ID, true); err != nil {
			out.add(s.itemFailed(it.ID, err))
			continue
		}
		st, err := s.deps.Stories.Update(ctx, it.ID, story.Actor{ID: u.ID, IsAdmin: u.IsAdmin}, it.input())
		if err != nil {
			out.add(s.itemFailed(it.ID, err))
			continue
		}
		out.add(itemDone(st))
		if changed[st.ProjectID] == nil {
			projects = append(projects, st.ProjectID)
		}
		changed[st.ProjectID] = append(changed[st.ProjectID], st.ID)
		if it.BlockedBy != nil {
			changed[st.ProjectID] = append(changed[st.ProjectID], *it.BlockedBy...)
		}
	}
	for _, pid := range projects {
		s.publishMany(pid, uniq(changed[pid]))
	}
	return nil, out, nil
}

// uniq drops repeated ids, keeping the first occurrence.
func uniq(ids []int64) []int64 {
	seen := map[int64]bool{}
	return slices.DeleteFunc(ids, func(id int64) bool {
		if seen[id] {
			return true
		}
		seen[id] = true
		return false
	})
}

type moveStoriesIn struct {
	IDs     []int64 `json:"ids" jsonschema:"story ids, in the order they should end up"`
	Section string  `json:"section" jsonschema:"icebox, backlog or current"`
	PrevID  *int64  `json:"prev_id,omitempty" jsonschema:"place the first story directly after this one"`
	NextID  *int64  `json:"next_id,omitempty" jsonschema:"place the first story directly before this one"`
}

// moveStories moves each story on its own (one transaction per story), so
// one that cannot move does not hold back the rest. Every story must belong
// to the project of the first one that resolves.
func (s *server) moveStories(ctx context.Context, _ *mcp.CallToolRequest, in moveStoriesIn) (*mcp.CallToolResult, bulkOut, error) {
	if err := checkBulkSize(len(in.IDs)); err != nil {
		return nil, bulkOut{}, err
	}
	for _, id := range in.IDs {
		if (in.PrevID != nil && *in.PrevID == id) || (in.NextID != nil && *in.NextID == id) {
			return nil, bulkOut{}, apperr.Invalid("the drop target cannot be one of the moved stories")
		}
	}
	u, err := userFrom(ctx)
	if err != nil {
		return nil, bulkOut{}, err
	}
	out := bulkOut{Results: make([]itemResult, 0, len(in.IDs))}
	var projectID int64
	var moved []int64
	seen := map[int64]bool{}
	prev, next := in.PrevID, in.NextID
	for _, id := range in.IDs {
		if seen[id] {
			out.add(itemResult{ID: id, Error: "listed twice; moved at its first position"})
			continue
		}
		seen[id] = true
		pid, err := s.storyProject(ctx, id, true)
		if err != nil {
			out.add(s.itemFailed(id, err))
			continue
		}
		if projectID == 0 {
			projectID = pid
		} else if pid != projectID {
			out.add(itemResult{ID: id, Error: "belongs to another project than the stories before it"})
			continue
		}
		res, err := s.deps.Stories.Move(ctx, id, u.ID, story.MoveInput{Section: story.Section(in.Section), PrevID: prev, NextID: next})
		if err != nil {
			out.add(s.itemFailed(id, err))
			continue
		}
		out.add(itemDone(res.Story))
		moved = append(moved, id)
		prev, next = &id, nil
	}
	s.publishMany(projectID, moved)
	return nil, out, nil
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
