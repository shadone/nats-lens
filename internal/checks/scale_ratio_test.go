package checks

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
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
// so both sides of a round see the same load; the median of the rounds'
// ratios counts. Passes timed apart from the checks can land in a quiet
// moment while the check does not, and the smallest ratio of the rounds
// can sit at half the typical one under load.
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
	type round struct {
		ratio      float64
		took, pass time.Duration
	}
	rounds := make([]round, 5)
	for i := range rounds {
		before := once()
		start := time.Now()
		check()
		took := time.Since(start)
		p := max(before, once())
		rounds[i] = round{float64(took) / float64(p), took, p}
	}
	slices.SortFunc(rounds, func(a, b round) int { return cmp.Compare(a.ratio, b.ratio) })
	ratios := make([]string, len(rounds))
	for i, r := range rounds {
		ratios[i] = fmt.Sprintf("%.1f", r.ratio)
	}
	m := rounds[len(rounds)/2]
	f.t.Logf("%s: median round %s, one pass over the history %s (%.1f passes; rounds %s)",
		what, m.took, m.pass, m.ratio, strings.Join(ratios, " "))
	if m.ratio > maxPasses {
		f.t.Errorf("%s took %s, a median of %.1f passes over the history (%s), want at most %d", what, m.took, m.ratio, m.pass, maxPasses)
	}
}
