// Package api is the HTTP layer: routing, middleware and JSON handlers. It
// contains no business rules; those live in the service packages.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"trackstar/internal/auth"
	"trackstar/internal/events"
	"trackstar/internal/mcpserver"
	"trackstar/internal/project"
	"trackstar/internal/story"
	"trackstar/internal/user"
	"trackstar/internal/velocity"
)

// SystemDB is what the health and system-info endpoints need to know.
type SystemDB interface {
	Health(ctx context.Context) error
	SizeBytes() int64
	Driver() string
}

type Server struct {
	Auth     *auth.Service
	Users    *user.Service
	Projects *project.Service
	Stories  *story.Service
	Velocity *velocity.Service
	DB       SystemDB
	Events   *events.Hub
	Logger   *slog.Logger

	PublicURL      *url.URL
	TrustedProxies []netip.Prefix
	Version        string
	// Timezone is the IANA name iterations are cut in; the UI formats dates with it.
	Timezone string
	Frontend fs.FS

	started      time.Time
	loginLimiter *auth.Limiter
}

// Handler builds the complete HTTP handler.
func (s *Server) Handler() http.Handler {
	s.started = time.Now()
	s.loginLimiter = auth.NewLimiter(20, 5*time.Minute)
	if s.Events == nil {
		s.Events = events.NewHub()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	authed := func(pattern string, h func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, s.requireUser(http.HandlerFunc(h)))
	}
	session := func(pattern string, h func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, s.requireSession(http.HandlerFunc(h)))
	}
	authed("GET /api/me", s.handleMe)
	session("PATCH /api/me", s.handleUpdateMe)
	session("GET /api/me/tokens", s.handleListTokens)
	session("POST /api/me/tokens", s.handleCreateToken)
	session("DELETE /api/me/tokens/{id}", s.handleRevokeToken)
	authed("GET /api/users", s.handleListUsers)
	session("PATCH /api/users/{id}", s.handleUpdateUser)
	session("POST /api/users/{id}/password", s.handleSetUserPassword)
	authed("GET /api/system/info", s.handleSystemInfo)

	authed("GET /api/projects", s.handleListProjects)
	authed("POST /api/projects", s.handleCreateProject)
	authed("GET /api/projects/{project}", s.handleGetProject)
	authed("PATCH /api/projects/{project}", s.handleUpdateProject)
	authed("DELETE /api/projects/{project}", s.handleDeleteProject)
	authed("POST /api/projects/{project}/archive", s.handleArchiveProject)
	authed("DELETE /api/projects/{project}/archive", s.handleUnarchiveProject)
	authed("GET /api/projects/{project}/stories", s.handleListStories)
	authed("POST /api/projects/{project}/stories", s.handleCreateStory)
	authed("GET /api/projects/{project}/labels", s.handleListLabels)
	authed("GET /api/projects/{project}/iterations", s.handleListIterations)
	authed("GET /api/projects/{project}/velocity", s.handleVelocity)
	authed("GET /api/projects/{project}/events", s.handleEvents)
	authed("GET /api/projects/{project}/epics", s.handleListEpics)
	authed("POST /api/projects/{project}/epics", s.handleCreateEpic)
	authed("PATCH /api/epics/{id}", s.handleUpdateEpic)
	authed("DELETE /api/epics/{id}", s.handleDeleteEpic)
	authed("GET /api/projects/{project}/members", s.handleListMembers)
	authed("PUT /api/projects/{project}/members/{user}", s.handleSetMember)
	authed("DELETE /api/projects/{project}/members/{user}", s.handleRemoveMember)
	authed("GET /api/projects/{project}/filters", s.handleListFilters)
	authed("POST /api/projects/{project}/filters", s.handleSaveFilter)
	authed("DELETE /api/filters/{id}", s.handleDeleteFilter)
	authed("POST /api/stories/move", s.handleMoveStories)
	authed("POST /api/stories/{id}/tasks", s.handleCreateTask)
	authed("PATCH /api/tasks/{id}", s.handleUpdateTask)
	authed("DELETE /api/tasks/{id}", s.handleDeleteTask)

	authed("GET /api/stories/{id}", s.handleGetStory)
	authed("PATCH /api/stories/{id}", s.handleUpdateStory)
	authed("DELETE /api/stories/{id}", s.handleDeleteStory)
	authed("POST /api/stories/{id}/restore", s.handleRestoreStory)
	authed("POST /api/stories/{id}/move", s.handleMoveStory)
	authed("POST /api/stories/{id}/comments", s.handleCreateComment)
	authed("DELETE /api/comments/{id}", s.handleDeleteComment)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})

	// MCP for AI agents: the same services behind the same authentication.
	// Agents authenticate with an API token; a browser session works too.
	mux.Handle("/mcp", s.requireUser(mcpserver.Handler(mcpserver.Deps{
		Projects: s.Projects, Stories: s.Stories, Velocity: s.Velocity, Users: s.Users,
		Events: s.Events, Logger: s.Logger, Version: s.Version,
	}, func(r *http.Request) (user.User, bool) {
		u := currentUser(r.Context())
		return u, u.ID != 0
	})))
	mux.Handle("/", s.frontendHandler())

	var h http.Handler = mux
	h = s.checkOrigin(h)
	h = securityHeaders(h)
	h = s.recoverPanics(h)
	h = s.logRequests(h)
	return h
}
