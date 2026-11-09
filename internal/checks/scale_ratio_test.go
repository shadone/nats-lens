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
// takes more than maxPasses passes. Each of five rounds times a pass, the
// check and another pass, and divides the check's time by the slower pass,
// so both sides of a round see the same load; the smallest ratio of the
// rounds counts. Passes timed apart from the checks can land in a quiet
// moment while the check does not.
func (f *fx) scaleRatio(what, pass string, want int, check func()) {
	f.t.Helper()
	once := func() time.Duration {
		start := time.Now()
		var got int
		if err := f.st.DB().QueryRowContext(context.Background(), pass).Scan(&got); err != nil {
			f.t.Fatal(err)
		}
		if got != want {
			f.t.Fatalf("one pass counted %d, want %d", got, want)
		}
		return time.Since(start)
	}
	best := -1.0
	var bestTook, bestPass time.Duration
	for round := 0; round < 5; round++ {
		before := once()
		start := time.Now()
		check()
		took := time.Since(start)
		p := max(before, once())
		if ratio := float64(took) / float64(p); best < 0 || ratio < best {
			best, bestTook, bestPass = ratio, took, p
		}
	}
	f.t.Logf("%s: %s, one pass over the history %s (%.1f passes)", what, bestTook, bestPass, best)
	if best > maxPasses {
		f.t.Errorf("%s took %s, %.1f passes over the history (%s), want at most %d", what, bestTook, best, bestPass, maxPasses)
	}
}
