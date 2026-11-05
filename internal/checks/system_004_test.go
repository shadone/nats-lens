package checks

import (
	"testing"
	"time"
)

// restartAt replaces nats-1's process (S1) by a new one (S1b) from epoch
// e: the new process started at e's collection.
func (f *fx) restartAt(e int64) {
	f.t.Helper()
	f.exec(`UPDATE servers SET last_epoch = ? WHERE server_id = 'S1'`, e-1)
	f.server("S1b", "nats-1", e, 1000, at(e).Add(-5*time.Second), "2.15.0")
	f.exec(`UPDATE server_stats SET server_id = 'S1b' WHERE server_id = 'S1' AND epoch >= ?`, e)
}

// The meta leader sees a peer not current from epoch 5 on: at the default
// min_duration of 5 minutes (ten collections) the finding fires at epoch
// 15, the first whose window starts at epoch 5, and not at 14.
func TestMetaGroupBehindPeerNotCurrent(t *testing.T) {
	f := newFx(t)
	f.metaCluster(16, func(e int64, i int) m {
		if i == 0 && e >= 5 {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	f.fires("system-004", 14)
	r := f.fires("system-004", 15, "")
	want(t, r[""], "warning", "The JetStream meta group is behind: the meta leader nats-0 sees 1 of 2 peers not current",
		"every collection for at least 5 minutes")
	// The healthy cluster of another store fires nothing.
	g := newFx(t)
	g.metaCluster(16, nil)
	g.fires("system-004", 16)
}

// An offline peer is not current either; it is server-001's and
// system-002's, so it does not count as behind. A follower reports the
// other followers as not current, as it does not track them: only the
// leader's view counts.
func TestMetaGroupBehindLeavesOfflinePeersOut(t *testing.T) {
	f := newFx(t)
	f.metaCluster(16, func(e int64, i int) m {
		if i == 0 {
			return m{"meta_peers_not_current": 1, "meta_peers_offline": 1}
		}
		return m{"meta_peers_not_current": 1}
	})
	f.fires("system-004", 16)
}

// Pending meta operations above max_pending (100) count; 100 does not.
func TestMetaGroupBehindPending(t *testing.T) {
	f := newFx(t)
	f.metaCluster(16, func(e int64, i int) m {
		switch i {
		case 2:
			return m{"meta_pending": 340}
		case 1:
			return m{"meta_pending": 100}
		}
		return nil
	})
	r := f.fires("system-004", 16, "")
	want(t, r[""], "warning", "meta operations pending on nats-2 340 (more than 100)")
	if v := r[""].Value; v == nil || *v != 340 {
		t.Errorf("value %v, want 340", v)
	}
	g := newFx(t)
	g.metaCluster(16, func(e int64, i int) m { return m{"meta_pending": 100} })
	g.fires("system-004", 16)
}

// A server that restarts catches up the meta group first: the
// collections within checks.restart_window of its start do not count, so
// a rolling restart does not fire.
func TestMetaGroupBehindDuringRestart(t *testing.T) {
	f := newFx(t)
	f.metaCluster(30, func(e int64, i int) m {
		if i == 0 && e >= 8 && e <= 20 {
			return m{"meta_peers_not_current": 1}
		}
		if i == 1 && e >= 8 && e <= 20 {
			return m{"meta_pending": 5000}
		}
		return nil
	})
	f.restartAt(8)
	for e := int64(8); e <= 30; e++ {
		f.fires("system-004", e)
	}
}

// A gap in collection leaves fewer collections in the window, but the
// condition must still hold from a collection min_duration ago; a single
// collection after a long gap is not enough when the one before was fine.
func TestMetaGroupBehindAfterGap(t *testing.T) {
	f := newFx(t)
	f.metaCluster(5, nil)
	f.epochAt(6, at(5).Add(time.Hour), three)
	for i, id := range []string{"S0", "S1", "S2"} {
		f.row("epoch_endpoints", m{"epoch": 6, "server_id": id, "endpoint": "JSZ", "state": "ok"})
		cols := m{"meta_cluster_size": 3, "meta_leader": "nats-0", "meta_pending": 0, "meta_peers_offline": 0, "meta_peers_not_current": 0}
		if i == 0 {
			cols["meta_peers_not_current"] = 1
		}
		f.stats(6, id, cols)
	}
	f.fires("system-004", 6)
}

// A firing finding keeps firing while any collection of the last
// min_duration was behind, and resolves after min_duration without one;
// a finding that was not firing needs every collection.
func TestMetaGroupBehindResolves(t *testing.T) {
	f := newFx(t)
	f.metaCluster(40, func(e int64, i int) m {
		if i == 0 && (e <= 20 || e == 24) {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	f.fires("system-004", 30)
	f.firingSystem("system-004", 29)
	r := f.fires("system-004", 30, "")
	want(t, r[""], "warning", "was behind at 09:11:30 UTC", "resolves once no collection of the last 5 minutes is behind")
	f.fires("system-004", 35)
}

// A single server, or servers without a clustered meta group, have no
// meta group to fall behind.
func TestMetaGroupBehindSingleServer(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 16, "nats-0")
	f.server("S0", "nats-0", 1, 16, t0.Add(-time.Hour), "2.15.0")
	for e := int64(1); e <= 16; e++ {
		f.stats(e, "S0", m{"meta_pending": 500})
	}
	f.fires("system-004", 16)
}
