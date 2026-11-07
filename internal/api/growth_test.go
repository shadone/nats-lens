package api

import (
	"math"
	"testing"
	"time"
)

func TestRobustPerDay(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	line := func(n int, step time.Duration, perHour float64) []Sample {
		out := make([]Sample, n)
		for i := range out {
			out[i] = Sample{T: t0.Add(time.Duration(i) * step), V: 1e6 + perHour*float64(i)*step.Hours()}
		}
		return out
	}
	// Three minutes with one write: never extrapolated to a day.
	short := line(6, 30*time.Second, 0)
	short[5].V += 120000
	if got := RobustPerDay(short); got != nil {
		t.Errorf("3 minutes of data gave %v, want unset", *got)
	}
	// Below six hours: unset.
	if got := RobustPerDay(line(5*60, time.Minute, 1000)); got != nil {
		t.Errorf("5 hours gave %v, want unset", *got)
	}
	// Seven hours of steady growth, 1000 bytes an hour, with one large
	// write and one purge: the slope is the steady one.
	s := line(7*60, time.Minute, 1000)
	s[100].V += 5e6
	s[300].V -= 5e5
	got := RobustPerDay(s)
	if got == nil || math.Abs(*got-24000) > 100 {
		t.Errorf("steady growth gave %v, want about 24000 a day", got)
	}
	// A bucket that shrinks under a TTL shrinks every hour: a real slope,
	// so negative growth is allowed.
	if got := RobustPerDay(line(8*60, time.Minute, -500)); got == nil || math.Abs(*got+12000) > 100 {
		t.Errorf("shrinking bucket gave %v, want about -12000 a day", got)
	}
	// Flat with two blips stays at zero.
	flat := line(7*60, time.Minute, 0)
	flat[10].V += 1000
	if got := RobustPerDay(flat); got == nil || *got != 0 {
		t.Errorf("flat gave %v, want 0", got)
	}
}

func TestGrowthObservedBounds(t *testing.T) {
	at := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	// SQL median timestamps are inside their actual observation bounds.
	points := []Sample{
		{T: at.Add(time.Minute), V: 60, From: at, To: at.Add(2 * time.Minute)},
		{T: at.Add(6*time.Hour - time.Minute), V: 21540, From: at.Add(6*time.Hour - 2*time.Minute), To: at.Add(6 * time.Hour)},
	}
	if got := RobustPerDay(points); got == nil || math.Abs(*got-86400) > 1 {
		t.Fatalf("exact six-hour observations rejected: %v", got)
	}
	points[1].To = at.Add(MinGrowthSpan - time.Nanosecond)
	if got := RobustPerDay(points); got != nil {
		t.Fatalf("short actual span accepted: %v", *got)
	}
	// Unreduced exact-boundary observations, including a gap, are valid.
	if got := RobustPerDay([]Sample{{T: at, V: 0}, {T: at.Add(MinGrowthSpan), V: 21600}}); got == nil || *got != 86400 {
		t.Fatalf("exact boundary rejected: %v", got)
	}
}

func TestGrowthLookbackBounds(t *testing.T) {
	end := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ span, interval, extra time.Duration }{
		{time.Hour, 30 * time.Second, 0}, {MinGrowthSpan - time.Nanosecond, time.Minute, 0},
		{MinGrowthSpan, 30 * time.Second, 30 * time.Second}, {7 * time.Hour, 10 * time.Minute, 5 * time.Minute},
		{24 * time.Hour, 0, 0}, {24 * time.Hour, -time.Second, 0},
	} {
		start := end.Add(-tc.span)
		if got := GrowthStart(start, end, tc.interval); !got.Equal(start.Add(-tc.extra)) {
			t.Errorf("%v / %v: start %v", tc.span, tc.interval, got)
		}
	}
}

func TestGrowthBucketReductionBound(t *testing.T) {
	at := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	var samples []Sample
	for i := 0; i <= 720; i++ {
		samples = append(samples, Sample{T: at.Add(time.Duration(i) * 30 * time.Second), V: float64(i)})
	}
	if got := len(bucketMedians(samples, GrowthBuckets)); got > GrowthBuckets {
		t.Fatalf("reduced to %d points, maximum %d", got, GrowthBuckets)
	}
	if got := RobustPerDay(samples); got == nil || math.Abs(*got-2880) > 1 {
		t.Fatalf("reduction lost exact-boundary slope: %v", got)
	}
}
