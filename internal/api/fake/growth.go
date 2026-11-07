package fake

import (
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// growth is a stream's or bucket's growth per day over the range, computed
// as the store provider computes it: the range cut into api.GrowthBuckets
// buckets of at least one collect interval, each bucket's bytes at the
// middle of the epochs it holds, and the Theil-Sen slope of those
// (api.RobustPerDay). It is unset when the data in the range spans less
// than six hours, as for a short range or a system that stopped collecting.
func (s scen) growth(e *v1.EntityRef, r *v1.TimeRange) *float64 {
	return s.growthOf("stream.bytes", e, r)
}

// growthOf is growth's rule for any metric of the scenario: a stream's
// messages, a server's file store.
func (s scen) growthOf(metric string, e *v1.EntityRef, r *v1.TimeRange) *float64 {
	growthStart := api.GrowthStart(r.GetStart().AsTime(), r.GetEnd().AsTime(), sampleEvery)
	start := latest(growthStart, s.dataStart())
	end := earliest(r.GetEnd().AsTime(), s.dataEnd())
	width := max(r.GetEnd().AsTime().Sub(growthStart)/api.GrowthBuckets, sampleEvery)
	var pts []api.Sample
	for b := api.BucketStart(start, width); b.Before(end); b = b.Add(width) {
		// Samples lie on the Unix-aligned sampleEvery grid of the series;
		// the epochs of freshness lie 4 seconds before it (see lastEpoch).
		first := api.BucketStart(latest(b, start).Add(sampleEvery-time.Nanosecond), sampleEvery)
		last := api.BucketStart(earliest(b.Add(width), end).Add(-time.Nanosecond), sampleEvery)
		if last.Before(first) {
			continue
		}
		mid := first.Add(last.Sub(first) / 2)
		if v, ok := s.sc.value(metric, e, s.clock(mid)); ok {
			pts = append(pts, api.Sample{T: mid, V: v, From: first, To: last})
		}
	}
	return api.RobustPerDay(pts)
}

// reviewGrowth is the growth the acceptance review reads at t
// (api.WeeklyGrowth), read as store.GrowthPoints and the runner read it:
// one epoch every sampleEvery on the series' grid, each epoch inside a span
// of api.AcceptGrowthSpans(t) (after its start, up to its end, so the last
// bucket is cut at t), and per bucket of api.AcceptGrowthWidth the median
// of its epochs' values at the middle of its first and last epoch.
func reviewGrowth(value func(c time.Time) (float64, bool), t time.Time) *api.Growth {
	width := api.AcceptGrowthWidth(sampleEvery)
	var pts []api.Sample
	for _, sp := range api.AcceptGrowthSpans(t) {
		var bucket time.Time
		var ts []time.Time
		var vs []float64
		flush := func() {
			if len(ts) > 0 {
				pts = append(pts, api.Sample{T: ts[0].Add(ts[len(ts)-1].Sub(ts[0]) / 2), V: median(sorted(vs))})
			}
			ts, vs = nil, nil
		}
		for c := api.BucketStart(sp.From, sampleEvery).Add(sampleEvery); !c.After(sp.To); c = c.Add(sampleEvery) {
			if b := api.BucketStart(c, width); !b.Equal(bucket) {
				flush()
				bucket = b
			}
			if v, ok := value(c); ok {
				ts = append(ts, c)
				vs = append(vs, v)
			}
		}
		flush()
	}
	return api.WeeklyGrowth(pts, t, width)
}

// sorted is a sorted copy of the values, for median.
func sorted(vs []float64) []float64 {
	s := slices.Clone(vs)
	slices.Sort(s)
	return s
}
