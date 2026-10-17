package api

// OffPlurality reports whether v differs from the value most of values
// hold. A nil value is unreported: it is neither compared nor counted. A
// tie for the most common value has no plurality and marks nothing.
func OffPlurality(v *int64, values []*int64) bool {
	if v == nil {
		return false
	}
	counts := map[int64]int{}
	for _, x := range values {
		if x != nil {
			counts[*x]++
		}
	}
	best, bestN, tie := int64(0), 0, false
	for x, n := range counts {
		switch {
		case n > bestN:
			best, bestN, tie = x, n, false
		case n == bestN:
			tie = true
		}
	}
	return !tie && *v != best
}
