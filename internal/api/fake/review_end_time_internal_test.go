package fake

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// runnerGrowth reads growth as store.GrowthPoints and the runner do,
// written apart from reviewGrowth so the two can be compared: one
// epoch every sampleEvery, bucket medians at the middle of each bucket's
// epochs, the last bucket cut at t.
func runnerGrowth(value func(time.Time) float64, t time.Time) *api.Growth {
	width := api.AcceptGrowthWidth(sampleEvery)
	var pts []api.Sample
	for _, sp := range api.AcceptGrowthSpans(t) {
		var bucket time.Time
		var ts []time.Time
		var vs []float64
		flush := func() {
			if len(ts) == 0 {
				return
			}
			mid := ts[0].Add(ts[len(ts)-1].Sub(ts[0]) / 2)
			med := vs[len(vs)/2]
			if len(vs)%2 == 0 {
				med = (vs[len(vs)/2-1] + vs[len(vs)/2]) / 2
			}
			pts = append(pts, api.Sample{T: mid, V: med})
			ts, vs = nil, nil
		}
		for c := api.BucketStart(sp.From, sampleEvery).Add(sampleEvery); !c.After(sp.To); c = c.Add(sampleEvery) {
			b := api.BucketStart(c, width)
			if !b.Equal(bucket) {
				flush()
				bucket = b
			}
			ts = append(ts, c)
			vs = append(vs, value(c)) // monotonic series: sorted
		}
		flush()
	}
	return api.WeeklyGrowth(pts, t, width)
}

// firstEnd is the first collection from 3 hours before the seeded end at
// which growth read by read ends the acceptance of KV_feature-flags.
func firstEnd(read func(at time.Time) *api.Growth) time.Time {
	for at := globalFlagsEnded.Add(-3 * time.Hour); at.Before(globalFlagsEnded.Add(3 * time.Hour)); at = at.Add(sampleEvery) {
		if g := read(at); g != nil && api.GrowthChanged(globalFlagsBefore, g.PerDay) {
			return at
		}
	}
	return time.Time{}
}

// The demo's review helper reads the series as the runner reads the store,
// so the seeded end of KV_feature-flags' acceptance is the collection at
// which the runner would end it, with the same end reason.
func TestDemoReviewEndsTheAcceptanceWhereTheRunnerWould(t *testing.T) {
	sp := streamSpec{bytes: globalFlagsNow, perDay: globalFlagsRate}
	val := func(c time.Time) float64 { return globalFlagsBytes(sp, c) }
	runner := firstEnd(func(at time.Time) *api.Growth { return runnerGrowth(val, at) })
	helper := firstEnd(func(at time.Time) *api.Growth {
		return reviewGrowth(func(c time.Time) (float64, bool) { return val(c), true }, at)
	})
	if !runner.Equal(globalFlagsEnded) || !helper.Equal(globalFlagsEnded) {
		t.Errorf("the runner ends it at %v, the demo's helper at %v, the demo seeds %v", runner, helper, globalFlagsEnded)
	}
	h := reviewGrowth(func(c time.Time) (float64, bool) { return val(c), true }, globalFlagsEnded)
	if r := runnerGrowth(val, globalFlagsEnded); h.EndReason(globalFlagsBefore) != r.EndReason(globalFlagsBefore) {
		t.Errorf("end reasons differ: demo %q, runner %q", h.EndReason(globalFlagsBefore), r.EndReason(globalFlagsBefore))
	}
}
