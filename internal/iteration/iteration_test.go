package iteration

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d, h int, loc *time.Location) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, loc)
}

func TestFirstIterationStartsOnWeekdayBeforeAnchor(t *testing.T) {
	// 2026-09-17 is a Thursday.
	s := Schedule{LengthDays: 7, StartWeekday: time.Monday, Anchor: date(2026, 9, 17, 15, time.UTC)}
	it := s.Number(1)
	if want := date(2026, 9, 14, 0, time.UTC); !it.StartAt.Equal(want) {
		t.Errorf("start = %v, want %v", it.StartAt, want)
	}
	if want := date(2026, 9, 21, 0, time.UTC); !it.EndAt.Equal(want) {
		t.Errorf("end = %v, want %v", it.EndAt, want)
	}
}

func TestAnchorOnStartWeekday(t *testing.T) {
	s := Schedule{LengthDays: 7, StartWeekday: time.Monday, Anchor: date(2026, 9, 14, 0, time.UTC)}
	if got := s.Number(1).StartAt; !got.Equal(date(2026, 9, 14, 0, time.UTC)) {
		t.Errorf("start = %v", got)
	}
}

func TestAt(t *testing.T) {
	s := Schedule{LengthDays: 14, StartWeekday: time.Wednesday, Anchor: date(2026, 1, 1, 12, time.UTC)}
	// 2026-01-01 is a Thursday → iteration 1 starts Wed 2025-12-31.
	cases := []struct {
		at   time.Time
		want int
	}{
		{date(2025, 12, 31, 0, time.UTC), 1},
		{date(2026, 1, 13, 23, time.UTC), 1},
		{date(2026, 1, 14, 0, time.UTC), 2},
		{date(2026, 2, 11, 0, time.UTC), 4},
		{date(2020, 1, 1, 0, time.UTC), 1}, // clamped
	}
	for _, c := range cases {
		it := s.At(c.at)
		if it.Number != c.want {
			t.Errorf("At(%v) = %d, want %d", c.at, it.Number, c.want)
		}
		if c.at.After(s.Number(1).StartAt) && !it.Contains(c.at) {
			t.Errorf("iteration %d does not contain %v", it.Number, c.at)
		}
	}
}

func TestIterationsAreContiguousAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	s := Schedule{LengthDays: 7, StartWeekday: time.Monday, Anchor: date(2026, 2, 2, 9, loc), Location: loc}
	its := s.Range(1, 12) // spans the March DST change
	for i, it := range its {
		local := it.StartAt.In(loc)
		if local.Weekday() != time.Monday || local.Hour() != 0 {
			t.Errorf("iteration %d starts %v, want Monday midnight", it.Number, local)
		}
		if i > 0 && !its[i-1].EndAt.Equal(it.StartAt) {
			t.Errorf("gap between %d and %d", its[i-1].Number, it.Number)
		}
		if got := s.At(it.StartAt).Number; got != it.Number {
			t.Errorf("At(start of %d) = %d", it.Number, got)
		}
		if got := s.At(it.EndAt.Add(-time.Second)).Number; got != it.Number {
			t.Errorf("At(end of %d) = %d", it.Number, got)
		}
	}
}
