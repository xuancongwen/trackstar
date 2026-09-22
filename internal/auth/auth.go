// Package auth implements email/password accounts and server-side sessions.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
	"trackstar/internal/user"
)

const (
	SessionTTL = 30 * 24 * time.Hour

	minPasswordLength = 8
	maxPasswordLength = 72 // bcrypt ignores everything after 72 bytes
)

type Service struct {
	store             database.Store
	secret            []byte
	allowRegistration bool
	bcryptCost        int
	now               func() time.Time
	dummyHash         func() []byte
}

type Options struct {
	Secret            []byte
	AllowRegistration bool
	// BcryptCost defaults to 12; tests lower it.
	BcryptCost int
	Now        func() time.Time
}

func NewService(store database.Store, opts Options) *Service {
	if opts.BcryptCost == 0 {
		opts.BcryptCost = 12
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	// Compared against when the email is unknown so both paths cost the same.
	// Hashing is deliberately slow, so it is computed on first use, not at startup.
	dummy := sync.OnceValue(func() []byte {
		hash, _ := bcrypt.GenerateFromPassword([]byte("trackstar-dummy-password"), opts.BcryptCost)
		return hash
	})
	return &Service{
		store:             store,
		secret:            opts.Secret,
		allowRegistration: opts.AllowRegistration,
		bcryptCost:        opts.BcryptCost,
		now:               opts.Now,
		dummyHash:         dummy,
	}
}

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// RegistrationOpen reports whether a new account may be created right now.
// The very first account can always be created (and becomes the admin), so
// TRACKSTAR_ALLOW_REGISTRATION=false never locks anyone out of a fresh install.
func (s *Service) RegistrationOpen(ctx context.Context) (bool, error) {
	if s.allowRegistration {
		return true, nil
	}
	n, err := s.store.CountUsers(ctx)
	return n == 0, err
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (user.User, error) {
	email := normalizeEmail(in.Email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		return user.User{}, apperr.Invalid("a valid email address is required")
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		name = email[:strings.IndexByte(email, '@')]
	}
	if len(name) > 100 {
		return user.User{}, apperr.Invalid("display name is too long")
	}
	if len(in.Password) < minPasswordLength || len(in.Password) > maxPasswordLength {
		return user.User{}, apperr.Invalid("password must be %d to %d characters", minPasswordLength, maxPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.bcryptCost)
	if err != nil {
		return user.User{}, err
	}

	var created dbgen.User
	err = s.store.InTx(ctx, func(q dbgen.Querier) error {
		count, err := q.CountUsers(ctx)
		if err != nil {
			return err
		}
		if count > 0 && !s.allowRegistration {
			return apperr.Forbidden("registration is disabled")
		}
		if _, err := q.GetUserByEmail(ctx, email); err == nil {
			return apperr.Conflict("an account with this email already exists")
		} else if !database.IsNotFound(err) {
			return err
		}
		created, err = q.CreateUser(ctx, dbgen.CreateUserParams{
			Email:        email,
			PasswordHash: string(hash),
			DisplayName:  name,
			IsAdmin:      count == 0,
			Now:          s.now().Unix(),
		})
		return err
	})
	if err != nil {
		return user.User{}, err
	}
	return user.FromRow(created), nil
}

// SetPassword replaces a user's password and signs them out everywhere. It
// backs the `trackstar reset-password` command; there is no e-mail flow.
func (s *Service) SetPassword(ctx context.Context, email, password string) error {
	return s.setPassword(ctx, password, func(q dbgen.Querier) (dbgen.User, error) {
		return q.GetUserByEmail(ctx, normalizeEmail(email))
	})
}

// SetPasswordByID is SetPassword for an administrator's reset of user id.
func (s *Service) SetPasswordByID(ctx context.Context, id int64, password string) error {
	return s.setPassword(ctx, password, func(q dbgen.Querier) (dbgen.User, error) {
		return q.GetUser(ctx, id)
	})
}

func (s *Service) setPassword(ctx context.Context, password string, find func(dbgen.Querier) (dbgen.User, error)) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return apperr.Invalid("password must be %d to %d characters", minPasswordLength, maxPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q dbgen.Querier) error {
		row, err := find(q)
		if database.IsNotFound(err) {
			return apperr.NotFound("user")
		}
		if err != nil {
			return err
		}
		if err := q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{ID: row.ID, PasswordHash: string(hash), Now: s.now().Unix()}); err != nil {
			return err
		}
		return q.DeleteUserSessions(ctx, row.ID)
	})
}

// ChangePassword lets a signed-in user change their own password after
// proving the current one. Other sessions are revoked; keepToken survives.
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next, keepToken string) error {
	row, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(current)) != nil {
		return apperr.Invalid("current password is incorrect")
	}
	if err := s.SetPassword(ctx, row.Email, next); err != nil {
		return err
	}
	if keepToken == "" {
		return nil
	}
	// SetPassword dropped every session; re-create the caller's under the same token.
	now := s.now()
	return s.store.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: s.hashToken(keepToken), UserID: userID, CreatedAt: now.Unix(), ExpiresAt: now.Add(SessionTTL).Unix(),
	})
}

// Login verifies credentials and returns a new session token.
func (s *Service) Login(ctx context.Context, email, password string) (string, user.User, error) {
	row, err := s.store.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil && !database.IsNotFound(err) {
		return "", user.User{}, err
	}
	hash := s.dummyHash()
	if err == nil {
		hash = []byte(row.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || err != nil {
		return "", user.User{}, apperr.Unauthorized("invalid email or password")
	}
	if !row.IsActive {
		return "", user.User{}, apperr.Forbidden("this account has been deactivated")
	}
	token, err := s.StartSession(ctx, row.ID)
	if err != nil {
		return "", user.User{}, err
	}
	return token, user.FromRow(row), nil
}

// StartSession creates a session for userID. Only an HMAC of the token is
// stored, so a leaked database does not leak usable sessions.
func (s *Service) StartSession(ctx context.Context, userID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	// Opportunistic cleanup instead of a background worker.
	if err := s.store.DeleteExpiredSessions(ctx, now.Unix()); err != nil {
		return "", err
	}
	err := s.store.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: s.hashToken(token),
		UserID:    userID,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(SessionTTL).Unix(),
	})
	return token, err
}

// Authenticate resolves a session token to its user.
func (s *Service) Authenticate(ctx context.Context, token string) (user.User, error) {
	if token == "" {
		return user.User{}, apperr.Unauthorized("not signed in")
	}
	row, err := s.store.GetSessionUser(ctx, dbgen.GetSessionUserParams{
		TokenHash: s.hashToken(token),
		Now:       s.now().Unix(),
	})
	if database.IsNotFound(err) {
		return user.User{}, apperr.Unauthorized("not signed in")
	}
	if err != nil {
		return user.User{}, err
	}
	return user.FromRow(row), nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, s.hashToken(token))
}

func (s *Service) hashToken(token string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
