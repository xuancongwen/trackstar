package mcpserver

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"trackstar/internal/apperr"
	"trackstar/internal/story"
)

// Resources let a client attach a project's current iteration or backlog as
// context without a tool call. URIs: trackstar://projects/{project}/current
// and trackstar://projects/{project}/backlog, where {project} is an id or slug.

const resourcePrefix = "trackstar://projects/"

func (s *server) addResources(srv *mcp.Server) {
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "current_iteration", Title: "Current iteration",
		Description: "The stories in a project's current iteration, in order, as JSON.",
		URITemplate: resourcePrefix + "{project}/current", MIMEType: "application/json",
	}, s.readSection)
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "backlog", Title: "Backlog",
		Description: "A project's backlog in priority order, as JSON.",
		URITemplate: resourcePrefix + "{project}/backlog", MIMEType: "application/json",
	}, s.readSection)
}

func (s *server) readSection(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	rest, ok := strings.CutPrefix(uri, resourcePrefix)
	ref, sec, found := strings.Cut(rest, "/")
	if !ok || !found || ref == "" || (sec != "current" && sec != "backlog") {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	p, err := s.resolveProject(ctx, ref, false)
	if apperr.KindOf(err) == apperr.KindNotFound {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if err != nil {
		return nil, s.fail(err)
	}
	stories, err := s.deps.Stories.List(ctx, p.ID, story.ListOptions{})
	if err != nil {
		return nil, s.fail(err)
	}
	var out []story.Story
	for _, st := range stories {
		if string(st.Section) == sec {
			out = append(out, st)
		}
	}
	if out == nil {
		out = []story.Story{}
	}
	return jsonText(uri, map[string]any{"project": p, "section": sec, "stories": out})
}
