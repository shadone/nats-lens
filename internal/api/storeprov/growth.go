package storeprov

import (
	"slices"
	"time"
)

// minGrowthSpan is the shortest span of observations a growth per day is
// computed from. Shorter spans give a slope that one write or one purge
// decides, extrapolated to a day.
const minGrowthSpan = 6 * time.Hour

// growthBuckets is the most points the slope is computed over; longer
// series are reduced to bucket medians first.
const growthBuckets = 200

type sample struct {
	t time.Time
	v float64
}

// robustPerDay is the Theil-Sen slope of the samples (the median of the
// slopes between every pair), in units per day, or nil when the samples
// span less than minGrowthSpan. One large write or purge moves the median
// little, so a single step is not taken for a trend. The slope may be
// negative: a bucket whose entries expire by TTL faster than they are
// written shrinks for real.
func robustPerDay(s []sample) *float64 {
	if len(s) < 2 || s[len(s)-1].t.Sub(s[0].t) < minGrowthSpan {
		return nil
	}
	s = bucketMedians(s, growthBuckets)
	slopes := make([]float64, 0, len(s)*(len(s)-1)/2)
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if dt := s[j].t.Sub(s[i].t).Hours() / 24; dt > 0 {
				slopes = append(slopes, (s[j].v-s[i].v)/dt)
			}
		}
	}
	if len(slopes) == 0 {
		return nil
	}
	m := median(slopes)
	return &m
}

// bucketMedians reduces time-ordered samples to at most n, each the
// median time and value of an equal share of the span.
func bucketMedians(s []sample, n int) []sample {
	if len(s) <= n {
		return s
	}
	first, width := s[0].t, s[len(s)-1].t.Sub(s[0].t)/time.Duration(n)
	var out []sample
	for i := 0; i < len(s); {
		b := int(s[i].t.Sub(first) / max(width, 1))
		j := i
		var ts []time.Time
		var vs []float64
		for ; j < len(s) && int(s[j].t.Sub(first)/max(width, 1)) == b; j++ {
			ts, vs = append(ts, s[j].t), append(vs, s[j].v)
		}
		out = append(out, sample{t: ts[len(ts)/2], v: median(vs)})
		i = j
	}
	return out
}

func median(xs []float64) float64 {
	c := slices.Clone(xs)
	slices.Sort(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}
