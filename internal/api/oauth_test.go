package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"trackstar/internal/auth"
)

const (
	oauthRedirect = "https://claude.example/callback"
	oauthVerifier = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	// newServer's public URL; the test server itself listens elsewhere.
	oauthIssuer = "https://track.example.com"
)

func oauthChallenge() string {
	sum := sha256.Sum256([]byte(oauthVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// send is a request without the client helper's JSON conveniences: it
// returns the response headers and decodes a JSON body into out.
func send(t *testing.T, method, target string, body io.Reader, out any, headers ...string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, target, body)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, target, data, err)
		}
	}
	return resp
}

// postForm calls an OAuth endpoint the way a client on another origin does.
func postForm(t *testing.T, ts *httptest.Server, path string, form url.Values, out any) int {
	t.Helper()
	return send(t, "POST", ts.URL+path, strings.NewReader(form.Encode()), out,
		"Content-Type", "application/x-www-form-urlencoded", "Origin", "https://inspector.example").StatusCode
}

// mcpInitialize reports the status of an MCP initialize call with token.
func mcpInitialize(t *testing.T, ts *httptest.Server, token string) *http.Response {
	t.Helper()
	headers := []string{"Content-Type", "application/json", "Accept", "application/json, text/event-stream"}
	if token != "" {
		headers = append(headers, "Authorization", "Bearer "+token)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
	return send(t, "POST", ts.URL+"/mcp", strings.NewReader(body), nil, headers...)
}

func registerOAuthClient(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	var reg struct {
		ClientID string `json:"client_id"`
	}
	body := `{"client_name":"Claude","redirect_uris":["` + oauthRedirect + `"],"logo_uri":"https://claude.example/logo.png","token_endpoint_auth_method":"none"}`
	resp := send(t, "POST", ts.URL+"/oauth/register", strings.NewReader(body), &reg, "Content-Type", "application/json", "Origin", "https://inspector.example")
	if resp.StatusCode != http.StatusCreated || reg.ClientID == "" {
		t.Fatalf("register client: %d %+v", resp.StatusCode, reg)
	}
	return reg.ClientID
}

func authorizeBody(clientID string, approve bool) map[string]any {
	return map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect, "response_type": "code",
		"code_challenge": oauthChallenge(), "code_challenge_method": "S256",
		"state": "xyz", "resource": oauthIssuer + "/mcp", "scope": "", "approve": approve,
	}
}

// approve answers the consent screen as browser and returns the redirect's
// query parameters.
func approve(t *testing.T, browser *client, body map[string]any) url.Values {
	t.Helper()
	var out struct {
		RedirectTo string `json:"redirect_to"`
	}
	browser.must(http.StatusOK, "POST", "/api/oauth/authorize", body, &out)
	u, err := url.Parse(out.RedirectTo)
	if err != nil || !strings.HasPrefix(out.RedirectTo, oauthRedirect+"?") {
		t.Fatalf("redirect_to = %q", out.RedirectTo)
	}
	return u.Query()
}

func TestOAuthDiscovery(t *testing.T) {
	_, ts := newServer(t, true)
	const metadataURL = oauthIssuer + "/.well-known/oauth-protected-resource"

	// The 401 on /mcp is what starts a client's flow.
	resp := mcpInitialize(t, ts, "")
	if got := resp.Header.Get("WWW-Authenticate"); resp.StatusCode != http.StatusUnauthorized || got != `Bearer resource_metadata="`+metadataURL+`"` {
		t.Fatalf("unauthenticated /mcp: %d, WWW-Authenticate %q", resp.StatusCode, got)
	}
	for _, token := range []string{"tst_bogus", "tsa_bogus", "whatever"} {
		resp = mcpInitialize(t, ts, token)
		if got := resp.Header.Get("WWW-Authenticate"); resp.StatusCode != http.StatusUnauthorized || !strings.Contains(got, metadataURL) || !strings.Contains(got, `error="invalid_token"`) {
			t.Fatalf("/mcp with %s: %d, WWW-Authenticate %q", token, resp.StatusCode, got)
		}
	}
	// The API keeps its plain 401.
	if resp := send(t, "GET", ts.URL+"/api/me", nil, nil); resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != "" {
		t.Fatalf("/api/me: %d, WWW-Authenticate %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		var doc struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			BearerMethods        []string `json:"bearer_methods_supported"`
		}
		resp := send(t, "GET", ts.URL+path, nil, &doc)
		if resp.StatusCode != http.StatusOK || doc.Resource != oauthIssuer+"/mcp" || len(doc.AuthorizationServers) != 1 ||
			doc.AuthorizationServers[0] != oauthIssuer || len(doc.BearerMethods) != 1 || doc.BearerMethods[0] != "header" {
			t.Fatalf("%s = %d %+v", path, resp.StatusCode, doc)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(resp.Header.Get("Cache-Control"), "public") {
			t.Fatalf("%s headers: %v", path, resp.Header)
		}
	}

	var as map[string]any
	resp = send(t, "GET", ts.URL+"/.well-known/oauth-authorization-server", nil, &as)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("authorization server metadata: %d %v", resp.StatusCode, resp.Header)
	}
	for key, want := range map[string]string{
		"issuer":                 oauthIssuer,
		"authorization_endpoint": oauthIssuer + "/oauth/authorize",
		"token_endpoint":         oauthIssuer + "/oauth/token",
		"registration_endpoint":  oauthIssuer + "/oauth/register",
		"revocation_endpoint":    oauthIssuer + "/oauth/revoke",
	} {
		if as[key] != want {
			t.Errorf("%s = %v, want %s", key, as[key], want)
		}
	}
	for key, want := range map[string]string{
		"response_types_supported":              "code",
		"grant_types_supported":                 "authorization_code refresh_token",
		"code_challenge_methods_supported":      "S256",
		"token_endpoint_auth_methods_supported": "none",
	} {
		var got []string
		for _, v := range as[key].([]any) {
			got = append(got, v.(string))
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s = %v, want %s", key, got, want)
		}
	}

	// A document we do not have is a JSON 404, not the app shell.
	if resp := send(t, "GET", ts.URL+"/.well-known/openid-configuration", nil, nil); resp.StatusCode != http.StatusNotFound || !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		t.Fatalf("unknown well-known: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// Browser-based clients preflight the endpoints they call.
	for _, path := range []string{"/oauth/register", "/oauth/token", "/oauth/revoke", "/.well-known/oauth-authorization-server"} {
		resp := send(t, "OPTIONS", ts.URL+path, nil, nil, "Origin", "https://inspector.example", "Access-Control-Request-Method", "POST")
		if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Content-Type") {
			t.Fatalf("preflight %s: %d %v", path, resp.StatusCode, resp.Header)
		}
	}
	// The consent screen is the app.
	if resp := send(t, "GET", ts.URL+"/oauth/authorize?client_id=x", nil, nil); resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		t.Fatalf("/oauth/authorize: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestOAuthClientRegistrationOverHTTP(t *testing.T) {
	_, ts := newServer(t, true)
	post := func(body string) (int, map[string]any) {
		var out map[string]any
		resp := send(t, "POST", ts.URL+"/oauth/register", strings.NewReader(body), &out, "Content-Type", "application/json")
		return resp.StatusCode, out
	}

	status, out := post(`{"client_name":"Claude","redirect_uris":["` + oauthRedirect + `"],"grant_types":["authorization_code","refresh_token"],"contacts":["x@example.com"]}`)
	if status != http.StatusCreated || out["client_id"] == "" || out["client_name"] != "Claude" || out["token_endpoint_auth_method"] != "none" {
		t.Fatalf("register = %d %v", status, out)
	}
	if _, secret := out["client_secret"]; secret {
		t.Fatalf("a public client was given a secret: %v", out)
	}
	if status, out := post(`{"client_name":"x","redirect_uris":["http://evil.example/cb"]}`); status != http.StatusBadRequest || out["error"] != "invalid_redirect_uri" {
		t.Fatalf("bad redirect = %d %v", status, out)
	}
	if status, out := post(`{"client_name":"x"}`); status != http.StatusBadRequest || out["error"] != "invalid_redirect_uri" {
		t.Fatalf("no redirect = %d %v", status, out)
	}
	if status, out := post(`not json`); status != http.StatusBadRequest || out["error"] != "invalid_client_metadata" {
		t.Fatalf("not json = %d %v", status, out)
	}

	// Open to anyone, so bounded per address.
	last := 0
	for range registerLimit {
		last, _ = post(`{"redirect_uris":["` + oauthRedirect + `"]}`)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status after %d registrations = %d, want 429", registerLimit+4, last)
	}
}

func TestOAuthFlowOverHTTP(t *testing.T) {
	_, ts := newServer(t, true)
	browser := newClient(t, ts)
	browser.register("sam@example.com")
	clientID := registerOAuthClient(t, ts)
	query := func(body map[string]any) string {
		q := url.Values{}
		for k, v := range body {
			if s, ok := v.(string); ok {
				q.Set(k, s)
			}
		}
		return "/api/oauth/authorize?" + q.Encode()
	}

	// The consent screen needs a signed-in browser.
	stranger := newClient(t, ts)
	stranger.must(http.StatusUnauthorized, "GET", query(authorizeBody(clientID, false)), nil, nil)
	stranger.must(http.StatusUnauthorized, "POST", "/api/oauth/authorize", authorizeBody(clientID, true), nil)

	var info map[string]string
	browser.must(http.StatusOK, "GET", query(authorizeBody(clientID, false)), nil, &info)
	if info["client_name"] != "Claude" || info["redirect_host"] != "claude.example" || info["redirect_to"] != "" {
		t.Fatalf("authorize info = %v", info)
	}

	// An unknown client or an unregistered redirect URI is shown to the
	// user and never redirected to.
	for name, change := range map[string][2]string{
		"unknown client":        {"client_id", "nope"},
		"unregistered redirect": {"redirect_uri", "https://evil.example/callback"},
	} {
		body := authorizeBody(clientID, true)
		body[change[0]] = change[1]
		var out map[string]string
		if got := browser.do("GET", query(body), nil, &out); got != http.StatusUnprocessableEntity || out["redirect_to"] != "" || out["error"] == "" {
			t.Errorf("%s (info): %d %v", name, got, out)
		}
		out = nil
		if got := browser.do("POST", "/api/oauth/authorize", body, &out); got != http.StatusUnprocessableEntity || out["redirect_to"] != "" {
			t.Errorf("%s (decision): %d %v", name, got, out)
		}
	}
	// Everything else is the client's mistake and goes back to it, with state.
	for name, tc := range map[string][3]string{
		"implicit flow":   {"response_type", "token", "unsupported_response_type"},
		"no PKCE":         {"code_challenge", "", "invalid_request"},
		"plain PKCE":      {"code_challenge_method", "plain", "invalid_request"},
		"short challenge": {"code_challenge", "abc", "invalid_request"},
		"other resource":  {"resource", "https://other.example/mcp", "invalid_target"},
	} {
		body := authorizeBody(clientID, true)
		body[tc[0]] = tc[1]
		if q := approve(t, browser, body); q.Get("error") != tc[2] || q.Get("state") != "xyz" || q.Get("code") != "" {
			t.Errorf("%s: redirect query = %v", name, q)
		}
		var out map[string]string
		browser.must(http.StatusOK, "GET", query(body), nil, &out)
		if !strings.Contains(out["redirect_to"], "error="+tc[2]) {
			t.Errorf("%s (info): %v", name, out)
		}
	}

	// Deny.
	if q := approve(t, browser, authorizeBody(clientID, false)); q.Get("error") != "access_denied" || q.Get("state") != "xyz" || q.Get("code") != "" {
		t.Fatalf("deny: redirect query = %v", q)
	}
	// Another site cannot approve for the user.
	if got := browser.do("POST", "/api/oauth/authorize", authorizeBody(clientID, true), nil, "Origin", "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("cross-origin approval: %d, want 403", got)
	}

	// Approve, then exchange the code.
	q := approve(t, browser, authorizeBody(clientID, true))
	code := q.Get("code")
	if code == "" || q.Get("state") != "xyz" || q.Get("error") != "" {
		t.Fatalf("approve: redirect query = %v", q)
	}
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "redirect_uri": {oauthRedirect}, "code_verifier": {oauthVerifier}, "resource": {oauthIssuer + "/mcp"}}
	var oerr map[string]string
	for name, tc := range map[string][3]string{
		"wrong verifier":  {"code_verifier", oauthVerifier + "x", "invalid_grant"},
		"wrong redirect":  {"redirect_uri", oauthRedirect + "x", "invalid_grant"},
		"other resource":  {"resource", "https://other.example/mcp", "invalid_target"},
		"password grant":  {"grant_type", "password", "unsupported_grant_type"},
		"unknown client":  {"client_id", "nope", "invalid_client"},
		"no code at all":  {"code", "", "invalid_grant"},
		"made-up code":    {"code", "guess", "invalid_grant"},
		"missing verifer": {"code_verifier", "", "invalid_grant"},
	} {
		form := url.Values{}
		for k, v := range exchange {
			form[k] = v
		}
		form.Set(tc[0], tc[1])
		oerr = nil
		want := http.StatusBadRequest
		if tc[2] == "invalid_client" {
			want = http.StatusUnauthorized
		}
		if got := postForm(t, ts, "/oauth/token", form, &oerr); got != want || oerr["error"] != tc[2] {
			t.Errorf("%s: %d %v", name, got, oerr)
		}
	}
	var tokens auth.OAuthTokens
	if got := postForm(t, ts, "/oauth/token", exchange, &tokens); got != http.StatusOK || tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.TokenType != "Bearer" || tokens.ExpiresIn <= 0 {
		t.Fatalf("exchange: %d %+v", got, tokens)
	}

	// The access token works on /mcp and nowhere else.
	if resp := mcpInitialize(t, ts, tokens.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("/mcp with access token: %d", resp.StatusCode)
	}
	if resp := mcpInitialize(t, ts, tokens.RefreshToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/mcp with refresh token: %d", resp.StatusCode)
	}
	jarless := &client{t: t, http: &http.Client{}, base: ts.URL}
	bearer := []string{"Authorization", "Bearer " + tokens.AccessToken}
	for _, path := range []string{"/api/me", "/api/projects", "/api/me/grants"} {
		if got := jarless.do("GET", path, nil, nil, bearer...); got != http.StatusUnauthorized {
			t.Errorf("access token on %s: %d, want 401", path, got)
		}
	}
	if got := jarless.do("POST", "/api/oauth/authorize", authorizeBody(clientID, true), nil, bearer...); got != http.StatusUnauthorized {
		t.Errorf("access token approving a client: %d, want 401", got)
	}
	// Nor can a personal API token approve a client or manage grants.
	var pat auth.CreatedToken
	browser.must(http.StatusCreated, "POST", "/api/me/tokens", map[string]any{"name": "script"}, &pat)
	patBearer := []string{"Authorization", "Bearer " + pat.Secret}
	if got := jarless.do("POST", "/api/oauth/authorize", authorizeBody(clientID, true), nil, patBearer...); got != http.StatusForbidden {
		t.Errorf("API token approving a client: %d, want 403", got)
	}
	if got := jarless.do("GET", "/api/me/grants", nil, nil, patBearer...); got != http.StatusForbidden {
		t.Errorf("API token listing grants: %d, want 403", got)
	}

	// Refresh rotates; the old refresh token then revokes the grant.
	var refreshed auth.OAuthTokens
	refresh := func(token string, out any) int {
		return postForm(t, ts, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {token}}, out)
	}
	if got := refresh(tokens.RefreshToken, &refreshed); got != http.StatusOK || refreshed.AccessToken == tokens.AccessToken || refreshed.RefreshToken == tokens.RefreshToken {
		t.Fatalf("refresh: %d %+v", got, refreshed)
	}
	if resp := mcpInitialize(t, ts, refreshed.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("/mcp with refreshed token: %d", resp.StatusCode)
	}

	// Connected apps: listed for the owner only, and Disconnect is immediate.
	var grants []auth.OAuthGrant
	browser.must(http.StatusOK, "GET", "/api/me/grants", nil, &grants)
	if len(grants) != 1 || grants[0].ClientName != "Claude" || grants[0].LastUsedAt == nil {
		t.Fatalf("grants = %+v", grants)
	}
	kim := newClient(t, ts)
	kim.register("kim@example.com")
	var kimGrants []auth.OAuthGrant
	kim.must(http.StatusOK, "GET", "/api/me/grants", nil, &kimGrants)
	if len(kimGrants) != 0 {
		t.Fatalf("kim sees sam's grants: %+v", kimGrants)
	}
	kim.must(http.StatusNotFound, "DELETE", "/api/me/grants/1", nil, nil)
	browser.must(http.StatusNoContent, "DELETE", "/api/me/grants/1", nil, nil)
	if resp := mcpInitialize(t, ts, refreshed.AccessToken); resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("/mcp after disconnect: %d", resp.StatusCode)
	}
	oerr = nil
	if got := refresh(refreshed.RefreshToken, &oerr); got != http.StatusBadRequest || oerr["error"] != "invalid_grant" {
		t.Fatalf("refresh after disconnect: %d %v", got, oerr)
	}

	// Reconnecting works, and the client can revoke its own tokens.
	exchange.Set("code", approve(t, browser, authorizeBody(clientID, true)).Get("code"))
	if got := postForm(t, ts, "/oauth/token", exchange, &tokens); got != http.StatusOK {
		t.Fatalf("second exchange: %d", got)
	}
	if got := postForm(t, ts, "/oauth/revoke", url.Values{"token": {"unknown"}}, nil); got != http.StatusOK {
		t.Fatalf("revoke unknown token: %d", got)
	}
	if got := postForm(t, ts, "/oauth/revoke", url.Values{"token": {tokens.RefreshToken}}, nil); got != http.StatusOK {
		t.Fatalf("revoke: %d", got)
	}
	if resp := mcpInitialize(t, ts, tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/mcp after client revocation: %d", resp.StatusCode)
	}

	// An administrator deactivating the account disconnects its apps for
	// good: they do not come back when the account is reactivated.
	exchange.Set("code", approve(t, kim, authorizeBody(clientID, true)).Get("code"))
	if got := postForm(t, ts, "/oauth/token", exchange, &tokens); got != http.StatusOK {
		t.Fatalf("kim's exchange: %d", got)
	}
	browser.must(http.StatusOK, "PATCH", "/api/users/2", map[string]any{"is_active": false}, nil)
	browser.must(http.StatusOK, "PATCH", "/api/users/2", map[string]any{"is_active": true}, nil)
	if resp := mcpInitialize(t, ts, tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/mcp after deactivation: %d", resp.StatusCode)
	}
}
