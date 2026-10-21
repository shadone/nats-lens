package fake

import (
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
	start := latest(r.GetStart().AsTime(), s.dataStart())
	end := earliest(r.GetEnd().AsTime(), s.dataEnd())
	width := max(r.GetEnd().AsTime().Sub(r.GetStart().AsTime())/api.GrowthBuckets, sampleEvery)
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
			pts = append(pts, api.Sample{T: mid, V: v})
		}
	}
	return api.RobustPerDay(pts)
}

// seriesGrowth is the growth per day of a series' points, nil when they
// span less than six hours.
func seriesGrowth(ser *v1.Series) *float64 {
	var pts []api.Sample
	for _, p := range ser.GetPoints() {
		pts = append(pts, api.Sample{T: p.GetTime().AsTime(), V: p.GetValue()})
	}
	return api.RobustPerDay(pts)
}

// reviewGrowth is the growth the acceptance review reads at t
// (api.WeeklyGrowth) from a series' values at fixed-frame times, sampled
// once per bucket of api.AcceptGrowthWidth over api.AcceptGrowthSpans(t).
func reviewGrowth(value func(c time.Time) (float64, bool), t time.Time) *api.Growth {
	width := api.AcceptGrowthWidth(sampleEvery)
	var pts []api.Sample
	for _, sp := range api.AcceptGrowthSpans(t) {
		for b := api.BucketStart(sp.From, width).Add(width); !b.After(sp.To); b = b.Add(width) {
			if v, ok := value(b); ok {
				pts = append(pts, api.Sample{T: b, V: v})
			}
		}
	}
	return api.WeeklyGrowth(pts, t, width)
}
