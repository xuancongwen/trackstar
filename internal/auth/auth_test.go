package auth

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"tracker/internal/apperr"
	"tracker/internal/database"
)

func newService(t *testing.T, allowRegistration bool) (*Service, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	svc := NewService(database.NewTestDB(t), Options{
		Secret:            []byte("test-secret-test-secret-test-secret"),
		AllowRegistration: allowRegistration,
		BcryptCost:        bcrypt.MinCost,
		Now:               func() time.Time { return now },
	})
	return svc, &now
}

func TestRegisterLoginLogout(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, true)

	u, err := svc.Register(ctx, RegisterInput{Email: " Sam@Example.com ", Password: "correct horse", DisplayName: "Sam"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "sam@example.com" || !u.IsAdmin {
		t.Fatalf("first user = %+v, want normalized email and admin", u)
	}

	second, err := svc.Register(ctx, RegisterInput{Email: "kim@example.com", Password: "another pass"})
	if err != nil {
		t.Fatal(err)
	}
	if second.IsAdmin || second.DisplayName != "kim" {
		t.Fatalf("second user = %+v", second)
	}

	if _, _, err := svc.Login(ctx, "sam@example.com", "wrong password"); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("wrong password: err = %v", err)
	}
	if _, _, err := svc.Login(ctx, "nobody@example.com", "correct horse"); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("unknown user: err = %v", err)
	}

	token, _, err := svc.Login(ctx, "SAM@example.com", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Authenticate(ctx, token)
	if err != nil || got.ID != u.ID {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}
	if _, err := svc.Authenticate(ctx, token+"x"); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("tampered token: err = %v", err)
	}

	if err := svc.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("after logout: err = %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, true)
	for _, in := range []RegisterInput{
		{Email: "not-an-email", Password: "long enough"},
		{Email: "a@example.com", Password: "short"},
	} {
		if _, err := svc.Register(ctx, in); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("Register(%+v) err = %v, want invalid", in, err)
		}
	}
	if _, err := svc.Register(ctx, RegisterInput{Email: "a@example.com", Password: "long enough"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Email: "A@example.com", Password: "long enough"}); apperr.KindOf(err) != apperr.KindConflict {
		t.Errorf("duplicate email: err = %v, want conflict", err)
	}
}

func TestRegistrationDisabledStillAllowsFirstUser(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, false)

	if open, _ := svc.RegistrationOpen(ctx); !open {
		t.Fatal("registration should be open on an empty install")
	}
	if _, err := svc.Register(ctx, RegisterInput{Email: "admin@example.com", Password: "long enough"}); err != nil {
		t.Fatal(err)
	}
	if open, _ := svc.RegistrationOpen(ctx); open {
		t.Fatal("registration should be closed after the first user")
	}
	if _, err := svc.Register(ctx, RegisterInput{Email: "two@example.com", Password: "long enough"}); apperr.KindOf(err) != apperr.KindForbidden {
		t.Fatalf("err = %v, want forbidden", err)
	}
}

func TestSessionExpires(t *testing.T) {
	ctx := context.Background()
	svc, now := newService(t, true)
	if _, err := svc.Register(ctx, RegisterInput{Email: "a@example.com", Password: "long enough"}); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.Login(ctx, "a@example.com", "long enough")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(SessionTTL + time.Second)
	if _, err := svc.Authenticate(ctx, token); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("expired session: err = %v", err)
	}
}

func TestLimiter(t *testing.T) {
	now := time.Now()
	l := NewLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	if !l.Allow("ip") || !l.Allow("ip") {
		t.Fatal("first two attempts must pass")
	}
	if l.Allow("ip") {
		t.Fatal("third attempt must be blocked")
	}
	if !l.Allow("other") {
		t.Fatal("other keys are independent")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("ip") {
		t.Fatal("window should have reset")
	}
}

func TestSetPassword(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, true)
	if _, err := svc.Register(ctx, RegisterInput{Email: "a@example.com", Password: "old password"}); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.Login(ctx, "a@example.com", "old password")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPassword(ctx, "A@example.com", "new password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("old session should be revoked, err = %v", err)
	}
	if _, _, err := svc.Login(ctx, "a@example.com", "old password"); apperr.KindOf(err) != apperr.KindUnauthorized {
		t.Fatalf("old password still works, err = %v", err)
	}
	if _, _, err := svc.Login(ctx, "a@example.com", "new password"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPassword(ctx, "nobody@example.com", "new password"); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("unknown user: err = %v", err)
	}
}
