package api

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"trackstar/internal/auth"
	"trackstar/internal/user"
)

func TestAPITokens(t *testing.T) {
	_, ts := newServer(t, true)
	browser := newClient(t, ts)
	browser.register("sam@example.com")

	// Mint a token through the browser session.
	var created auth.CreatedToken
	browser.must(http.StatusCreated, "POST", "/api/me/tokens", map[string]any{"name": "claude"}, &created)
	if !strings.HasPrefix(created.Secret, auth.TokenPrefix) || created.Name != "claude" {
		t.Fatalf("created = %+v", created)
	}
	browser.must(http.StatusUnprocessableEntity, "POST", "/api/me/tokens", map[string]any{"name": ""}, nil)
	browser.must(http.StatusUnprocessableEntity, "POST", "/api/me/tokens", map[string]any{"name": "x", "bogus": 1}, nil)

	// A client with no cookies, only the bearer header.
	jar, _ := cookiejar.New(nil)
	script := &client{t: t, http: &http.Client{Jar: jar}, base: ts.URL}
	bearer := []string{"Authorization", "Bearer " + created.Secret}

	if got := script.do("GET", "/api/me", nil, nil); got != http.StatusUnauthorized {
		t.Fatalf("no credentials: %d", got)
	}
	var me user.User
	if got := script.do("GET", "/api/me", nil, &me, bearer...); got != http.StatusOK || me.Email != "sam@example.com" {
		t.Fatalf("bearer /api/me: %d %+v", got, me)
	}
	if got := script.do("GET", "/api/me", nil, nil, "Authorization", "Bearer "+created.Secret+"x"); got != http.StatusUnauthorized {
		t.Fatalf("tampered bearer: %d", got)
	}
	if got := script.do("GET", "/api/me", nil, nil, "Authorization", "Basic abc"); got != http.StatusUnauthorized {
		t.Fatalf("non-bearer scheme: %d", got)
	}

	// Tokens act as their owner on project data, and the activity log
	// records the owner.
	if got := script.do("POST", "/api/projects", map[string]any{"name": "Apollo"}, nil, bearer...); got != http.StatusCreated {
		t.Fatalf("bearer create project: %d", got)
	}
	var st struct {
		ID          int64 `json:"id"`
		RequesterID int64 `json:"requester_id"`
	}
	if got := script.do("POST", "/api/projects/1/stories", map[string]any{"title": "From a script"}, &st, bearer...); got != http.StatusCreated || st.RequesterID != me.ID {
		t.Fatalf("bearer create story: %d %+v", got, st)
	}

	// Account and token management need a browser session.
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/me/tokens", nil},
		{"POST", "/api/me/tokens", map[string]any{"name": "more"}},
		{"DELETE", "/api/me/tokens/1", nil},
		{"PATCH", "/api/me", map[string]any{"display_name": "Mallory"}},
		{"PATCH", "/api/users/1", map[string]any{"is_admin": false}},
		{"POST", "/api/users/1/password", map[string]any{"password": "hijacked pw"}},
	} {
		if got := script.do(tc.method, tc.path, tc.body, nil, bearer...); got != http.StatusForbidden {
			t.Errorf("bearer %s %s: %d, want 403", tc.method, tc.path, got)
		}
	}

	// The header wins over a cookie on the same request.
	kim := newClient(t, ts)
	kim.register("kim@example.com")
	var who user.User
	if got := kim.do("GET", "/api/me", nil, &who, bearer...); got != http.StatusOK || who.Email != "sam@example.com" {
		t.Fatalf("bearer over cookie: %d %+v", got, who)
	}

	// Listing shows metadata, never the secret, and last_used_at was recorded.
	var list []map[string]any
	browser.must(http.StatusOK, "GET", "/api/me/tokens", nil, &list)
	if len(list) != 1 || list[0]["name"] != "claude" || list[0]["last_used_at"] == nil {
		t.Fatalf("list = %v", list)
	}
	if _, leaked := list[0]["token"]; leaked {
		t.Fatalf("secret in listing: %v", list)
	}
	var kimList []map[string]any
	kim.must(http.StatusOK, "GET", "/api/me/tokens", nil, &kimList)
	if len(kimList) != 0 {
		t.Fatalf("kim sees sam's tokens: %v", kimList)
	}

	// Revocation: only the owner can, and the token stops working at once.
	kim.must(http.StatusNotFound, "DELETE", "/api/me/tokens/1", nil, nil)
	browser.must(http.StatusNoContent, "DELETE", "/api/me/tokens/1", nil, nil)
	if got := script.do("GET", "/api/me", nil, nil, bearer...); got != http.StatusUnauthorized {
		t.Fatalf("revoked bearer: %d", got)
	}
}

func TestLoginRateLimitDoesNotApplyToBearer(t *testing.T) {
	srv, ts := newServer(t, true)
	browser := newClient(t, ts)
	browser.register("sam@example.com")
	var created auth.CreatedToken
	browser.must(http.StatusCreated, "POST", "/api/me/tokens", map[string]any{"name": "ci"}, &created)

	// Exhaust the limiter for this client address.
	for range 25 {
		srv.loginLimiter.Allow("127.0.0.1")
	}
	jar, _ := cookiejar.New(nil)
	script := &client{t: t, http: &http.Client{Jar: jar}, base: ts.URL}
	if got := script.do("GET", "/api/projects", nil, nil, "Authorization", "Bearer "+created.Secret); got != http.StatusOK {
		t.Fatalf("bearer while limited: %d", got)
	}
}
