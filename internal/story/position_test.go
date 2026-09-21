package story

import "testing"

func p(v int64) *int64 { return &v }

func TestBetween(t *testing.T) {
	cases := []struct {
		name       string
		prev, next *int64
		want       int64
		ok         bool
	}{
		{"empty section", nil, nil, Gap, true},
		{"top", nil, p(1000), 1000 - Gap, true},
		{"bottom", p(1000), nil, 1000 + Gap, true},
		{"middle", p(1000), p(2000), 1500, true},
		{"tight", p(10), p(12), 11, true},
		{"negative", p(-5), p(5), 0, true},
		{"no room", p(10), p(11), 0, false},
		{"equal", p(10), p(10), 0, false},
	}
	for _, c := range cases {
		got, ok := Between(c.prev, c.next)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: Between = %d, %v; want %d, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestBetweenExhaustsAfterRepeatedInserts(t *testing.T) {
	lo, hi := int64(0), Gap
	inserts := 0
	for {
		mid, ok := Between(&lo, &hi)
		if !ok {
			break
		}
		if mid <= lo || mid >= hi {
			t.Fatalf("mid %d not strictly between %d and %d", mid, lo, hi)
		}
		hi = mid
		inserts++
	}
	if inserts != 16 {
		t.Fatalf("inserts before exhaustion = %d, want 16", inserts)
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize(3)
	want := []int64{Gap, 2 * Gap, 3 * Gap}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Normalize(3) = %v, want %v", got, want)
		}
	}
}
