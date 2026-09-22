package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/bcrypt"

	"trackstar/internal/auth"
	"trackstar/internal/database"
	"trackstar/internal/project"
	"trackstar/internal/story"
	"trackstar/internal/user"
	"trackstar/internal/velocity"
)

type client struct {
	t    *testing.T
	http *http.Client
	base string
}

func newServer(t *testing.T, allowRegistration bool) (*Server, *httptest.Server) {
	t.Helper()
	db := database.NewTestDB(t)
	publicURL, _ := url.Parse("https://track.example.com/")
	srv := &Server{
		Auth:           auth.NewService(db, auth.Options{Secret: []byte("0123456789abcdef0123456789abcdef"), AllowRegistration: allowRegistration, BcryptCost: bcrypt.MinCost}),
		Users:          user.NewService(db, nil),
		Projects:       project.NewService(db, nil),
		Stories:        story.NewService(db, time.UTC, nil),
		Velocity:       velocity.NewService(db, time.UTC, nil),
		DB:             db,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		PublicURL:      publicURL,
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Version:        "test",
		Frontend: fstest.MapFS{
			"index.html":    {Data: []byte("<html>app</html>")},
			"assets/app.js": {Data: []byte("console.log(1)")},
		},
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, http: &http.Client{Jar: jar}, base: ts.URL}
}

// do sends a JSON request and decodes the response into out (when non-nil).
func (c *client) do(method, path string, body, out any, headers ...string) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	return resp.StatusCode
}

func (c *client) must(want int, method, path string, body, out any) {
	c.t.Helper()
	if got := c.do(method, path, body, out); got != want {
		c.t.Fatalf("%s %s: status %d, want %d", method, path, got, want)
	}
}

func (c *client) register(email string) {
	c.t.Helper()
	c.must(http.StatusCreated, "POST", "/api/auth/register", map[string]string{"email": email, "password": "correct horse", "display_name": "Sam"}, nil)
}

func TestHealthIsPublic(t *testing.T) {
	_, ts := newServer(t, true)
	var out map[string]string
	newClient(t, ts).must(http.StatusOK, "GET", "/health", nil, &out)
	if out["status"] != "ok" {
		t.Fatalf("health = %v", out)
	}
}

func TestEndToEndWorkflow(t *testing.T) {
	_, ts := newServer(t, true)
	c := newClient(t, ts)

	c.must(http.StatusUnauthorized, "GET", "/api/me", nil, nil)
	c.must(http.StatusUnauthorized, "GET", "/api/projects", nil, nil)
	c.register("sam@example.com")

	var me user.User
	c.must(http.StatusOK, "GET", "/api/me", nil, &me)
	if me.Email != "sam@example.com" || !me.IsAdmin {
		t.Fatalf("me = %+v", me)
	}

	var p project.Project
	c.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, &p)
	c.must(http.StatusOK, "GET", "/api/projects/apollo", nil, &p) // by slug
	c.must(http.StatusOK, "PATCH", "/api/projects/1", map[string]any{"velocity_window": 4}, &p)
	if p.VelocityWindow != 4 || p.Name != "Apollo" {
		t.Fatalf("project = %+v", p)
	}
	c.must(http.StatusUnprocessableEntity, "POST", "/api/projects", map[string]any{"name": ""}, nil)
	c.must(http.StatusUnprocessableEntity, "POST", "/api/projects", map[string]any{"name": "x", "bogus": 1}, nil)

	var a, b story.Story
	c.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "A", "estimate": 3, "section": "backlog"}, &a)
	c.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "B", "type": "bug"}, &b)
	if a.State != story.StateBacklog || b.State != story.StateIcebox {
		t.Fatalf("states = %s, %s", a.State, b.State)
	}

	var moved story.MoveResult
	c.must(http.StatusOK, "POST", "/api/stories/2/move", map[string]any{"section": "backlog", "next_id": a.ID}, &moved)
	if moved.Story.State != story.StateBacklog || moved.Story.Position >= a.Position {
		t.Fatalf("moved = %+v (A at %d)", moved.Story, a.Position)
	}
	c.must(http.StatusUnprocessableEntity, "POST", "/api/stories/2/move", map[string]any{"section": "done"}, nil)

	for _, state := range []string{"started", "finished", "delivered", "accepted"} {
		c.must(http.StatusOK, "PATCH", "/api/stories/1", map[string]any{"state": state}, &a)
	}
	if a.AcceptedAt == nil || a.OwnerID == nil {
		t.Fatalf("accepted story = %+v", a)
	}
	c.must(http.StatusUnprocessableEntity, "PATCH", "/api/stories/1", map[string]any{"state": "started"}, nil)
	c.must(http.StatusOK, "PATCH", "/api/stories/2", map[string]any{"owner_id": nil, "estimate": nil, "labels": []string{"ops"}}, &b)

	c.must(http.StatusCreated, "POST", "/api/stories/2/comments", map[string]any{"body": "hello"}, nil)
	var detail story.Detail
	c.must(http.StatusOK, "GET", "/api/stories/2", nil, &detail)
	if len(detail.Comments) != 1 || len(detail.Labels) != 1 {
		t.Fatalf("detail = %+v", detail)
	}

	var list []story.Story
	c.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, &list)
	if len(list) != 2 {
		t.Fatalf("stories = %d, want 2", len(list))
	}
	c.must(http.StatusOK, "GET", "/api/projects/1/stories?q=hello-nothing", nil, &list)
	if len(list) != 0 {
		t.Fatalf("search results = %d, want 0", len(list))
	}

	var v velocity.Result
	c.must(http.StatusOK, "GET", "/api/projects/1/velocity", nil, &v)
	if !v.Estimated || v.Window != 4 {
		t.Fatalf("velocity = %+v", v)
	}
	var its []velocity.IterationSummary
	c.must(http.StatusOK, "GET", "/api/projects/1/iterations", nil, &its)
	if len(its) != 1 || !its[0].Current || its[0].Points != 3 {
		t.Fatalf("iterations = %+v", its)
	}

	c.must(http.StatusOK, "DELETE", "/api/stories/2", nil, nil)
	c.must(http.StatusOK, "GET", "/api/projects/1/stories", nil, &list)
	if len(list) != 1 {
		t.Fatalf("stories after delete = %d, want 1", len(list))
	}
	c.must(http.StatusOK, "GET", "/api/projects/1/stories?section=deleted", nil, &list)
	if len(list) != 1 || list[0].DeletedAt == nil {
		t.Fatalf("trash = %+v", list)
	}
	c.must(http.StatusOK, "POST", "/api/stories/2/restore", nil, &b)
	if b.DeletedAt != nil || b.State != story.StateBacklog {
		t.Fatalf("restored = %+v", b)
	}
	c.must(http.StatusUnprocessableEntity, "POST", "/api/stories/2/restore", nil, nil)
	c.must(http.StatusOK, "DELETE", "/api/stories/2", nil, nil)
	c.must(http.StatusNotFound, "DELETE", "/api/stories/2", nil, nil)
	c.must(http.StatusNotFound, "GET", "/api/stories/abc", nil, nil)
	c.must(http.StatusNotFound, "GET", "/api/nope", nil, nil)

	c.must(http.StatusNoContent, "POST", "/api/auth/logout", nil, nil)
	c.must(http.StatusUnauthorized, "GET", "/api/me", nil, nil)
	c.must(http.StatusUnauthorized, "POST", "/api/auth/login", map[string]string{"email": "sam@example.com", "password": "wrong"}, nil)
	c.must(http.StatusOK, "POST", "/api/auth/login", map[string]string{"email": "sam@example.com", "password": "correct horse"}, nil)
	c.must(http.StatusOK, "GET", "/api/me", nil, nil)
}

func TestSessionCookieAttributes(t *testing.T) {
	_, ts := newServer(t, true)
	body := strings.NewReader(`{"email":"sam@example.com","password":"correct horse"}`)
	resp, err := http.Post(ts.URL+"/api/auth/register", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	// Public URL is https, so Secure must be set even though this hop is plain HTTP.
	if c := cookies[0]; !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("cookie = %+v", c)
	}
}

func TestRegistrationClosedAndAdminOnly(t *testing.T) {
	_, ts := newServer(t, false)
	admin := newClient(t, ts)
	admin.register("admin@example.com") // first account is always allowed

	var cfg map[string]any
	admin.must(http.StatusOK, "GET", "/api/config", nil, &cfg)
	if cfg["allow_registration"] != false {
		t.Fatalf("config = %v", cfg)
	}
	other := newClient(t, ts)
	other.must(http.StatusForbidden, "POST", "/api/auth/register", map[string]string{"email": "two@example.com", "password": "correct horse"}, nil)

	var info map[string]any
	admin.must(http.StatusOK, "GET", "/api/system/info", nil, &info)
	if info["database_driver"] != "sqlite" || info["version"] != "test" {
		t.Fatalf("info = %v", info)
	}
}

func TestSystemInfoForbiddenForRegularUsers(t *testing.T) {
	_, ts := newServer(t, true)
	newClient(t, ts).register("admin@example.com")
	regular := newClient(t, ts)
	regular.register("dev@example.com")
	regular.must(http.StatusForbidden, "GET", "/api/system/info", nil, nil)

	admin := newClient(t, ts)
	admin.must(http.StatusOK, "POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct horse"}, nil)
	admin.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, nil)
	regular.must(http.StatusForbidden, "DELETE", "/api/projects/1", nil, nil)
	admin.must(http.StatusNoContent, "DELETE", "/api/projects/1", nil, nil)
}

func TestCrossOriginWritesAreRejected(t *testing.T) {
	_, ts := newServer(t, true)
	c := newClient(t, ts)
	body := map[string]string{"email": "sam@example.com", "password": "correct horse"}

	if got := c.do("POST", "/api/auth/register", body, nil, "Origin", "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("evil origin: status %d, want 403", got)
	}
	if got := c.do("POST", "/api/auth/register", body, nil, "Origin", "https://track.example.com"); got != http.StatusCreated {
		t.Fatalf("public origin: status %d, want 201", got)
	}
	if got := c.do("POST", "/api/auth/logout", nil, nil, "Origin", ts.URL); got != http.StatusNoContent {
		t.Fatalf("same-host origin: status %d, want 204", got)
	}
}

func TestClientIPTrustsOnlyConfiguredProxies(t *testing.T) {
	srv, _ := newServer(t, true)
	cases := []struct {
		name, remote, cf, xff, want string
	}{
		{"direct client cannot spoof", "203.0.113.9:1234", "1.1.1.1", "2.2.2.2", "203.0.113.9"},
		{"cloudflare header via trusted proxy", "10.0.0.5:1234", "198.51.100.7", "", "198.51.100.7"},
		{"xff rightmost untrusted hop", "10.0.0.5:1234", "", "6.6.6.6, 198.51.100.7, 10.0.0.9", "198.51.100.7"},
		{"trusted proxy without headers", "10.0.0.5:1234", "", "", "10.0.0.5"},
		{"garbage header ignored", "10.0.0.5:1234", "not-an-ip", "also-bad", "10.0.0.5"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.cf != "" {
			r.Header.Set("CF-Connecting-IP", c.cf)
		}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := srv.clientIP(r); got != c.want {
			t.Errorf("%s: clientIP = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFrontendFallback(t *testing.T) {
	_, ts := newServer(t, true)
	get := func(path string) (*http.Response, string) {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, string(data)
	}

	for _, path := range []string{"/", "/projects/apollo", "/assets/missing.js"} {
		if resp, body := get(path); resp.StatusCode != 200 || body != "<html>app</html>" {
			t.Errorf("GET %s = %d %q, want the app shell", path, resp.StatusCode, body)
		}
	}
	resp, body := get("/assets/app.js")
	if body != "console.log(1)" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: body %q, cache-control %q", body, resp.Header.Get("Cache-Control"))
	}
	if resp, _ := get("/"); resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("X-Request-ID") == "" {
		t.Errorf("missing security/request-id headers: %v", resp.Header)
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, ts := newServer(t, true)
	c := newClient(t, ts)
	last := 0
	for i := 0; i < 25; i++ {
		last = c.do("POST", "/api/auth/login", map[string]string{"email": "x@example.com", "password": "nope"}, nil)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status after 25 attempts = %d, want 429", last)
	}
}
