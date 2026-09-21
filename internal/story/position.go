package story

// Story ordering uses sparse integer positions: neighbours start Gap apart and
// an insert takes the midpoint, so a drag updates exactly one row. When two
// neighbours become adjacent the section is rebalanced once (Normalize) and
// the insert retried.
//
// Everything that knows about this strategy is in this file; swapping in
// LexoRank-style keys later means replacing Between/Normalize and the column
// type, nothing else.

// Gap is the distance between freshly normalized positions. 2^16 allows 16
// consecutive inserts into the same slot before a rebalance is needed.
const Gap int64 = 1 << 16

// Between returns a position strictly between prev and next. A nil bound means
// "no neighbour on that side". ok is false when there is no room left.
func Between(prev, next *int64) (pos int64, ok bool) {
	switch {
	case prev == nil && next == nil:
		return Gap, true
	case prev == nil:
		return *next - Gap, true
	case next == nil:
		return *prev + Gap, true
	case *next-*prev < 2:
		return 0, false
	default:
		return *prev + (*next-*prev)/2, true
	}
}

// Normalize returns evenly spaced positions for n stories in order.
func Normalize(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i+1) * Gap
	}
	return out
}
