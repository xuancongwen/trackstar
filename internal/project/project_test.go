package project

import (
	"context"
	"testing"

	"trackstar/internal/apperr"
	"trackstar/internal/database"
	"trackstar/internal/testutil"
)

func ptr[T any](v T) *T { return &v }

func TestCreateDefaultsAndUniqueSlug(t *testing.T) {
	ctx := context.Background()
	svc := NewService(database.NewTestDB(t), nil)

	p, err := svc.Create(ctx, 0, Input{Name: ptr("  Apollo: Launch Pad!  ")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "apollo-launch-pad" || p.IterationLengthDays != 7 || p.IterationStartWeekday != 1 || p.VelocityWindow != 3 {
		t.Fatalf("unexpected project: %+v", p)
	}
	p2, err := svc.Create(ctx, 0, Input{Name: ptr("Apollo Launch Pad")})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Slug != "apollo-launch-pad-2" {
		t.Fatalf("slug = %q", p2.Slug)
	}
}

func TestUpdateIsPartialAndValidated(t *testing.T) {
	ctx := context.Background()
	svc := NewService(database.NewTestDB(t), nil)
	p, err := svc.Create(ctx, 0, Input{Name: ptr("Apollo")})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Update(ctx, p.ID, Input{IterationLengthDays: ptr(int64(14)), VelocityWindow: ptr(int64(5))})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Apollo" || got.IterationLengthDays != 14 || got.VelocityWindow != 5 || got.Slug != p.Slug {
		t.Fatalf("unexpected project: %+v", got)
	}

	for _, in := range []Input{
		{Name: ptr(" ")},
		{IterationLengthDays: ptr(int64(10))},
		{IterationStartWeekday: ptr(int64(7))},
		{VelocityWindow: ptr(int64(0))},
	} {
		if _, err := svc.Update(ctx, p.ID, in); apperr.KindOf(err) != apperr.KindInvalid {
			t.Errorf("Update(%+v) err = %v, want invalid", in, err)
		}
	}
	if _, err := svc.Get(ctx, 999); apperr.KindOf(err) != apperr.KindNotFound {
		t.Errorf("Get(999) err = %v, want not found", err)
	}
}

func TestMembershipAccess(t *testing.T) {
	ctx := context.Background()
	db := database.NewTestDB(t)
	svc := NewService(db, nil)
	alice := testutil.CreateUser(t, db, "alice@example.com")
	bob := testutil.CreateUser(t, db, "bob@example.com")
	p, _ := svc.Create(ctx, alice, Input{Name: ptr("Apollo")})
	open, _ := svc.Create(ctx, 0, Input{Name: ptr("Open")})

	// The creator owns the project; nobody else sees it.
	if a, _ := svc.AccessFor(ctx, p.ID, alice, false); !a.Read || !a.Write || !a.Manage {
		t.Fatalf("owner access = %+v", a)
	}
	if a, _ := svc.AccessFor(ctx, p.ID, bob, false); a.Read || a.Write || a.Manage {
		t.Fatalf("non-member access = %+v", a)
	}
	if a, _ := svc.AccessFor(ctx, p.ID, bob, true); !a.Manage {
		t.Fatal("admins always have full access")
	}
	// A project without members is open to all but managed by admins only.
	if a, _ := svc.AccessFor(ctx, open.ID, bob, false); !a.Read || !a.Write || a.Manage {
		t.Fatalf("open project access = %+v", a)
	}
	visible, _ := svc.ListVisible(ctx, bob, false)
	if len(visible) != 1 || visible[0].ID != open.ID {
		t.Fatalf("bob sees %+v, want only the open project", visible)
	}

	// Members read and write but do not manage.
	if err := svc.SetMember(ctx, p.ID, bob, RoleMember); err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.AccessFor(ctx, p.ID, bob, false); !a.Read || !a.Write || a.Manage {
		t.Fatalf("member access = %+v", a)
	}
	if visible, _ := svc.ListVisible(ctx, bob, false); len(visible) != 2 {
		t.Fatalf("bob sees %d projects, want 2", len(visible))
	}
	if err := svc.SetMember(ctx, p.ID, bob, "viewer"); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("bad role: err = %v", err)
	}

	// The only owner can neither step down nor leave; a second owner can.
	if err := svc.SetMember(ctx, p.ID, alice, RoleMember); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("demoting the only owner: err = %v", err)
	}
	if err := svc.RemoveMember(ctx, p.ID, alice); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("removing the only owner: err = %v", err)
	}
	if err := svc.SetMember(ctx, p.ID, bob, RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMember(ctx, p.ID, alice, RoleMember); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveMember(ctx, p.ID, alice); err != nil { // members may always be removed
		t.Fatal(err)
	}
	if err := svc.RemoveMember(ctx, p.ID, bob); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("removing the last owner: err = %v", err)
	}
	if members, _ := svc.Members(ctx, p.ID); len(members) != 1 || members[0].Role != RoleOwner {
		t.Fatalf("members = %+v", members)
	}
}

func TestSavedFilters(t *testing.T) {
	ctx := context.Background()
	db := database.NewTestDB(t)
	svc := NewService(db, nil)
	p, _ := svc.Create(ctx, 0, Input{Name: ptr("Apollo")})
	alice := testutil.CreateUser(t, db, "alice@example.com")
	bob := testutil.CreateUser(t, db, "bob@example.com")

	f, err := svc.SaveFilter(ctx, alice, p.ID, " Bugs ", "type:bug  state:started")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Bugs" || f.Query != "type:bug state:started" {
		t.Fatalf("filter = %+v", f)
	}
	if _, err := svc.SaveFilter(ctx, alice, p.ID, "Bugs", "type:bug"); err != nil { // overwrite by name
		t.Fatal(err)
	}
	list, _ := svc.SavedFilters(ctx, alice, p.ID)
	if len(list) != 1 || list[0].Query != "type:bug" {
		t.Fatalf("filters = %+v", list)
	}
	if list, _ := svc.SavedFilters(ctx, bob, p.ID); len(list) != 0 {
		t.Fatal("filters are per user")
	}
	if err := svc.DeleteFilter(ctx, bob, list[0].ID); apperr.KindOf(err) != apperr.KindNotFound {
		t.Fatalf("deleting someone else's filter: err = %v", err)
	}
	if err := svc.DeleteFilter(ctx, alice, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFilter(ctx, alice, p.ID, "", "x"); apperr.KindOf(err) != apperr.KindInvalid {
		t.Fatalf("blank name: err = %v", err)
	}
}
