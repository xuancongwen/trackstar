package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/database/dbgen"
)

func TestAPITokens(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	u, err := svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.CreateToken(ctx, u.ID, CreateTokenInput{Name: " laptop "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Secret, TokenPrefix) || created.Name != "laptop" || created.ExpiresAt != nil || created.LastUsedAt != nil {
		t.Fatalf("created = %+v", created)
	}

	got, err := svc.AuthenticateToken(ctx, created.Secret)
	if err != nil || got.ID != u.ID {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}
	// A session token is not an API token, and vice versa.
	if _, err := svc.Authenticate(ctx, created.Secret); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("API token as session: err = %v", err)
	}
	sess, _ := svc.StartSession(ctx, u.ID)
	if _, err := svc.AuthenticateToken(ctx, sess); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("session as API token: err = %v", err)
	}
	if _, err := svc.AuthenticateToken(ctx, created.Secret+"x"); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("tampered: err = %v", err)
	}

	// last_used_at is written on first use, then at most every touchInterval.
	list, _ := svc.Tokens(ctx, u.ID)
	if len(list) != 1 || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(*now) {
		t.Fatalf("after first use: %+v", list)
	}
	*now = now.Add(time.Minute)
	if _, err := svc.AuthenticateToken(ctx, created.Secret); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.Tokens(ctx, u.ID)
	if list[0].LastUsedAt.Equal(*now) {
		t.Fatalf("last_used_at written within touchInterval: %+v", list[0])
	}
	*now = now.Add(touchInterval)
	if _, err := svc.AuthenticateToken(ctx, created.Secret); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.Tokens(ctx, u.ID)
	if !list[0].LastUsedAt.Equal(*now) {
		t.Fatalf("last_used_at not refreshed after touchInterval: %+v", list[0])
	}

	// Expiry.
	short, err := svc.CreateToken(ctx, u.ID, CreateTokenInput{Name: "ci", ExpiresInDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	if short.ExpiresAt == nil || !short.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("expires_at = %v", short.ExpiresAt)
	}
	if _, err := svc.AuthenticateToken(ctx, short.Secret); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(25 * time.Hour)
	if _, err := svc.AuthenticateToken(ctx, short.Secret); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("expired: err = %v", err)
	}

	// Revoking someone else's token is "not found"; revoking your own works.
	other, _ := svc.Register(ctx, RegisterInput{Email: "kim@example.com", Password: "another pass"})
	if err := svc.RevokeToken(ctx, other.ID, created.ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("revoke foreign: err = %v", err)
	}
	if err := svc.RevokeToken(ctx, u.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, created.Secret); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("revoked: err = %v", err)
	}
	list, _ = svc.Tokens(ctx, u.ID)
	if len(list) != 1 || list[0].ID != short.ID {
		t.Fatalf("after revoke: %+v", list)
	}
}

func TestAPITokenValidation(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, true)
	u, _ := svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"})
	for _, in := range []CreateTokenInput{
		{Name: "  "},
		{Name: strings.Repeat("x", 101)},
		{Name: "ok", ExpiresInDays: -1},
		{Name: "ok", ExpiresInDays: 4000},
	} {
		if _, err := svc.CreateToken(ctx, u.ID, in); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("%+v: err = %v, want invalid", in, err)
		}
	}
}

func TestAPITokenDeactivatedUser(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	svc.Register(ctx, RegisterInput{Email: "sam@example.com", Password: "correct horse"}) // the admin
	kim, _ := svc.Register(ctx, RegisterInput{Email: "kim@example.com", Password: "another pass"})
	tok, err := svc.CreateToken(ctx, kim.ID, CreateTokenInput{Name: "bot"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, tok.Secret); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.UpdateUser(ctx, dbgen.UpdateUserParams{ID: kim.ID, DisplayName: kim.DisplayName, IsActive: false, Now: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, tok.Secret); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("deactivated user's token: err = %v", err)
	}
}
