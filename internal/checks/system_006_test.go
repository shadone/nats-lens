package checks

import (
	"fmt"
	"testing"
	"time"
)

// upgradeAt replaces server i's process (Si) by a new one (Si-new) from
// epoch e, started at e's collection.
func (f *fx) upgradeAt(i int, e int64) {
	f.t.Helper()
	old, id := fmt.Sprintf("S%d", i), fmt.Sprintf("S%d-new", i)
	f.exec(`UPDATE servers SET last_epoch = ? WHERE server_id = ?`, e-1, old)
	f.server(id, fmt.Sprintf("nats-%d", i), e, 1000, at(e).Add(-5*time.Second), "2.16.0")
	f.exec(`UPDATE server_stats SET server_id = ? WHERE server_id = ? AND epoch >= ?`, id, old, e)
}

// One server reports another API level from the first collection: with
// the default min_duration of 30 minutes the finding fires at epoch 61,
// the first collection 30 minutes after epoch 1, and not at 60.
func TestMixedAPILevels(t *testing.T) {
	f := newFx(t)
	f.metaCluster(61, func(e int64, i int) m {
		if i == 2 {
			return m{"js_api_level": 2}
		}
		return nil
	})
	f.fires("system-006", 60)
	r := f.fires("system-006", 61, "")
	want(t, r[""], "warning", "The JetStream servers report 2 API levels: level 2 on nats-2; level 1 on nats-0, nats-1.",
		"mixed in every collection for at least 30 minutes")
	if v := r[""].Value; v == nil || *v != 2 {
		t.Errorf("value %v, want 2", v)
	}
	// Versions that differ with one API level do not fire.
	g := newFx(t)
	g.metaCluster(61, func(e int64, i int) m {
		if i == 2 {
			return m{"version": "2.15.1"}
		}
		return nil
	})
	g.fires("system-006", 61)
}

// A rolling upgrade restarts one server after the other onto the new
// level, here 25 minutes apart, so the levels are mixed for 40 minutes
// on end: every collection with a restarting server is left out, so it
// never fires. One that stops half-way fires min_duration after its last
// restart's restart_window.
func TestMixedAPILevelsRollingUpgrade(t *testing.T) {
	f := newFx(t)
	f.metaCluster(150, func(e int64, i int) m {
		if (i == 0 && e >= 10) || (i == 1 && e >= 60) || (i == 2 && e >= 110) {
			return m{"js_api_level": 2}
		}
		return nil
	})
	f.upgradeAt(0, 10)
	f.upgradeAt(1, 60)
	f.upgradeAt(2, 110)
	for e := int64(1); e <= 150; e++ {
		f.fires("system-006", e)
	}

	g := newFx(t)
	g.metaCluster(100, func(e int64, i int) m {
		if i == 1 && e >= 10 {
			return m{"js_api_level": 2}
		}
		return nil
	})
	g.upgradeAt(1, 10)
	// Restarting in epochs 10 to 29 (checks.restart_window, 10 minutes);
	// mixed from 30, so the first collection 30 minutes later is 90.
	g.fires("system-006", 89)
	r := g.fires("system-006", 90, "")
	want(t, r[""], "warning", "level 2 on nats-1; level 1 on nats-0, nats-2")
}

// A server before API levels reports 0, which reads as none reported.
// Servers with JetStream disabled are left out.
func TestMixedAPILevelsNoneReported(t *testing.T) {
	f := newFx(t)
	f.metaCluster(61, func(e int64, i int) m {
		switch i {
		case 0:
			return m{"js_api_level": 0}
		case 1:
			return m{"js_enabled": false, "js_api_level": 3}
		}
		return nil
	})
	r := f.fires("system-006", 61, "")
	want(t, r[""], "warning", "2 API levels: level 1 on nats-2; level 0 (none reported) on nats-0.")
}

// A firing finding stays while any collection of the last min_duration
// was mixed, and resolves after min_duration without one.
func TestMixedAPILevelsResolves(t *testing.T) {
	f := newFx(t)
	f.metaCluster(131, func(e int64, i int) m {
		if i == 2 && e <= 71 {
			return m{"js_api_level": 2}
		}
		return nil
	})
	f.firingSystem("system-006", 129)
	r := f.fires("system-006", 130, "")
	want(t, r[""], "warning", "At 09:35:00 UTC the JetStream servers reported 2 API levels", "resolves once")
	f.exec(`UPDATE findings SET last_seen_epoch = 130`)
	f.fires("system-006", 131)
}
