// Package testutil has fixtures shared by service tests.
package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"tracker/internal/database"
	"tracker/internal/database/dbgen"
)

// Clock is a manually advanced clock.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func NewClock(t time.Time) *Clock { return &Clock{t: t} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *Clock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

// CreateUser inserts a user directly (no password hashing cost).
func CreateUser(t testing.TB, store database.Store, email string) int64 {
	t.Helper()
	u, err := store.CreateUser(context.Background(), dbgen.CreateUserParams{
		Email: email, PasswordHash: "x", DisplayName: email, Now: 1,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}
