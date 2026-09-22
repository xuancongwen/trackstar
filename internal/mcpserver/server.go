// Package mcpserver exposes Trackstar to AI agents over the Model Context
// Protocol. It is a thin layer over the service packages, exactly like
// internal/api: every tool calls the same service method the HTTP handler
// calls, so a tool can do nothing the API cannot, and the same access rules,
// validation and activity log apply.
//
// One server is built at startup and served stateless over Streamable HTTP,
// so each tool call is one ordinary HTTP request. The authenticated user
// travels in the request context (WithUser), which the SDK carries through
// to tool and resource handlers.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"trackstar/internal/apperr"
	"trackstar/internal/events"
	"trackstar/internal/project"
	"trackstar/internal/story"
	"trackstar/internal/user"
	"trackstar/internal/velocity"
)

// Deps are the services the tools are built on.
type Deps struct {
	Projects *project.Service
	Stories  *story.Service
	Velocity *velocity.Service
	Users    *user.Service
	Events   *events.Hub // may be nil
	Logger   *slog.Logger
	Version  string
}

// clientID marks change events published by MCP tools; browsers ignore only
// their own id, so every open board refetches.
const clientID = "mcp"

const instructions = `Trackstar is a Pivotal-Tracker-style project tracker. Stories belong to a
project and sit in one of four sections: icebox (ideas), backlog (planned,
ordered by priority), current (this iteration) and done (accepted in an
earlier iteration). Within a section the order matters: the top of the
backlog is what happens next.

Story workflow: icebox/backlog -> unstarted -> started -> finished ->
delivered -> accepted (or rejected, which goes back to started). Set "state"
with update_story to advance work; use move_story to change section or
priority. A move names a neighbour (prev_id or next_id) in the target
section, or neither to put the story at the top. Started or later stories
stay in the current section; accepted stories cannot be moved.

Types are feature, bug and chore. Only features carry an estimate (points).
Labels are free text; an epic is a label with a description and progress.
Projects can be named by numeric id or by slug. Users are referenced by id;
list_users maps ids to names. Every change is recorded in the story's
activity log under the token owner's name.`

type server struct {
	deps Deps
}

type ctxKey struct{}

// WithUser marks ctx as acting for u. Every tool and resource handler
// requires it.
func WithUser(ctx context.Context, u user.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func userFrom(ctx context.Context) (user.User, error) {
	u, ok := ctx.Value(ctxKey{}).(user.User)
	if !ok || u.ID == 0 {
		return user.User{}, apperr.Unauthorized("not signed in")
	}
	return u, nil
}

// New builds the MCP server. Tool registration infers and compiles JSON
// schemas (a few milliseconds), so build it once and share it; the acting
// user comes from each request's context.
func New(deps Deps) *mcp.Server {
	s := &server{deps: deps}
	srv := mcp.NewServer(&mcp.Implementation{Name: "trackstar", Title: "Trackstar", Version: deps.Version}, &mcp.ServerOptions{Instructions: instructions})
	s.addTools(srv)
	s.addResources(srv)
	return srv
}

// Handler serves stateless Streamable HTTP. It must sit behind
// authentication middleware: userOf reads the authenticated user from the
// request, and a request without one is refused.
func Handler(deps Deps, userOf func(*http.Request) (user.User, bool)) http.Handler {
	srv := New(deps)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := userOf(r)
		if !ok {
			http.Error(w, "not signed in", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

// --- helpers ----------------------------------------------------------------------------

// fail converts a service error into a tool error the model can read. Kinds
// the API maps to 4xx keep their message; anything else is logged and hidden.
func (s *server) fail(err error) error {
	if apperr.KindOf(err) != 0 {
		return err
	}
	if s.deps.Logger != nil {
		s.deps.Logger.Error("mcp tool error", "error", err)
	}
	return errors.New("internal server error")
}

// authorize mirrors the API's rule: no read access reads as "not found" so a
// project's existence is not disclosed; write on a viewer role is forbidden.
func (s *server) authorize(ctx context.Context, projectID int64, write bool) error {
	u, err := userFrom(ctx)
	if err != nil {
		return err
	}
	access, err := s.deps.Projects.AccessFor(ctx, projectID, u.ID, u.IsAdmin)
	if err != nil {
		return err
	}
	if !access.Read {
		return apperr.NotFound("project")
	}
	if write && !access.Write {
		return apperr.Forbidden("you have read-only access to this project")
	}
	return nil
}

// resolveProject accepts a numeric id or a slug and checks access.
func (s *server) resolveProject(ctx context.Context, ref string, write bool) (project.Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return project.Project{}, apperr.Invalid("project is required (numeric id or slug)")
	}
	p, err := s.deps.Projects.Resolve(ctx, ref)
	if err != nil {
		return project.Project{}, err
	}
	return p, s.authorize(ctx, p.ID, write)
}

// storyProject checks access to the project that owns story id.
func (s *server) storyProject(ctx context.Context, id int64, write bool) (int64, error) {
	if id < 1 {
		return 0, apperr.NotFound("story")
	}
	projectID, err := s.deps.Stories.ProjectOfStory(ctx, id)
	if err != nil {
		return 0, err
	}
	return projectID, s.authorize(ctx, projectID, write)
}

func (s *server) publish(projectID, storyID int64) {
	if s.deps.Events != nil {
		s.deps.Events.Publish(events.Event{Type: "stories", ProjectID: projectID, StoryID: storyID, Client: clientID})
	}
}

func parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// jsonText renders v as the text content of a resource.
func jsonText(uri string, v any) (*mcp.ReadResourceResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", uri, err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(data)}}}, nil
}
