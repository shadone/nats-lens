package checks

import (
	"testing"
	"time"
)

func upAll(i int, e int64) string { return "up" }

// meta leader flaps every epoch (30 s) for 10 minutes, no restarts.
func TestT3D_MetaFlapNoRestart(t *testing.T) {
	for _, withEvents := range []bool{true, false} {
		f := newFx(t)
		g := gen{n: 3, holders: []int{0, 1, 2}, total: 40, state: upAll}
		g.sleader = func(e int64) int { return 0 }
		g.mleader = func(e int64) int {
			if e < 10 {
				return 0
			}
			return int(e % 3)
		}
		g.run(f, nil)
		if withEvents {
			for e := int64(10); e <= 40; e++ {
				f.event(5000+e, at(e).Add(-5*time.Second), "meta.leader_elected", m{"source": "sys", "server_name": three[e%3]})
			}
		}
		for _, e := range []int64{12, 14, 20, 30, 40} {
			got := f.eval("system-003", e)
			t.Logf("events=%v e%d: %s", withEvents, e, rowsText(got))
			if e >= 20 && len(got) == 0 {
				t.Errorf("events=%v e%d: a leader moving every epoch without restarts is not reported", withEvents, e)
			}
		}
	}
}

// Flapping A<->B, with one real lame duck of A. Epochs irregular: every
// other epoch missing (60 s steps) and one 10 minute collector pause.
func TestT3D_MetaFlapIrregularEpochs(t *testing.T) {
	f := newFx(t)
	g := gen{n: 3, holders: []int{0, 1, 2}, total: 60, state: upAll}
	g.sleader = func(e int64) int { return 0 }
	// leader alternates nats-0 / nats-1 every epoch from 20 on
	g.mleader = func(e int64) int { return int(e % 2) }
	g.lameduck = map[int64][]int{29: {0}}
	g.run(f, nil)
	// remove odd epochs 21..39 and 41..59's data is kept; drop epoch rows to make gaps
	for e := int64(21); e <= 39; e += 2 {
		f.exec(`DELETE FROM server_stats WHERE epoch = ?`, e)
		f.exec(`DELETE FROM epochs WHERE epoch = ?`, e)
	}
	for _, e := range []int64{30, 40, 50, 60} {
		got := f.eval("system-003", e)
		t.Logf("e%d: %s", e, rowsText(got))
		if len(got) == 0 {
			t.Errorf("e%d: flapping with irregular epochs not reported", e)
		}
	}
}

// A single leader move after a real lame duck, but the collector paused 6
// minutes around it: the change is seen late.
func TestT3D_MetaOneHandoverCollectorPause(t *testing.T) {
	f := newFx(t)
	g := gen{n: 3, holders: []int{0, 1, 2}, total: 40, state: upAll}
	g.sleader = func(e int64) int { return 0 }
	g.mleader = func(e int64) int {
		if e < 20 {
			return 0
		}
		return 1
	}
	g.lameduck = map[int64][]int{18: {0}}
	g.run(f, nil)
	// pause: epochs 19..30 never collected
	for e := int64(19); e <= 30; e++ {
		f.exec(`DELETE FROM server_stats WHERE epoch = ?`, e)
		f.exec(`DELETE FROM epochs WHERE epoch = ?`, e)
	}
	if got := f.eval("system-003", 35); len(got) != 0 {
		t.Errorf("one hand-over seen late after a collector pause: %s", rowsText(got))
	}
	// four real handovers in a pause window each excused individually?
}

// Rolling restart where the lame duck lasts 2 min before the leader steps down and
// each node is drained 2.5 min apart: leader goes 0 -> 1 -> 2 -> 0 and every move
// is later than 2 epochs after the announcement.
func TestT3D_MetaRollingSlowStepDown(t *testing.T) {
	f := newFx(t)
	g := gen{n: 3, holders: []int{0, 1, 2}, total: 40, state: upAll}
	g.sleader = func(e int64) int { return 0 }
	g.mleader = func(e int64) int {
		switch {
		case e < 10:
			return 0
		case e < 20:
			return 1
		case e < 30:
			return 2
		}
		return 0
	}
	// announcements 5 epochs (2.5 min) before the move
	g.lameduck = map[int64][]int{4: {0}, 14: {1}, 24: {2}}
	g.run(f, nil)
	for _, e := range []int64{12, 22, 32, 40} {
		if got := f.eval("system-003", e); len(got) != 0 {
			t.Errorf("e%d: three slow hand-overs reported: %s", e, rowsText(got))
		}
	}
}
