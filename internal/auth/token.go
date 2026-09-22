package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
	"trackstar/internal/user"
)

// TokenPrefix marks personal API tokens so they are recognisable in logs and
// secret scanners, and cannot be confused with a session cookie value.
const TokenPrefix = "tst_"

// touchInterval bounds how often a token's last_used_at is written: an agent
// polling every few seconds must not turn every read into a write.
const touchInterval = 5 * time.Minute

const maxTokenNameLength = 100

// Token is a personal API token as shown to its owner. The secret itself is
// only ever returned once, from CreateToken.
type Token struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// CreatedToken is Token plus the secret, returned exactly once.
type CreatedToken struct {
	Token
	Secret string `json:"token"`
}

type CreateTokenInput struct {
	Name string `json:"name"`
	// ExpiresInDays of 0 (or absent) means the token never expires.
	ExpiresInDays int `json:"expires_in_days"`
}

// CreateToken mints a bearer token that acts as userID.
func (s *Service) CreateToken(ctx context.Context, userID int64, in CreateTokenInput) (CreatedToken, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreatedToken{}, apperr.Invalid("a token name is required")
	}
	if len(name) > maxTokenNameLength {
		return CreatedToken{}, apperr.Invalid("token name is too long")
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > 3650 {
		return CreatedToken{}, apperr.Invalid("expires_in_days must be between 0 and 3650")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return CreatedToken{}, err
	}
	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	var expires sql.NullInt64
	if in.ExpiresInDays > 0 {
		expires = sql.NullInt64{Int64: now.Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour).Unix(), Valid: true}
	}
	row, err := s.store.CreateAPIToken(ctx, dbgen.CreateAPITokenParams{
		UserID:    userID,
		Name:      name,
		TokenHash: s.hashToken(secret),
		CreatedAt: now.Unix(),
		ExpiresAt: expires,
	})
	if err != nil {
		return CreatedToken{}, err
	}
	return CreatedToken{Token: tokenFromRow(row), Secret: secret}, nil
}

// Tokens lists userID's tokens, newest first.
func (s *Service) Tokens(ctx context.Context, userID int64) ([]Token, error) {
	rows, err := s.store.ListAPITokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Token, len(rows))
	for i, r := range rows {
		out[i] = tokenFromRow(r)
	}
	return out, nil
}

// RevokeToken deletes one of userID's tokens; someone else's is "not found".
func (s *Service) RevokeToken(ctx context.Context, userID, id int64) error {
	n, err := s.store.DeleteAPIToken(ctx, dbgen.DeleteAPITokenParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("token")
	}
	return nil
}

// AuthenticateToken resolves a bearer token to its user. Expired tokens and
// tokens of deactivated users fail like unknown ones. The lookup is one
// indexed read of an HMAC, so an attacker gains nothing from timing.
func (s *Service) AuthenticateToken(ctx context.Context, secret string) (user.User, error) {
	if !strings.HasPrefix(secret, TokenPrefix) {
		return user.User{}, apperr.Unauthorized("invalid API token")
	}
	now := s.now()
	row, err := s.store.GetAPITokenUser(ctx, dbgen.GetAPITokenUserParams{TokenHash: s.hashToken(secret), Now: sql.NullInt64{Int64: now.Unix(), Valid: true}})
	if database.IsNotFound(err) {
		return user.User{}, apperr.Unauthorized("invalid API token")
	}
	if err != nil {
		return user.User{}, err
	}
	if !row.LastUsedAt.Valid || now.Unix()-row.LastUsedAt.Int64 >= int64(touchInterval/time.Second) {
		if err := s.store.TouchAPIToken(ctx, dbgen.TouchAPITokenParams{ID: row.TokenID, Now: sql.NullInt64{Int64: now.Unix(), Valid: true}}); err != nil {
			return user.User{}, err
		}
	}
	return user.FromRow(row.User), nil
}

func tokenFromRow(r dbgen.ApiToken) Token {
	t := Token{ID: r.ID, Name: r.Name, CreatedAt: time.Unix(r.CreatedAt, 0).UTC()}
	if r.LastUsedAt.Valid {
		at := time.Unix(r.LastUsedAt.Int64, 0).UTC()
		t.LastUsedAt = &at
	}
	if r.ExpiresAt.Valid {
		at := time.Unix(r.ExpiresAt.Int64, 0).UTC()
		t.ExpiresAt = &at
	}
	return t
}
