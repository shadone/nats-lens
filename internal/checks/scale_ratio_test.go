package checks

import (
	"context"
	"time"
)

// maxPasses is how many passes over the history a check in a scale test
// may take. Load on the machine slows the check and the pass alike, so the
// ratio holds where a fixed time limit failed on loaded runners, while a
// check that joins each entity's whole history takes well over it.
const maxPasses = 10

// scaleRatio times check against pass, a query that reads the history once
// and returns a count that must be want, and fails the test when the check
// takes more than maxPasses passes. Each is timed as the best of three
// runs, so a burst of load slows neither.
func (f *fx) scaleRatio(what, pass string, want int, check func()) {
	f.t.Helper()
	p := best(func() {
		var got int
		if err := f.st.DB().QueryRowContext(context.Background(), pass).Scan(&got); err != nil {
			f.t.Fatal(err)
		}
		if got != want {
			f.t.Fatalf("one pass counted %d, want %d", got, want)
		}
	})
	took := best(check)
	ratio := float64(took) / float64(p)
	f.t.Logf("%s: %s, one pass over the history %s (%.1f passes)", what, took, p, ratio)
	if ratio > maxPasses {
		f.t.Errorf("%s took %s, %.1f passes over the history (%s), want at most %d", what, took, ratio, p, maxPasses)
	}
}

// best runs fn three times and returns the shortest time.
func best(fn func()) time.Duration {
	var shortest time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		fn()
		if d := time.Since(start); i == 0 || d < shortest {
			shortest = d
		}
	}
	return shortest
}
