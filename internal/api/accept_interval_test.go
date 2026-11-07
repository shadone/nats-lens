package api

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// epochPoints is what the store gives for epochs every interval, each
// started within jit of the interval grid from 1970, of a stream growing
// +10 MiB/day: over AcceptGrowthSpans(t), buckets of width on the grid
// from 1970, each bucket's middle time and median value.
func epochPoints(t time.Time, interval, width, jit time.Duration, r *rand.Rand) []Sample {
	type bucket struct {
		ts []time.Time
		vs []float64
	}
	var out []Sample
	for _, sp := range AcceptGrowthSpans(t) {
		var keys []time.Time
		bs := map[time.Time]*bucket{}
		for at := sp.From.Truncate(interval).Add(-interval); !at.After(sp.To.Add(interval)); at = at.Add(interval) {
			e := at
			if jit > 0 {
				e = at.Add(time.Duration(r.Int63n(int64(2*jit))) - jit)
			}
			if !e.After(sp.From) || e.After(sp.To) {
				continue
			}
			k := e.Truncate(width)
			if bs[k] == nil {
				bs[k] = &bucket{}
				keys = append(keys, k)
			}
			bs[k].ts = append(bs[k].ts, e)
			bs[k].vs = append(bs[k].vs, e.Sub(history).Hours()/24*10*mib)
		}
		for _, k := range keys {
			b := bs[k]
			out = append(out, Sample{T: b.ts[0].Add(b.ts[len(b.ts)-1].Sub(b.ts[0]) / 2), V: median(b.vs)})
		}
	}
	return out
}

// With a collect interval of 48 minutes or more the bucket is the
// interval, and epochs within jitter of the bucket grid put two epochs in
// one bucket and none in the next. That loses no data, so the week's
// growth is still measured.
func TestJitterOnTheBucketGridKeepsTheRate(t *testing.T) {
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct{ interval, jit time.Duration }{
		{48 * time.Minute, 50 * time.Millisecond},
		{time.Hour, 50 * time.Millisecond},
		{2 * time.Hour, 50 * time.Millisecond},
		{30 * time.Minute, time.Second},
	} {
		for seed := int64(1); seed <= 50; seed++ {
			r := rand.New(rand.NewSource(seed))
			for h := 0; h < 24; h += 5 {
				end := at.Add(time.Duration(h) * time.Hour)
				width := AcceptGrowthWidth(c.interval)
				g := WeeklyGrowth(epochPoints(end, c.interval, width, c.jit, r), end, width)
				if g == nil {
					t.Errorf("%v ±%v, seed %d, +%dh: no rate", c.interval, c.jit, seed, h)
					continue
				}
				if g.PerDay < 9.5*mib || g.PerDay > 10.5*mib {
					t.Errorf("%v ±%v, seed %d, +%dh: %s", c.interval, c.jit, seed, h, fmt.Sprintf("%.2f MiB/day", g.PerDay/mib))
				}
			}
		}
	}
}
