package user

import (
	"context"
	"testing"

	"tracker/internal/apperr"
	"tracker/internal/database"
	"tracker/internal/database/dbgen"
)

func ptr[T any](v T) *T { return &v }

func TestAdminUpdateProtectsLastAdmin(t *testing.T) {
	ctx := context.Background()
	db := database.NewTestDB(t)
	svc := NewService(db, nil)
	admin, _ := db.CreateUser(ctx, dbgen.CreateUserParams{Email: "a@example.com", PasswordHash: "x", DisplayName: "A", IsAdmin: true, Now: 1})
	dev, _ := db.CreateUser(ctx, dbgen.CreateUserParams{Email: "d@example.com", PasswordHash: "x", DisplayName: "D", Now: 1})

	if _, err := svc.Update(ctx, admin.ID, admin.ID, AdminUpdate{IsAdmin: ptr(false)}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("demoting the last admin: err = %v", err)
	}
	if _, err := svc.Update(ctx, admin.ID, admin.ID, AdminUpdate{IsActive: ptr(false)}); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("deactivating the last admin: err = %v", err)
	}
	u, err := svc.Update(ctx, admin.ID, dev.ID, AdminUpdate{IsAdmin: ptr(true), DisplayName: ptr("  Dev  Two ")})
	if err != nil || !u.IsAdmin || u.DisplayName != "Dev Two" {
		t.Fatalf("promote: %+v, %v", u, err)
	}
	// Now there are two admins, so the first can step down.
	if _, err := svc.Update(ctx, admin.ID, admin.ID, AdminUpdate{IsAdmin: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, dev.ID, 999, AdminUpdate{}); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("unknown user: err = %v", err)
	}
	if _, err := svc.Rename(ctx, dev.ID, " "); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("blank name: err = %v", err)
	}
}

func TestDeactivationRevokesSessions(t *testing.T) {
	ctx := context.Background()
	db := database.NewTestDB(t)
	svc := NewService(db, nil)
	admin, _ := db.CreateUser(ctx, dbgen.CreateUserParams{Email: "a@example.com", PasswordHash: "x", DisplayName: "A", IsAdmin: true, Now: 1})
	dev, _ := db.CreateUser(ctx, dbgen.CreateUserParams{Email: "d@example.com", PasswordHash: "x", DisplayName: "D", Now: 1})
	if err := db.CreateSession(ctx, dbgen.CreateSessionParams{TokenHash: "h", UserID: dev.ID, CreatedAt: 1, ExpiresAt: 1 << 40}); err != nil {
		t.Fatal(err)
	}
	u, err := svc.Update(ctx, admin.ID, dev.ID, AdminUpdate{IsActive: ptr(false)})
	if err != nil || u.IsActive {
		t.Fatalf("deactivate: %+v, %v", u, err)
	}
	if _, err := db.GetSessionUser(ctx, dbgen.GetSessionUserParams{TokenHash: "h", Now: 2}); !database.IsNotFound(err) {
		t.Fatalf("session still valid after deactivation: %v", err)
	}
	users, _ := svc.List(ctx)
	if len(users) != 2 {
		t.Fatalf("deactivated users must still be listed, got %d", len(users))
	}
}
