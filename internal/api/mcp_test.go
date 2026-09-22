package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"trackstar/internal/auth"
)

type bearerTransport struct {
	token string
	next  http.RoundTripper
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.next.RoundTrip(r)
}

// TestMCPOverHTTP drives the real /mcp endpoint with the SDK's Streamable
// HTTP client: no credentials is 401, an API token works end to end and
// changes show up in the REST API.
func TestMCPOverHTTP(t *testing.T) {
	_, ts := newServer(t, true)
	browser := newClient(t, ts)
	browser.register("sam@example.com")
	var created auth.CreatedToken
	browser.must(http.StatusCreated, "POST", "/api/me/tokens", map[string]any{"name": "agent"}, &created)

	ctx := context.Background()
	connect := func(token string) (*mcp.ClientSession, error) {
		transport := &mcp.StreamableClientTransport{
			Endpoint:             ts.URL + "/mcp",
			HTTPClient:           &http.Client{Transport: bearerTransport{token: token, next: http.DefaultTransport}},
			MaxRetries:           -1,
			DisableStandaloneSSE: true, // the server is stateless: GET is 405
		}
		return mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, transport, nil)
	}

	if _, err := connect(""); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("no credentials: err = %v", err)
	}
	if _, err := connect("tst_bogus"); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("bad token: err = %v", err)
	}

	cs, err := connect(created.Secret)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if cs.InitializeResult().ServerInfo.Name != "trackstar" {
		t.Fatalf("server info = %+v", cs.InitializeResult().ServerInfo)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects"})
	if err != nil || res.IsError {
		t.Fatalf("list_projects: %+v, %v", res, err)
	}
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_story", Arguments: map[string]any{"project": "apollo", "title": "From MCP"}})
	if err != nil || !res.IsError {
		t.Fatalf("create in missing project: %+v, %v", res, err)
	}
	browser.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, nil)
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_story", Arguments: map[string]any{"project": "apollo", "title": "From MCP", "section": "backlog"}})
	if err != nil || res.IsError {
		t.Fatalf("create_story: %+v, %v", res, err)
	}
	var stories []map[string]any
	browser.must(http.StatusOK, "GET", "/api/projects/apollo/stories", nil, &stories)
	if len(stories) != 1 || stories[0]["title"] != "From MCP" || stories[0]["section"] != "backlog" {
		t.Fatalf("stories via REST = %v", stories)
	}

	// Stateless: no session id is required, so a fresh client works at once
	// and a stale one keeps working after the server "restarts".
	cs2, err := connect(created.Secret)
	if err != nil {
		t.Fatal(err)
	}
	cs2.Close()
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_stories", Arguments: map[string]any{"project": "apollo"}})
	if err != nil || res.IsError {
		t.Fatalf("list_stories: %+v, %v", res, err)
	}

	// Revoking the token cuts the agent off immediately.
	browser.must(http.StatusNoContent, "DELETE", "/api/me/tokens/1", nil, nil)
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects"}); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("after revoke: err = %v", err)
	}
}
