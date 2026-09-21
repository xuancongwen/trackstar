package project

import (
	"context"
	"testing"

	"tracker/internal/apperr"
	"tracker/internal/database"
)

func ptr[T any](v T) *T { return &v }

func TestCreateDefaultsAndUniqueSlug(t *testing.T) {
	ctx := context.Background()
	svc := NewService(database.NewTestDB(t), nil)

	p, err := svc.Create(ctx, Input{Name: ptr("  Apollo: Launch Pad!  ")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "apollo-launch-pad" || p.IterationLengthDays != 7 || p.IterationStartWeekday != 1 || p.VelocityWindow != 3 {
		t.Fatalf("unexpected project: %+v", p)
	}
	p2, err := svc.Create(ctx, Input{Name: ptr("Apollo Launch Pad")})
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
	p, err := svc.Create(ctx, Input{Name: ptr("Apollo")})
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
