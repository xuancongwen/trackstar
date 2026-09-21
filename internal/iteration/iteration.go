// Package iteration computes iterations from a project's settings. Iterations
// are never persisted: number N always covers the same calendar range for a
// given schedule, so history is derived on demand.
package iteration

import "time"

// Schedule describes how a project slices time into iterations.
type Schedule struct {
	LengthDays   int
	StartWeekday time.Weekday
	// Anchor is any instant inside iteration 1 (the project's creation time).
	Anchor time.Time
	// Location decides where day boundaries fall; nil means UTC.
	Location *time.Location
}

type Iteration struct {
	Number  int       `json:"number"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"` // exclusive
}

// Contains reports whether t falls inside the iteration.
func (it Iteration) Contains(t time.Time) bool {
	return !t.Before(it.StartAt) && t.Before(it.EndAt)
}

func (s Schedule) loc() *time.Location {
	if s.Location == nil {
		return time.UTC
	}
	return s.Location
}

func (s Schedule) length() int {
	if s.LengthDays < 1 {
		return 7
	}
	return s.LengthDays
}

// origin is local midnight of the first day of iteration 1: the closest
// StartWeekday on or before the anchor.
func (s Schedule) origin() time.Time {
	a := s.Anchor.In(s.loc())
	back := (int(a.Weekday()) - int(s.StartWeekday) + 7) % 7
	return time.Date(a.Year(), a.Month(), a.Day()-back, 0, 0, 0, 0, s.loc())
}

// Number returns iteration n (1-based). Calendar arithmetic (not fixed 24h
// steps) keeps boundaries at local midnight across DST changes.
func (s Schedule) Number(n int) Iteration {
	o := s.origin()
	start := time.Date(o.Year(), o.Month(), o.Day()+(n-1)*s.length(), 0, 0, 0, 0, s.loc())
	end := time.Date(o.Year(), o.Month(), o.Day()+n*s.length(), 0, 0, 0, 0, s.loc())
	return Iteration{Number: n, StartAt: start.UTC(), EndAt: end.UTC()}
}

// At returns the iteration containing t. Instants before the anchor's
// iteration are clamped to iteration 1.
func (s Schedule) At(t time.Time) Iteration {
	o := s.origin()
	if t.Before(o) {
		return s.Number(1)
	}
	n := int(t.Sub(o)/(time.Duration(s.length())*24*time.Hour)) + 1
	// The division assumes 24h days; step to absorb DST drift.
	for it := s.Number(n); ; it = s.Number(n) {
		switch {
		case t.Before(it.StartAt):
			n--
		case !t.Before(it.EndAt):
			n++
		default:
			return it
		}
	}
}

// Range returns iterations from..to inclusive.
func (s Schedule) Range(from, to int) []Iteration {
	if from < 1 {
		from = 1
	}
	out := make([]Iteration, 0, max(0, to-from+1))
	for n := from; n <= to; n++ {
		out = append(out, s.Number(n))
	}
	return out
}
