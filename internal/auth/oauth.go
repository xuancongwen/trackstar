package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
	"trackstar/internal/user"
)

// OAuth 2.1 authorization server for MCP clients. Clients are public and
// register themselves; the user approves one in the browser, and the client
// then holds an access token for /mcp and a rotating refresh token. A grant
// carries its user's full project access, like a personal API token.

const (
	// The prefixes keep OAuth tokens apart from personal API tokens and
	// session cookies in logs and secret scanners.
	AccessTokenPrefix  = "tsa_"
	RefreshTokenPrefix = "tsr_"

	AccessTokenTTL = time.Hour
	// RefreshTokenTTL is an idle limit: every refresh issues a new token
	// with a fresh lifetime, so only a connection left unused this long ends.
	RefreshTokenTTL = 60 * 24 * time.Hour

	codeTTL = time.Minute
	// idleClientTTL is how long a registration nobody is connected through
	// is kept.
	idleClientTTL = 30 * 24 * time.Hour

	maxClientNameLength = 100
	maxRedirectURIs     = 10
	maxRedirectURILen   = 2000

	kindAccess  = "access"
	kindRefresh = "refresh"
)

// OAuthError is an error the OAuth endpoints report to the client with its
// RFC 6749 / RFC 7591 code.
type OAuthError struct {
	Code        string
	Description string
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, description string) error {
	return &OAuthError{Code: code, Description: description}
}

var errInvalidGrant = oauthErr("invalid_grant", "the grant is invalid, expired or revoked")

// OAuthClient is a registered MCP client. The name is whatever the client
// claimed about itself; the redirect URIs are what it can actually receive
// a code on.
type OAuthClient struct {
	ClientID     string    `json:"client_id"`
	Name         string    `json:"client_name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"-"`

	rowID int64
}

// OAuthTokens is the token endpoint's success response.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// OAuthGrant is a connected app as shown to its user.
type OAuthGrant struct {
	ID         int64      `json:"id"`
	ClientName string     `json:"client_name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// RegisterClient stores a self-registered client and gives it a client_id.
func (s *Service) RegisterClient(ctx context.Context, name string, redirectURIs []string) (OAuthClient, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = "Unnamed client"
	}
	if r := []rune(name); len(r) > maxClientNameLength {
		name = string(r[:maxClientNameLength])
	}
	if len(redirectURIs) == 0 {
		return OAuthClient{}, oauthErr("invalid_redirect_uri", "at least one redirect_uri is required")
	}
	if len(redirectURIs) > maxRedirectURIs {
		return OAuthClient{}, oauthErr("invalid_redirect_uri", "too many redirect_uris")
	}
	for _, uri := range redirectURIs {
		if !validRedirectURI(uri) {
			return OAuthClient{}, oauthErr("invalid_redirect_uri", "redirect_uris must be https, or http on a loopback address, without a fragment")
		}
	}
	encoded, err := json.Marshal(redirectURIs)
	if err != nil {
		return OAuthClient{}, err
	}
	clientID, err := randomToken("")
	if err != nil {
		return OAuthClient{}, err
	}
	now := s.now()
	// Opportunistic cleanup instead of a background worker.
	if err := s.store.DeleteIdleOAuthClients(ctx, sql.NullInt64{Int64: now.Add(-idleClientTTL).Unix(), Valid: true}); err != nil {
		return OAuthClient{}, err
	}
	row, err := s.store.CreateOAuthClient(ctx, dbgen.CreateOAuthClientParams{
		ClientID: clientID, Name: name, RedirectUris: string(encoded), CreatedAt: now.Unix(),
	})
	if err != nil {
		return OAuthClient{}, err
	}
	return clientFromRow(row)
}

// ClientForRedirect resolves client_id and checks that redirectURI is one
// the client registered. Both failures must be shown to the user and never
// redirected to: the redirect target is exactly what cannot be trusted.
func (s *Service) ClientForRedirect(ctx context.Context, clientID, redirectURI string) (OAuthClient, error) {
	c, err := s.client(ctx, clientID)
	if err != nil {
		return OAuthClient{}, err
	}
	for _, registered := range c.RedirectURIs {
		if redirectMatches(registered, redirectURI) {
			return c, nil
		}
	}
	return OAuthClient{}, oauthErr("invalid_request", "redirect_uri is not registered for this client")
}

func (s *Service) client(ctx context.Context, clientID string) (OAuthClient, error) {
	row, err := s.store.GetOAuthClient(ctx, clientID)
	if database.IsNotFound(err) {
		return OAuthClient{}, oauthErr("invalid_client", "unknown client")
	}
	if err != nil {
		return OAuthClient{}, err
	}
	return clientFromRow(row)
}

// IssueCode records the user's approval as a single-use authorization code
// bound to the client, the redirect URI and the PKCE challenge.
func (s *Service) IssueCode(ctx context.Context, userID int64, c OAuthClient, redirectURI, codeChallenge string) (string, error) {
	code, err := randomToken("")
	if err != nil {
		return "", err
	}
	now := s.now()
	if err := s.store.DeleteExpiredOAuthCodes(ctx, now.Unix()); err != nil {
		return "", err
	}
	err = s.store.CreateOAuthCode(ctx, dbgen.CreateOAuthCodeParams{
		CodeHash:      s.hashToken(code),
		UserID:        userID,
		ClientID:      c.rowID,
		RedirectUri:   redirectURI,
		CodeChallenge: codeChallenge,
		ExpiresAt:     now.Add(codeTTL).Unix(),
	})
	return code, err
}

// ExchangeCode redeems an authorization code for tokens. A code works once:
// presenting it again revokes the grant its first use produced, since one of
// the two presenters stole it.
func (s *Service) ExchangeCode(ctx context.Context, clientID, code, redirectURI, codeVerifier string) (OAuthTokens, error) {
	c, err := s.client(ctx, clientID)
	if err != nil {
		return OAuthTokens{}, err
	}
	var (
		tokens OAuthTokens
		reused bool
	)
	now := s.now()
	err = s.store.InTx(ctx, func(q dbgen.Querier) error {
		row, err := q.GetOAuthCode(ctx, s.hashToken(code))
		if database.IsNotFound(err) {
			return errInvalidGrant
		}
		if err != nil {
			return err
		}
		if row.UsedAt.Valid {
			reused = true
			if row.GrantID.Valid {
				return q.DeleteOAuthGrantByID(ctx, row.GrantID.Int64)
			}
			return nil
		}
		if row.ExpiresAt <= now.Unix() || row.ClientID != c.rowID {
			return errInvalidGrant
		}
		// OAuth 2.1 no longer requires redirect_uri here (PKCE covers it),
		// but a client that sends one must send the one it used.
		if redirectURI != "" && redirectURI != row.RedirectUri {
			return errInvalidGrant
		}
		if !verifyPKCE(row.CodeChallenge, codeVerifier) {
			return errInvalidGrant
		}
		u, err := q.GetUser(ctx, row.UserID)
		if err != nil || !u.IsActive {
			return errInvalidGrant
		}
		if err := pruneOAuth(ctx, q, now); err != nil {
			return err
		}
		grant, err := q.CreateOAuthGrant(ctx, dbgen.CreateOAuthGrantParams{UserID: row.UserID, ClientID: c.rowID, CreatedAt: now.Unix()})
		if err != nil {
			return err
		}
		n, err := q.UseOAuthCode(ctx, dbgen.UseOAuthCodeParams{ID: row.ID, Now: nullTime(now), GrantID: sql.NullInt64{Int64: grant.ID, Valid: true}})
		if err != nil {
			return err
		}
		if n != 1 {
			return errInvalidGrant
		}
		if err := q.TouchOAuthClient(ctx, dbgen.TouchOAuthClientParams{ID: c.rowID, Now: nullTime(now)}); err != nil {
			return err
		}
		tokens, err = s.issueTokens(ctx, q, grant.ID, now)
		return err
	})
	if err != nil {
		return OAuthTokens{}, err
	}
	if reused {
		return OAuthTokens{}, errInvalidGrant
	}
	return tokens, nil
}

// Refresh rotates a refresh token: the old one stops working and a new pair
// is returned. Presenting an already-rotated token means it leaked, so the
// whole grant is revoked and both holders are cut off.
func (s *Service) Refresh(ctx context.Context, clientID, refreshToken string) (OAuthTokens, error) {
	c, err := s.client(ctx, clientID)
	if err != nil {
		return OAuthTokens{}, err
	}
	if !strings.HasPrefix(refreshToken, RefreshTokenPrefix) {
		return OAuthTokens{}, errInvalidGrant
	}
	var (
		tokens OAuthTokens
		reused bool
	)
	now := s.now()
	err = s.store.InTx(ctx, func(q dbgen.Querier) error {
		row, err := q.GetOAuthToken(ctx, s.hashToken(refreshToken))
		if database.IsNotFound(err) {
			return errInvalidGrant
		}
		if err != nil {
			return err
		}
		if row.Kind != kindRefresh || row.GrantClientID != c.rowID {
			return errInvalidGrant
		}
		if row.UsedAt.Valid {
			reused = true
			return q.DeleteOAuthGrantByID(ctx, row.GrantID)
		}
		if row.ExpiresAt <= now.Unix() {
			return errInvalidGrant
		}
		u, err := q.GetOAuthGrantUser(ctx, row.GrantID)
		if err != nil || !u.IsActive {
			return errInvalidGrant
		}
		n, err := q.UseOAuthToken(ctx, dbgen.UseOAuthTokenParams{ID: row.ID, Now: nullTime(now)})
		if err != nil {
			return err
		}
		if n != 1 {
			return errInvalidGrant
		}
		if err := q.TouchOAuthClient(ctx, dbgen.TouchOAuthClientParams{ID: c.rowID, Now: nullTime(now)}); err != nil {
			return err
		}
		if tokens, err = s.issueTokens(ctx, q, row.GrantID, now); err != nil {
			return err
		}
		return pruneOAuth(ctx, q, now)
	})
	if err != nil {
		return OAuthTokens{}, err
	}
	if reused {
		return OAuthTokens{}, errInvalidGrant
	}
	return tokens, nil
}

func (s *Service) issueTokens(ctx context.Context, q dbgen.Querier, grantID int64, now time.Time) (OAuthTokens, error) {
	access, err := randomToken(AccessTokenPrefix)
	if err != nil {
		return OAuthTokens{}, err
	}
	refresh, err := randomToken(RefreshTokenPrefix)
	if err != nil {
		return OAuthTokens{}, err
	}
	for _, t := range []struct {
		kind, secret string
		ttl          time.Duration
	}{{kindAccess, access, AccessTokenTTL}, {kindRefresh, refresh, RefreshTokenTTL}} {
		err := q.CreateOAuthToken(ctx, dbgen.CreateOAuthTokenParams{
			GrantID: grantID, Kind: t.kind, TokenHash: s.hashToken(t.secret), ExpiresAt: now.Add(t.ttl).Unix(),
		})
		if err != nil {
			return OAuthTokens{}, err
		}
	}
	return OAuthTokens{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(AccessTokenTTL / time.Second), RefreshToken: refresh}, nil
}

// pruneOAuth drops expired codes and tokens, then grants left with no token
// at all (their refresh token went unused for RefreshTokenTTL).
func pruneOAuth(ctx context.Context, q dbgen.Querier, now time.Time) error {
	if err := q.DeleteExpiredOAuthCodes(ctx, now.Unix()); err != nil {
		return err
	}
	if err := q.DeleteExpiredOAuthTokens(ctx, now.Unix()); err != nil {
		return err
	}
	return q.DeleteEmptyOAuthGrants(ctx)
}

// AuthenticateAccessToken resolves an OAuth access token to its user.
// Expired tokens, revoked grants and deactivated users fail like unknown
// tokens.
func (s *Service) AuthenticateAccessToken(ctx context.Context, secret string) (user.User, error) {
	if !strings.HasPrefix(secret, AccessTokenPrefix) {
		return user.User{}, apperr.Unauthorized("invalid access token")
	}
	now := s.now()
	row, err := s.store.GetOAuthAccessTokenUser(ctx, dbgen.GetOAuthAccessTokenUserParams{TokenHash: s.hashToken(secret), Now: now.Unix()})
	if database.IsNotFound(err) {
		return user.User{}, apperr.Unauthorized("invalid access token")
	}
	if err != nil {
		return user.User{}, err
	}
	if !row.LastUsedAt.Valid || now.Unix()-row.LastUsedAt.Int64 >= int64(touchInterval/time.Second) {
		if err := s.store.TouchOAuthGrant(ctx, dbgen.TouchOAuthGrantParams{ID: row.GrantID, Now: nullTime(now)}); err != nil {
			return user.User{}, err
		}
	}
	return user.FromRow(row.User), nil
}

// Grants lists the apps userID has connected, newest first.
func (s *Service) Grants(ctx context.Context, userID int64) ([]OAuthGrant, error) {
	rows, err := s.store.ListOAuthGrants(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]OAuthGrant, len(rows))
	for i, r := range rows {
		out[i] = OAuthGrant{ID: r.ID, ClientName: r.ClientName, CreatedAt: time.Unix(r.CreatedAt, 0).UTC()}
		if r.LastUsedAt.Valid {
			at := time.Unix(r.LastUsedAt.Int64, 0).UTC()
			out[i].LastUsedAt = &at
		}
	}
	return out, nil
}

// RevokeGrant disconnects one of userID's apps; someone else's is "not found".
func (s *Service) RevokeGrant(ctx context.Context, userID, id int64) error {
	n, err := s.store.DeleteOAuthGrant(ctx, dbgen.DeleteOAuthGrantParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("connected app")
	}
	return nil
}

// RevokeOAuthToken is the client's side of disconnecting (RFC 7009): either
// of its tokens ends the whole grant. An unknown token is not an error.
func (s *Service) RevokeOAuthToken(ctx context.Context, token string) error {
	row, err := s.store.GetOAuthToken(ctx, s.hashToken(token))
	if database.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.DeleteOAuthGrantByID(ctx, row.GrantID)
}

func clientFromRow(r dbgen.OauthClient) (OAuthClient, error) {
	c := OAuthClient{ClientID: r.ClientID, Name: r.Name, CreatedAt: time.Unix(r.CreatedAt, 0).UTC(), rowID: r.ID}
	if err := json.Unmarshal([]byte(r.RedirectUris), &c.RedirectURIs); err != nil {
		return OAuthClient{}, err
	}
	return c, nil
}

func randomToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func nullTime(t time.Time) sql.NullInt64 { return sql.NullInt64{Int64: t.Unix(), Valid: true} }

// validRedirectURI allows https anywhere and plain http only on a loopback
// address (a native client's local listener).
func validRedirectURI(raw string) bool {
	if len(raw) > maxRedirectURILen {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && isLoopback(u.Hostname()))
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectMatches compares exactly, except that a loopback http URI may
// differ in port: native clients listen on whatever port is free (RFC 8252
// section 7.3).
func redirectMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	a, errA := url.Parse(registered)
	b, errB := url.Parse(requested)
	if errA != nil || errB != nil || a.Scheme != "http" || b.Scheme != "http" || !isLoopback(a.Hostname()) {
		return false
	}
	return a.Hostname() == b.Hostname() && a.Path == b.Path && a.RawQuery == b.RawQuery && b.Fragment == "" && b.User == nil
}

// ValidCodeChallenge reports whether s has the shape of an S256 challenge:
// 43 base64url characters, the encoding of a SHA-256 digest.
func ValidCodeChallenge(s string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(raw) == sha256.Size
}

func verifyPKCE(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}
