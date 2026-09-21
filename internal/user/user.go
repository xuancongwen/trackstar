// Package user holds the user domain type.
package user

import (
	"context"
	"time"

	"tracker/internal/database/dbgen"
)

type User struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	CreatedAt   time.Time `json:"created_at"`
}

// FromRow converts a database row, dropping the password hash.
func FromRow(r dbgen.User) User {
	return User{
		ID:          r.ID,
		Email:       r.Email,
		DisplayName: r.DisplayName,
		IsAdmin:     r.IsAdmin,
		CreatedAt:   time.Unix(r.CreatedAt, 0).UTC(),
	}
}

type Service struct{ q dbgen.Querier }

func NewService(q dbgen.Querier) *Service { return &Service{q: q} }

func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, len(rows))
	for i, r := range rows {
		users[i] = FromRow(r)
	}
	return users, nil
}
