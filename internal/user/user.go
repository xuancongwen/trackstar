// Package user holds the user domain type and account management.
package user

import (
	"context"
	"strings"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/database/dbgen"
)

type User struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// FromRow converts a database row, dropping the password hash.
func FromRow(r dbgen.User) User {
	return User{
		ID:          r.ID,
		Email:       r.Email,
		DisplayName: r.DisplayName,
		IsAdmin:     r.IsAdmin,
		IsActive:    r.IsActive,
		CreatedAt:   time.Unix(r.CreatedAt, 0).UTC(),
	}
}

// AdminUpdate is what an administrator may change on any account.
// nil fields are left alone.
type AdminUpdate struct {
	DisplayName *string `json:"display_name"`
	IsAdmin     *bool   `json:"is_admin"`
	IsActive    *bool   `json:"is_active"`
}

type Service struct {
	store database.Store
	now   func() time.Time
}

func NewService(store database.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

// List returns every account, active or not: deactivated users still appear
// as owners and requesters of old stories.
func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, len(rows))
	for i, r := range rows {
		users[i] = FromRow(r)
	}
	return users, nil
}

// Rename changes the caller's own display name.
func (s *Service) Rename(ctx context.Context, id int64, displayName string) (User, error) {
	name, err := cleanName(displayName)
	if err != nil {
		return User{}, err
	}
	var row dbgen.User
	err = s.store.InTx(ctx, func(q dbgen.Querier) error {
		u, err := q.GetUser(ctx, id)
		if err != nil {
			return notFound(err)
		}
		row, err = q.UpdateUser(ctx, dbgen.UpdateUserParams{ID: id, DisplayName: name, IsAdmin: u.IsAdmin, IsActive: u.IsActive, Now: s.now().Unix()})
		return err
	})
	if err != nil {
		return User{}, err
	}
	return FromRow(row), nil
}

// Update applies an administrator's change to another (or their own) account.
// The last active administrator can neither be demoted nor deactivated, so
// the instance can never end up without one.
func (s *Service) Update(ctx context.Context, actorID, id int64, in AdminUpdate) (User, error) {
	var row dbgen.User
	err := s.store.InTx(ctx, func(q dbgen.Querier) error {
		u, err := q.GetUser(ctx, id)
		if err != nil {
			return notFound(err)
		}
		name := u.DisplayName
		if in.DisplayName != nil {
			if name, err = cleanName(*in.DisplayName); err != nil {
				return err
			}
		}
		isAdmin, isActive := u.IsAdmin, u.IsActive
		if in.IsAdmin != nil {
			isAdmin = *in.IsAdmin
		}
		if in.IsActive != nil {
			isActive = *in.IsActive
		}
		if u.IsAdmin && u.IsActive && !(isAdmin && isActive) {
			n, err := q.CountActiveAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return apperr.Invalid("this is the last active administrator")
			}
		}
		row, err = q.UpdateUser(ctx, dbgen.UpdateUserParams{ID: id, DisplayName: name, IsAdmin: isAdmin, IsActive: isActive, Now: s.now().Unix()})
		if err != nil {
			return err
		}
		if !isActive {
			return q.DeleteUserSessions(ctx, id) // signed out everywhere, immediately
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return FromRow(row), nil
}

func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", apperr.Invalid("display name is required")
	}
	if len(name) > 100 {
		return "", apperr.Invalid("display name is too long")
	}
	return name, nil
}

func notFound(err error) error {
	if database.IsNotFound(err) {
		return apperr.NotFound("user")
	}
	return err
}
