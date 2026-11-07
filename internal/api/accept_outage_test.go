package api

import (
	"math"
	"testing"
	"time"
)

// A 5-hour batch cycle (60 MiB up and down) on a steady +10 MiB/day
// stream. Over a whole day the median of the week's differences damps it.
// After the collector was down for most of the last day, the few points
// left give no rate, so no review after the restart ends the acceptance.
func TestOutageOverMostOfTheDayDoesNotEndOnACycle(t *testing.T) {
	val := func(at time.Time) float64 {
		h := at.Sub(history).Hours()
		return 1000*mib + h/24*10*mib + 60*mib*math.Sin(2*math.Pi*h/5)
	}
	s := newSeries(val)
	acc := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	rec := WeeklyGrowth(s.points(acc, everyPoint), acc, AcceptGrowthBucket)
	if rec == nil {
		t.Fatal("no rate at acceptance")
	}
	t.Logf("recorded %.2f MiB/day", rec.PerDay/mib)
	// Full days: never ends.
	for h := 1; h < 7*24; h++ {
		at := acc.Add(time.Duration(h) * time.Hour)
		if g := WeeklyGrowth(s.points(at, everyPoint), at, AcceptGrowthBucket); g == nil || GrowthChanged(rec.PerDay, g.PerDay) {
			t.Fatalf("full data ends at +%dh: %v", h, g)
		}
	}
	// The collector is down for 22 hours and comes back.
	ended := 0
	for h := 30; h < 30+5*24; h++ {
		at := acc.Add(time.Duration(h) * time.Hour)
		down0, down1 := at.Add(-22*time.Hour), at.Add(-10*time.Minute)
		up := func(x time.Time) bool { return x.Before(down0) || x.After(down1) }
		g := WeeklyGrowth(s.points(at, up), at, AcceptGrowthBucket)
		if g != nil && GrowthChanged(rec.PerDay, g.PerDay) {
			if ended == 0 {
				t.Errorf("first review after a 22h outage at +%dh: %s", h, g.EndReason(rec.PerDay))
			}
			ended++
		}
	}
	if ended > 0 {
		t.Errorf("%d of %d restarts end the acceptance", ended, 5*24)
	}
}

// With the collector down for all but the last 40 minutes of the day, the
// one or two points left give no rate.
func TestFewPointsInTheDayGiveNoRate(t *testing.T) {
	val := func(at time.Time) float64 { return 1000*mib + at.Sub(history).Hours()/24*10*mib }
	s := newSeries(val)
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	up := func(x time.Time) bool { return x.Before(at.Add(-day)) || x.After(at.Add(-40*time.Minute)) }
	pts := s.points(at, up)
	n := 0
	for _, p := range pts {
		if p.T.After(at.Add(-day)) {
			n++
		}
	}
	if g := WeeklyGrowth(pts, at, AcceptGrowthBucket); g != nil {
		t.Errorf("%d point(s) in the last day give a rate %.2f MiB/day; want none", n, g.PerDay/mib)
	}
}

// Collected every 50 minutes, the bucket is 50 minutes and a week is not a
// whole number of buckets; a large daily pattern still cancels.
func TestFiftyMinuteIntervalCancelsDailyPattern(t *testing.T) {
	val := func(at time.Time) float64 {
		h := at.Sub(history).Hours()
		return 5000*mib + h/24*10*mib + 2000*mib*math.Sin(2*math.Pi*h/24)
	}
	read := func(t0 time.Time) *Growth {
		var s []Sample
		// samples on the 50-minute grid since 1970, as each bucket holds one
		for x := t0.Add(-AcceptGrowthWindow - 2*time.Hour).Truncate(50 * time.Minute); !x.After(t0); x = x.Add(50 * time.Minute) {
			in := false
			for _, sp := range AcceptGrowthSpans(t0) {
				if x.After(sp.From) && !x.After(sp.To) {
					in = true
				}
			}
			if in {
				s = append(s, Sample{T: x, V: val(x)})
			}
		}
		return WeeklyGrowth(s, t0, 50*time.Minute)
	}
	base := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 7*24; h++ {
		g := read(base.Add(time.Duration(h) * time.Hour))
		if g == nil || GrowthChanged(10*mib, g.PerDay) {
			t.Fatalf("+%dh: %v", h, g)
		}
	}
}
