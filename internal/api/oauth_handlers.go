package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/auth"
	"trackstar/internal/user"
)

// OAuth 2.1 for MCP clients, with Trackstar as its own authorization server.
// A client that is given only the /mcp URL gets a 401 pointing at the
// protected-resource metadata, registers itself, sends the user's browser to
// /oauth/authorize (the single-page app's consent screen, backed by
// /api/oauth/authorize) and exchanges the resulting code at /oauth/token.
//
// Every advertised URL is built from TRACKSTAR_PUBLIC_URL, never from the
// Host header, so it is right behind a proxy or tunnel.

// issuer is the public URL without its trailing slash.
func (s *Server) issuer() string { return strings.TrimRight(s.PublicURL.String(), "/") }

// mcpResource is the one resource tokens are issued for (RFC 8707).
func (s *Server) mcpResource() string { return s.issuer() + "/mcp" }

// validResource accepts an absent resource parameter or the MCP endpoint.
func (s *Server) validResource(resource string) bool {
	return resource == "" || strings.EqualFold(strings.TrimRight(resource, "/"), s.mcpResource())
}

// crossOriginPaths are called by MCP clients, some of them running in a
// browser on another origin. They read no cookie and set none, so there is
// nothing for a cross-site request to ride on.
var crossOriginPaths = map[string]bool{
	"/oauth/register": true,
	"/oauth/token":    true,
	"/oauth/revoke":   true,
}

func allowAnyOrigin(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func handlePreflight(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

// writeMetadata serves a discovery document: public and cacheable.
func writeMetadata(w http.ResponseWriter, v any) {
	allowAnyOrigin(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(v)
}

// handleProtectedResourceMetadata is RFC 9728: which authorization server
// guards /mcp.
func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeMetadata(w, map[string]any{
		"resource":                 s.mcpResource(),
		"authorization_servers":    []string{s.issuer()},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Trackstar",
	})
}

// handleAuthorizationServerMetadata is RFC 8414.
func (s *Server) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeMetadata(w, map[string]any{
		"issuer":                                s.issuer(),
		"authorization_endpoint":                s.issuer() + "/oauth/authorize",
		"token_endpoint":                        s.issuer() + "/oauth/token",
		"registration_endpoint":                 s.issuer() + "/oauth/register",
		"revocation_endpoint":                   s.issuer() + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// requireMCPUser authenticates /mcp. It accepts what the API accepts (a
// personal API token or a browser session) plus OAuth access tokens, which
// are valid here and nowhere else. A 401 carries the WWW-Authenticate
// challenge that starts an MCP client's OAuth flow.
func (s *Server) requireMCPUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var (
			u   user.User
			err error
		)
		tok := bearerToken(r)
		switch {
		case strings.HasPrefix(tok, auth.AccessTokenPrefix):
			u, err = s.Auth.AuthenticateAccessToken(ctx, tok)
		case tok != "":
			u, err = s.Auth.AuthenticateToken(ctx, tok)
		default:
			u, err = s.Auth.Authenticate(ctx, sessionToken(r))
		}
		if err != nil {
			if apperr.KindOf(err) == apperr.KindUnauthorized {
				challenge := `Bearer resource_metadata="` + s.issuer() + `/.well-known/oauth-protected-resource"`
				if tok != "" {
					challenge += `, error="invalid_token"`
				}
				w.Header().Set("WWW-Authenticate", challenge)
			}
			s.fail(w, r, err)
			return
		}
		ctx = context.WithValue(ctx, ctxBearer, tok != "")
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxUser, u)))
	})
}

// writeOAuthError reports err in the RFC 6749 shape. Anything that is not an
// OAuth error is logged and hidden.
func (s *Server) writeOAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var oe *auth.OAuthError
	if !errors.As(err, &oe) {
		s.Logger.Error("internal error", "error", err, "request_id", requestID(r.Context()), "path", r.URL.Path)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	status := http.StatusBadRequest
	if oe.Code == "invalid_client" {
		status = http.StatusUnauthorized
	}
	writeJSON(w, status, map[string]string{"error": oe.Code, "error_description": oe.Description})
}

// handleRegisterClient is RFC 7591 dynamic client registration. It is open
// to anyone, so it is rate limited; a registration grants nothing until a
// user approves the client.
func (s *Server) handleRegisterClient(w http.ResponseWriter, r *http.Request) {
	allowAnyOrigin(w)
	if !s.registerLimiter.Allow(s.clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_requests", "error_description": "too many registrations, try again in a few minutes"})
		return
	}
	// Not decode(): clients send metadata we have no use for (logo_uri,
	// contacts, scope, ...), and unknown fields must be ignored.
	var in struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata", "error_description": "invalid JSON body"})
		return
	}
	c, err := s.Auth.RegisterClient(r.Context(), in.ClientName, in.RedirectURIs)
	if err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ClientID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// authorizeParams is an authorization request as the consent screen relays
// it from the query string of /oauth/authorize.
type authorizeParams struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	ResponseType        string `json:"response_type"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Resource            string `json:"resource"`
	Scope               string `json:"scope"`
	Approve             bool   `json:"approve"`
}

// redirectWith appends params (and state, when the client sent one) to the
// client's redirect URI.
func redirectWith(redirectURI, state string, params ...string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	for i := 0; i+1 < len(params); i += 2 {
		q.Set(params[i], params[i+1])
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// checkAuthorize validates an authorization request. An unknown client or
// an unregistered redirect URI is an error for the user to read; every
// other problem is reported to the client by redirect, returned as
// rejection.
func (s *Server) checkAuthorize(r *http.Request, p authorizeParams) (c auth.OAuthClient, rejection string, err error) {
	c, err = s.Auth.ClientForRedirect(r.Context(), p.ClientID, p.RedirectURI)
	if err != nil {
		var oe *auth.OAuthError
		if errors.As(err, &oe) {
			err = apperr.Invalid("This authorization request is not valid: %s. Nothing was shared; start again from the app you were connecting.", oe.Description)
		}
		return c, "", err
	}
	reject := func(code, description string) (auth.OAuthClient, string, error) {
		return c, redirectWith(p.RedirectURI, p.State, "error", code, "error_description", description), nil
	}
	switch {
	case p.ResponseType != "code":
		return reject("unsupported_response_type", "response_type must be code")
	case p.CodeChallenge == "":
		return reject("invalid_request", "code_challenge is required (PKCE)")
	case p.CodeChallengeMethod != "S256":
		return reject("invalid_request", "code_challenge_method must be S256")
	case !auth.ValidCodeChallenge(p.CodeChallenge):
		return reject("invalid_request", "code_challenge is not a valid S256 challenge")
	case !s.validResource(p.Resource):
		return reject("invalid_target", "resource must be "+s.mcpResource())
	}
	return c, "", nil
}

// handleAuthorizeInfo tells the consent screen who is asking, or where to
// send the browser when the request is one the client must be told is bad.
func (s *Server) handleAuthorizeInfo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := authorizeParams{
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		ResponseType:        q.Get("response_type"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
		State:               q.Get("state"),
		Resource:            q.Get("resource"),
	}
	c, rejection, err := s.checkAuthorize(r, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if rejection != "" {
		writeJSON(w, http.StatusOK, map[string]string{"redirect_to": rejection})
		return
	}
	host := p.RedirectURI
	if u, err := url.Parse(p.RedirectURI); err == nil {
		host = u.Host
	}
	writeJSON(w, http.StatusOK, map[string]string{"client_name": c.Name, "redirect_host": host})
}

// handleAuthorizeDecision records the user's answer and returns where the
// browser goes next: the client's redirect URI with a code, or with
// access_denied. It needs the browser session and, being a POST, passes
// checkOrigin, so no other site can approve on the user's behalf.
func (s *Server) handleAuthorizeDecision(w http.ResponseWriter, r *http.Request) {
	var p authorizeParams
	if err := decode(w, r, &p); err != nil {
		s.fail(w, r, err)
		return
	}
	c, rejection, err := s.checkAuthorize(r, p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if rejection == "" && !p.Approve {
		rejection = redirectWith(p.RedirectURI, p.State, "error", "access_denied", "error_description", "the user denied the request")
	}
	if rejection != "" {
		writeJSON(w, http.StatusOK, map[string]string{"redirect_to": rejection})
		return
	}
	code, err := s.Auth.IssueCode(r.Context(), currentUser(r.Context()).ID, c, p.RedirectURI, p.CodeChallenge)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect_to": redirectWith(p.RedirectURI, p.State, "code", code)})
}

// parseOAuthForm reads an application/x-www-form-urlencoded body.
func parseOAuthForm(w http.ResponseWriter, r *http.Request) bool {
	allowAnyOrigin(w)
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "the body must be application/x-www-form-urlencoded"})
		return false
	}
	return true
}

// handleToken is the token endpoint: authorization_code and refresh_token
// grants for public clients.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if !parseOAuthForm(w, r) {
		return
	}
	form := r.PostForm
	if !s.validResource(form.Get("resource")) {
		s.writeOAuthError(w, r, &auth.OAuthError{Code: "invalid_target", Description: "resource must be " + s.mcpResource()})
		return
	}
	var (
		tokens auth.OAuthTokens
		err    error
	)
	switch form.Get("grant_type") {
	case "authorization_code":
		tokens, err = s.Auth.ExchangeCode(r.Context(), form.Get("client_id"), form.Get("code"), form.Get("redirect_uri"), form.Get("code_verifier"))
	case "refresh_token":
		tokens, err = s.Auth.Refresh(r.Context(), form.Get("client_id"), form.Get("refresh_token"))
	default:
		err = &auth.OAuthError{Code: "unsupported_grant_type", Description: "grant_type must be authorization_code or refresh_token"}
	}
	if err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, tokens)
}

// handleRevoke is RFC 7009: a client gives up its tokens. It answers 200
// whether or not the token existed.
func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if !parseOAuthForm(w, r) {
		return
	}
	if err := s.Auth.RevokeOAuthToken(r.Context(), r.PostForm.Get("token")); err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// Connected apps. Like API tokens, these routes need a browser session.

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.Auth.Grants(r.Context(), currentUser(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.RevokeGrant(r.Context(), currentUser(r.Context()).ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Self-registration is bounded per client address.
const (
	registerLimit  = 20
	registerWindow = 10 * time.Minute
)
