package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database/dbgen"
)

const (
	testRedirect = "https://claude.example/callback"
	testVerifier = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
)

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func oauthCode(err error) string {
	var oe *OAuthError
	if errors.As(err, &oe) {
		return oe.Code
	}
	return ""
}

// connect registers a client and takes a user through approval to tokens.
func connect(t *testing.T, svc *Service, userID int64) (OAuthClient, OAuthTokens) {
	t.Helper()
	ctx := context.Background()
	c, err := svc.RegisterClient(ctx, "Claude", []string{testRedirect})
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.IssueCode(ctx, userID, c, testRedirect, challengeOf(testVerifier))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, testVerifier)
	if err != nil {
		t.Fatal(err)
	}
	return c, tokens
}

func TestOAuthClientRegistration(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, true)

	c, err := svc.RegisterClient(ctx, "  Claude \n Code ", []string{testRedirect, "http://localhost:1234/cb", "http://127.0.0.1/cb", "http://[::1]:9/cb"})
	if err != nil || c.ClientID == "" || c.Name != "Claude Code" || len(c.RedirectURIs) != 4 {
		t.Fatalf("register = %+v, %v", c, err)
	}
	if long, err := svc.RegisterClient(ctx, strings.Repeat("é", 300), []string{testRedirect}); err != nil || len([]rune(long.Name)) != maxClientNameLength {
		t.Fatalf("long name = %q, %v", long.Name, err)
	}
	if unnamed, err := svc.RegisterClient(ctx, "", []string{testRedirect}); err != nil || unnamed.Name == "" {
		t.Fatalf("unnamed = %+v, %v", unnamed, err)
	}

	for _, uris := range [][]string{
		nil,
		{"http://example.com/cb"},          // plain http off loopback
		{"https://example.com/cb#frag"},    // fragment
		{"https://example.com/cb#"},        // empty fragment
		{"javascript:alert(1)"},            // no host
		{"myapp://callback"},               // custom scheme
		{"https://user:pw@example.com/cb"}, // credentials
		{"/relative"},
		{testRedirect, "ftp://example.com/x"}, // one bad URI spoils the set
		make([]string, maxRedirectURIs+1),
	} {
		if _, err := svc.RegisterClient(ctx, "x", uris); oauthCode(err) != "invalid_redirect_uri" {
			t.Errorf("redirect_uris %q: err = %v", uris, err)
		}
	}

	// The redirect URI must be registered; a loopback one may change port.
	if _, err := svc.ClientForRedirect(ctx, c.ClientID, testRedirect); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClientForRedirect(ctx, c.ClientID, "http://localhost:54321/cb"); err != nil {
		t.Fatalf("loopback on another port: %v", err)
	}
	for _, uri := range []string{"", testRedirect + "/", testRedirect + "?x=1", "https://evil.example/callback", "http://localhost:1234/other", "http://evil.example:1234/cb"} {
		if _, err := svc.ClientForRedirect(ctx, c.ClientID, uri); oauthCode(err) != "invalid_request" {
			t.Errorf("redirect %q: err = %v", uri, err)
		}
	}
	if _, err := svc.ClientForRedirect(ctx, "nope", testRedirect); oauthCode(err) != "invalid_client" {
		t.Fatalf("unknown client: err = %v", err)
	}
}

func TestOAuthIdleClientsArePruned(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	u, _ := svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"})
	connected, _ := connect(t, svc, u.ID)
	abandoned, _ := svc.RegisterClient(ctx, "abandoned", []string{testRedirect})

	*now = now.Add(idleClientTTL + time.Hour)
	if _, err := svc.RegisterClient(ctx, "new", []string{testRedirect}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClientForRedirect(ctx, abandoned.ClientID, testRedirect); oauthCode(err) != "invalid_client" {
		t.Fatalf("abandoned client survived: %v", err)
	}
	if _, err := svc.ClientForRedirect(ctx, connected.ClientID, testRedirect); err != nil {
		t.Fatalf("client with a grant was pruned: %v", err)
	}
}

func TestOAuthCodeExchange(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	u, _ := svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"})
	c, _ := svc.RegisterClient(ctx, "Claude", []string{testRedirect})
	other, _ := svc.RegisterClient(ctx, "Other", []string{testRedirect})
	issue := func() string {
		t.Helper()
		code, err := svc.IssueCode(ctx, u.ID, c, testRedirect, challengeOf(testVerifier))
		if err != nil {
			t.Fatal(err)
		}
		return code
	}

	// Each of these fails without consuming the code.
	code := issue()
	for name, try := range map[string]func() (OAuthTokens, error){
		"wrong verifier": func() (OAuthTokens, error) {
			return svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, testVerifier+"x")
		},
		"short verifier": func() (OAuthTokens, error) { return svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, "short") },
		"no verifier":    func() (OAuthTokens, error) { return svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, "") },
		"wrong redirect": func() (OAuthTokens, error) {
			return svc.ExchangeCode(ctx, c.ClientID, code, testRedirect+"x", testVerifier)
		},
		"another client": func() (OAuthTokens, error) {
			return svc.ExchangeCode(ctx, other.ClientID, code, testRedirect, testVerifier)
		},
		"unknown code": func() (OAuthTokens, error) {
			return svc.ExchangeCode(ctx, c.ClientID, code+"x", testRedirect, testVerifier)
		},
		"the empty string": func() (OAuthTokens, error) { return svc.ExchangeCode(ctx, c.ClientID, "", testRedirect, testVerifier) },
	} {
		if _, err := try(); oauthCode(err) != "invalid_grant" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := svc.ExchangeCode(ctx, "nope", code, testRedirect, testVerifier); oauthCode(err) != "invalid_client" {
		t.Fatalf("unknown client: err = %v", err)
	}

	tokens, err := svc.ExchangeCode(ctx, c.ClientID, code, "", testVerifier) // redirect_uri is optional
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tokens.AccessToken, AccessTokenPrefix) || !strings.HasPrefix(tokens.RefreshToken, RefreshTokenPrefix) ||
		tokens.TokenType != "Bearer" || tokens.ExpiresIn != int64(AccessTokenTTL/time.Second) {
		t.Fatalf("tokens = %+v", tokens)
	}
	got, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken)
	if err != nil || got.ID != u.ID {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}
	// An access token is not an API token, a session or a refresh token.
	if _, err := svc.AuthenticateToken(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("access token as API token: %v", err)
	}
	if _, err := svc.Authenticate(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("access token as session: %v", err)
	}
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.RefreshToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("refresh token as access token: %v", err)
	}

	// Presenting the code again fails and revokes what its first use issued.
	if _, err := svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, testVerifier); oauthCode(err) != "invalid_grant" {
		t.Fatalf("reused code: err = %v", err)
	}
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("access token survived code reuse: %v", err)
	}
	if _, err := svc.Refresh(ctx, c.ClientID, tokens.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("refresh token survived code reuse: %v", err)
	}

	// A code is short-lived.
	code = issue()
	*now = now.Add(codeTTL)
	if _, err := svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, testVerifier); oauthCode(err) != "invalid_grant" {
		t.Fatalf("expired code: err = %v", err)
	}

	// The access token expires on its own.
	_, tokens = connect(t, svc, u.ID)
	*now = now.Add(AccessTokenTTL)
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("expired access token: %v", err)
	}
}

func TestOAuthRefresh(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	u, _ := svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"})
	c, first := connect(t, svc, u.ID)
	other, _ := svc.RegisterClient(ctx, "Other", []string{testRedirect})

	// A refresh token is bound to its client, and an access token is not one.
	if _, err := svc.Refresh(ctx, other.ClientID, first.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("another client's refresh: err = %v", err)
	}
	if _, err := svc.Refresh(ctx, c.ClientID, first.AccessToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("access token as refresh token: err = %v", err)
	}
	if _, err := svc.Refresh(ctx, "nope", first.RefreshToken); oauthCode(err) != "invalid_client" {
		t.Fatalf("unknown client: err = %v", err)
	}

	// Rotation: a new pair, and it keeps working well past the first
	// refresh token's own lifetime as long as it is used.
	tokens := first
	for range 3 {
		*now = now.Add(RefreshTokenTTL - time.Hour)
		next, err := svc.Refresh(ctx, c.ClientID, tokens.RefreshToken)
		if err != nil {
			t.Fatal(err)
		}
		if next.AccessToken == tokens.AccessToken || next.RefreshToken == tokens.RefreshToken {
			t.Fatalf("tokens were not rotated: %+v", next)
		}
		if _, err := svc.AuthenticateAccessToken(ctx, next.AccessToken); err != nil {
			t.Fatal(err)
		}
		tokens = next
	}
	if grants, _ := svc.Grants(ctx, u.ID); len(grants) != 1 {
		t.Fatalf("refreshing changed the grants: %+v", grants)
	}

	// Reuse of a rotated token revokes the grant: the current pair dies too.
	*now = now.Add(time.Minute)
	previous := tokens
	tokens, err := svc.Refresh(ctx, c.ClientID, previous.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(ctx, c.ClientID, previous.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("reused refresh token: err = %v", err)
	}
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("access token survived refresh reuse: %v", err)
	}
	if _, err := svc.Refresh(ctx, c.ClientID, tokens.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("refresh token survived refresh reuse: %v", err)
	}
	if grants, _ := svc.Grants(ctx, u.ID); len(grants) != 0 {
		t.Fatalf("grant survived refresh reuse: %+v", grants)
	}

	// Left unused for RefreshTokenTTL, the connection ends and the grant is
	// cleaned up by the next exchange.
	c, tokens = connect(t, svc, u.ID)
	*now = now.Add(RefreshTokenTTL)
	if _, err := svc.Refresh(ctx, c.ClientID, tokens.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("idle refresh token: err = %v", err)
	}
	connect(t, svc, u.ID)
	if grants, _ := svc.Grants(ctx, u.ID); len(grants) != 1 {
		t.Fatalf("expired grant was not pruned: %+v", grants)
	}
}

func TestOAuthGrantsFollowTheAccount(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	sam, _ := svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"})
	kim, _ := svc.Register(ctx, RegisterInput{Email: "kim@example.com", Password: "correct horse"})
	c, tokens := connect(t, svc, sam.ID)

	// Listing, with last_used_at recorded on use.
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	grants, err := svc.Grants(ctx, sam.ID)
	if err != nil || len(grants) != 1 || grants[0].ClientName != "Claude" || grants[0].LastUsedAt == nil || !grants[0].LastUsedAt.Equal(*now) {
		t.Fatalf("grants = %+v, %v", grants, err)
	}
	if kims, _ := svc.Grants(ctx, kim.ID); len(kims) != 0 {
		t.Fatalf("kim sees sam's grants: %+v", kims)
	}

	// Only the owner can disconnect, and it takes effect at once.
	if err := svc.RevokeGrant(ctx, kim.ID, grants[0].ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("kim revoking sam's grant: %v", err)
	}
	if err := svc.RevokeGrant(ctx, sam.ID, grants[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("access token after disconnect: %v", err)
	}
	if _, err := svc.Refresh(ctx, c.ClientID, tokens.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("refresh after disconnect: %v", err)
	}

	// The client can give its tokens up itself, with either one.
	for _, pick := range []func(OAuthTokens) string{
		func(t OAuthTokens) string { return t.AccessToken },
		func(t OAuthTokens) string { return t.RefreshToken },
	} {
		_, tokens = connect(t, svc, sam.ID)
		if err := svc.RevokeOAuthToken(ctx, pick(tokens)); err != nil {
			t.Fatal(err)
		}
		if grants, _ := svc.Grants(ctx, sam.ID); len(grants) != 0 {
			t.Fatalf("grant survived client revocation: %+v", grants)
		}
	}
	if err := svc.RevokeOAuthToken(ctx, "unknown"); err != nil {
		t.Fatalf("revoking an unknown token: %v", err)
	}

	// A password reset disconnects every app.
	connect(t, svc, sam.ID)
	if err := svc.SetPassword(ctx, "sam@example.com", "a new password"); err != nil {
		t.Fatal(err)
	}
	if grants, _ := svc.Grants(ctx, sam.ID); len(grants) != 0 {
		t.Fatalf("grants survived a password reset: %+v", grants)
	}

	// A deactivated user's tokens stop working and cannot be refreshed, and
	// a code issued just before cannot be redeemed.
	c, tokens = connect(t, svc, sam.ID)
	code, _ := svc.IssueCode(ctx, sam.ID, c, testRedirect, challengeOf(testVerifier))
	row, _ := svc.store.GetUser(ctx, sam.ID)
	if _, err := svc.store.UpdateUser(ctx, dbgen.UpdateUserParams{ID: sam.ID, DisplayName: row.DisplayName, IsAdmin: row.IsAdmin, IsActive: false, Now: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAccessToken(ctx, tokens.AccessToken); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("deactivated user's access token: %v", err)
	}
	if _, err := svc.Refresh(ctx, c.ClientID, tokens.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("deactivated user's refresh: %v", err)
	}
	if _, err := svc.ExchangeCode(ctx, c.ClientID, code, testRedirect, testVerifier); oauthCode(err) != "invalid_grant" {
		t.Fatalf("deactivated user's code: %v", err)
	}
}
