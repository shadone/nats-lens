package api

import (
	"hash/fnv"
	"math"
	"strings"
	"testing"
	"time"
)

const mib = 1 << 20

var history = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

const epochEvery = 30 * time.Second

// series reads a value as the store does for an acceptance: epochs 30
// seconds apart from history on, in buckets of AcceptGrowthBucket on the
// grid from 1970, each bucket's middle time and median value, over the
// spans AcceptGrowthSpans names. Whole buckets are kept between reads.
type series struct {
	val   func(time.Time) float64
	whole map[time.Time]Sample
}

func newSeries(val func(time.Time) float64) *series {
	return &series{val: val, whole: map[time.Time]Sample{}}
}

// bucket is the bucket starting at b over the epochs in (from, to].
func (s *series) bucket(b, from, to time.Time) (Sample, bool) {
	whole := b.After(from) && !b.Add(AcceptGrowthBucket).After(to)
	if whole {
		if x, ok := s.whole[b]; ok {
			return x, true
		}
	}
	var ts []time.Time
	var vs []float64
	for at := b; at.Before(b.Add(AcceptGrowthBucket)); at = at.Add(epochEvery) {
		if at.After(from) && !at.After(to) && !at.Before(history) {
			ts, vs = append(ts, at), append(vs, s.val(at))
		}
	}
	if len(ts) == 0 {
		return Sample{}, false
	}
	x := Sample{T: ts[0].Add(ts[len(ts)-1].Sub(ts[0]) / 2), V: median(vs)}
	if whole {
		s.whole[b] = x
	}
	return x, true
}

func (s *series) points(t time.Time, keep func(time.Time) bool) []Sample {
	var out []Sample
	for _, sp := range AcceptGrowthSpans(t) {
		for b := sp.From.Truncate(AcceptGrowthBucket); !b.After(sp.To); b = b.Add(AcceptGrowthBucket) {
			if x, ok := s.bucket(b, sp.From, sp.To); ok && keep(x.T) {
				out = append(out, x)
			}
		}
	}
	return out
}

func everyPoint(time.Time) bool { return true }

// at is the growth per day at t, or nil.
func (s *series) at(t time.Time) *float64 {
	if g := WeeklyGrowth(s.points(t, everyPoint), t); g != nil {
		return &g.PerDay
	}
	return nil
}

// spaced is val every 7 minutes over the 8 days and an hour before t: a
// grid that does not fall on the same times every day.
func spaced(val func(time.Time) float64, t time.Time) *float64 {
	var s []Sample
	for at := t.Add(-AcceptGrowthWindow - time.Hour).Add(time.Minute); !at.After(t); at = at.Add(7 * time.Minute) {
		if !at.Before(history) {
			s = append(s, Sample{T: at, V: val(at)})
		}
	}
	if g := WeeklyGrowth(s, t); g != nil {
		return &g.PerDay
	}
	return nil
}

// neverEnds fails when any pair of an acceptance hour and a later review
// hour, every step hours over a week from base, ends the acceptance, read
// from store buckets and from 7-minute points.
func neverEnds(t *testing.T, name string, val func(time.Time) float64, step int) {
	t.Helper()
	base := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	s := newSeries(val)
	readers := map[string]func(time.Time) *float64{"store buckets": s.at, "7-minute points": func(x time.Time) *float64 { return spaced(val, x) }}
	for rn, read := range readers {
		ends, tot := 0, 0
		for acc := 0; acc < 7*24; acc += step {
			rec := read(base.Add(time.Duration(acc) * time.Hour))
			if rec == nil {
				t.Fatalf("%s, %s: no rate at +%dh", name, rn, acc)
			}
			for rev := 1; rev < 7*24; rev += step {
				now := read(base.Add(time.Duration(acc+rev) * time.Hour))
				tot++
				if now == nil {
					t.Errorf("%s, %s: no rate at review +%dh", name, rn, acc+rev)
					continue
				}
				if GrowthChanged(*rec, *now) {
					if ends == 0 {
						t.Errorf("%s, %s: accepted at +%dh (%.2f MiB/day), ended at +%dh (%.2f MiB/day)",
							name, rn, acc, *rec/mib, acc+rev, *now/mib)
					}
					ends++
				}
			}
		}
		if ends > 0 {
			t.Errorf("%s, %s: %d of %d pairs end it", name, rn, ends, tot)
		}
	}
}

// Growth during business hours only (09-17 UTC, 3 MiB/h), the same every
// day.
func TestDailyPatternNotEnded(t *testing.T) {
	neverEnds(t, "daily pattern", func(at time.Time) float64 {
		d := at.Sub(history)
		h := (d % (24 * time.Hour)).Hours()
		in := 0.0
		if h > 9 {
			in = (min(h, 17) - 9) * 3 * mib
		}
		return float64(d/(24*time.Hour))*24*mib + in
	}, 5)
}

// Growth on weekdays only, 09-17 UTC at 3 MiB/h.
func TestWeekdayPatternNotEnded(t *testing.T) {
	neverEnds(t, "weekday pattern", func(at time.Time) float64 {
		v := 0.0
		for d := history; d.Before(at); d = d.Add(24 * time.Hour) {
			if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
				continue
			}
			s, e := d.Add(9*time.Hour), d.Add(17*time.Hour)
			if at.Before(e) {
				e = at
			}
			if e.After(s) {
				v += e.Sub(s).Hours() * 3 * mib
			}
		}
		return v
	}, 5)
}

// +10 MiB/day, cut back by 70 MiB every Sunday 03:00: a weekly sawtooth
// with no growth from one week to the next.
func TestWeeklySawtoothNotEnded(t *testing.T) {
	first := history.Add(3*24*time.Hour + 3*time.Hour) // history is a Thursday
	neverEnds(t, "weekly sawtooth", func(at time.Time) float64 {
		n := 0.0
		if !at.Before(first) {
			n = float64(int(at.Sub(first)/(7*24*time.Hour)) + 1)
		}
		return 1000*mib + at.Sub(history).Hours()/24*10*mib - n*70*mib
	}, 5)
}

// Steady +10 MiB/day with noise of up to 20 MiB at every epoch.
func TestNoiseNotEnded(t *testing.T) {
	neverEnds(t, "noise", func(at time.Time) float64 {
		h := fnv.New64a()
		_, _ = h.Write([]byte(at.Format(time.RFC3339)))
		noise := (float64(h.Sum64()%2001)/1000 - 1) * 20 * mib
		return 1000*mib + at.Sub(history).Hours()/24*10*mib + noise
	}, 7)
}

// The collector stopped for 6 hours: the growth is measured at every
// review before, during and after, and does not end the acceptance.
func TestCollectorGapNotEnded(t *testing.T) {
	down0 := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	down1 := down0.Add(6 * time.Hour)
	val := func(at time.Time) float64 { return 1000*mib + at.Sub(history).Hours()/24*10*mib }
	s := newSeries(val)
	up := func(x time.Time) bool { return x.Before(down0) || x.After(down1) }
	read := func(t time.Time) *Growth { return WeeklyGrowth(s.points(t, up), t) }
	rec := read(down0)
	for h := 1; h < 9*24; h += 3 {
		now := read(down0.Add(time.Duration(h) * time.Hour))
		if now == nil || GrowthChanged(rec.PerDay, now.PerDay) {
			t.Errorf("%dh after the stop: %v -> %v", h, rec.PerDay/mib, now)
		}
	}
}

// A 200 MiB purge on a steady +10 MiB/day stream changes the week's net
// growth, so it ends the acceptance within a day, and the end reason gives
// the week's growth and the day's change.
func TestPurgeEnds(t *testing.T) {
	purge := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	s := newSeries(func(at time.Time) float64 {
		v := 1000*mib + at.Sub(history).Hours()/24*10*mib
		if !at.Before(purge) {
			v -= 200 * mib
		}
		return v
	})
	rec := WeeklyGrowth(s.points(purge.Add(-time.Hour), everyPoint), purge.Add(-time.Hour))
	if rec == nil || math.Abs(rec.PerDay-10*mib) > 0.01*mib {
		t.Fatalf("recorded %v, want +10 MiB/day", rec)
	}
	for h := 1; h <= 24; h++ {
		at := purge.Add(time.Duration(h) * time.Hour)
		now := WeeklyGrowth(s.points(at, everyPoint), at)
		if !GrowthChanged(rec.PerDay, now.PerDay) {
			continue
		}
		if h < 6 {
			t.Errorf("ended %dh after the purge", h)
		}
		want := "growth changed from +10 MiB/day to -19 MiB/day over the last 7 days (-190 MiB in the last day)"
		if got := now.EndReason(rec.PerDay); got != want {
			t.Errorf("reason %q, want %q", got, want)
		}
		return
	}
	t.Error("a purge did not end the acceptance within a day")
}

// A lasting change from +10 MiB/day ends the acceptance; the reason gives
// the week's growth and the last day's bytes.
func TestLastingChangeEnds(t *testing.T) {
	change := time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC)
	for _, to := range []float64{100, 30, -10, 16} {
		s := newSeries(func(at time.Time) float64 {
			v := 1000*mib + at.Sub(history).Hours()/24*10*mib
			if at.After(change) {
				v += at.Sub(change).Hours() / 24 * (to - 10) * mib
			}
			return v
		})
		rec := WeeklyGrowth(s.points(change, everyPoint), change)
		h := 1
		var now *Growth
		for ; h <= 9*24; h++ {
			at := change.Add(time.Duration(h) * time.Hour)
			if now = WeeklyGrowth(s.points(at, everyPoint), at); GrowthChanged(rec.PerDay, now.PerDay) {
				break
			}
		}
		if h > 9*24 {
			t.Errorf("10 -> %v: never ended", to)
			continue
		}
		reason := now.EndReason(rec.PerDay)
		t.Logf("10 -> %v MiB/day: ended after %dh: %s", to, h, reason)
		// The week's growth passes the half-way mark when the new rate has
		// held for 7 days times 5 MiB over the change; the median over the
		// day adds up to a day.
		if most := 24 + int(7*24*5/math.Abs(to-10)); h < 6 || h > most {
			t.Errorf("10 -> %v: ended after %dh, want 6 to %d", to, h, most)
		}
		if !strings.HasPrefix(reason, "growth changed from +10 MiB/day to ") || !strings.HasSuffix(reason, " in the last day)") {
			t.Errorf("reason %q", reason)
		}
	}
	// The last day's bytes give the new rate once a day has passed.
	s := newSeries(func(at time.Time) float64 {
		v := at.Sub(history).Hours() / 24 * 10 * mib
		if at.After(change) {
			v += at.Sub(change).Hours() / 24 * 90 * mib
		}
		return v
	})
	at := change.Add(30 * time.Hour)
	if g := WeeklyGrowth(s.points(at, everyPoint), at); g.LastDay == nil || math.Abs(*g.LastDay-100*mib) > 2*mib {
		t.Errorf("last day after the change = %v, want 100 MiB", g.LastDay)
	}
}

// The week's growth needs two thirds of the last day to have data a week
// before, so about 8 days of history: under that, nothing is recorded,
// also for a young entity with a purge.
func TestWeeklyGrowthNeedsEightDays(t *testing.T) {
	start := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	purge := start.Add(10 * time.Hour)
	s := newSeries(func(at time.Time) float64 {
		v := 1000*mib + at.Sub(start).Hours()/24*10*mib
		if !at.Before(purge) {
			v -= 200 * mib
		}
		return v
	})
	young := func(x time.Time) bool { return !x.Before(start) }
	for _, h := range []int{25, 7 * 24, 7*24 + 7} {
		at := start.Add(time.Duration(h) * time.Hour)
		if g := WeeklyGrowth(s.points(at, young), at); g != nil {
			t.Errorf("%dh of data: %v MiB/day, want none", h, g.PerDay/mib)
		}
	}
	at := start.Add(8*24*time.Hour + time.Hour)
	if g := WeeklyGrowth(s.points(at, young), at); g == nil || math.Abs(g.PerDay-10*mib) > 0.01*mib {
		t.Errorf("8 days and an hour of data: %v, want +10 MiB/day", g)
	}
}

func TestGrowthEndReason(t *testing.T) {
	d := 4.5 * mib
	for _, c := range []struct {
		g    Growth
		want string
	}{
		{Growth{PerDay: 4 * mib}, "growth changed from +2.1 MiB/day to +4.0 MiB/day over the last 7 days"},
		{Growth{PerDay: 4 * mib, LastDay: &d}, "growth changed from +2.1 MiB/day to +4.0 MiB/day over the last 7 days (+4.5 MiB in the last day)"},
	} {
		if got := c.g.EndReason(2.1 * mib); got != c.want {
			t.Errorf("reason %q, want %q", got, c.want)
		}
	}
}
