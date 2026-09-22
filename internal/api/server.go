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

	"tracker/internal/auth"
	"tracker/internal/project"
	"tracker/internal/story"
	"tracker/internal/user"
	"tracker/internal/velocity"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	authed := func(pattern string, h func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, s.requireUser(http.HandlerFunc(h)))
	}
	authed("GET /api/me", s.handleMe)
	authed("GET /api/users", s.handleListUsers)
	authed("GET /api/system/info", s.handleSystemInfo)

	authed("GET /api/projects", s.handleListProjects)
	authed("POST /api/projects", s.handleCreateProject)
	authed("GET /api/projects/{project}", s.handleGetProject)
	authed("PATCH /api/projects/{project}", s.handleUpdateProject)
	authed("DELETE /api/projects/{project}", s.handleDeleteProject)
	authed("GET /api/projects/{project}/stories", s.handleListStories)
	authed("POST /api/projects/{project}/stories", s.handleCreateStory)
	authed("GET /api/projects/{project}/labels", s.handleListLabels)
	authed("GET /api/projects/{project}/iterations", s.handleListIterations)
	authed("GET /api/projects/{project}/velocity", s.handleVelocity)

	authed("GET /api/stories/{id}", s.handleGetStory)
	authed("PATCH /api/stories/{id}", s.handleUpdateStory)
	authed("DELETE /api/stories/{id}", s.handleDeleteStory)
	authed("POST /api/stories/{id}/move", s.handleMoveStory)
	authed("POST /api/stories/{id}/comments", s.handleCreateComment)
	authed("DELETE /api/comments/{id}", s.handleDeleteComment)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	mux.Handle("/", s.frontendHandler())

	var h http.Handler = mux
	h = s.checkOrigin(h)
	h = securityHeaders(h)
	h = s.recoverPanics(h)
	h = s.logRequests(h)
	return h
}
