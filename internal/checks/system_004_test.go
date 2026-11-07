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

// JSZ's meta pending counts the JetStream API requests queued on the
// server, not meta group operations behind: it does not count.
func TestMetaGroupBehindIgnoresPending(t *testing.T) {
	f := newFx(t)
	f.metaCluster(16, func(e int64, i int) m { return m{"meta_pending": 5000} })
	f.fires("system-004", 16)
}

// A collection without the meta leader's own report (its JSZ timed out,
// or no server claimed the lead during an election) is unknown: it does
// not break the hold, so the finding still fires.
func TestMetaGroupBehindLeaderReportMissing(t *testing.T) {
	f := newFx(t)
	f.metaCluster(40, func(e int64, i int) m {
		if i == 0 {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	f.exec(`DELETE FROM server_stats WHERE server_id = 'S0' AND epoch IN (8, 20, 32)`)
	f.exec(`UPDATE server_stats SET meta_leader = '' WHERE epoch = 38`)
	f.fires("system-004", 19, "")
	r := f.fires("system-004", 40, "")
	want(t, r[""], "warning", "every collection for at least 5 minutes")
	// Nor does it count towards resolving: a firing finding whose recent
	// collections are all unknown stays.
	g := newFx(t)
	g.metaCluster(30, func(e int64, i int) m {
		if i == 0 && e <= 18 {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	g.exec(`DELETE FROM server_stats WHERE server_id = 'S0' AND epoch > 18`)
	g.firingSystem("system-004", 29)
	r = g.fires("system-004", 30, "")
	want(t, r[""], "warning", "was behind at 09:08:30 UTC")
}

// A server new to nats-lens joins and catches up the meta group: the
// collections within checks.restart_window of its first one do not count.
func TestMetaGroupBehindNewServerJoining(t *testing.T) {
	f := newFx(t)
	f.metaCluster(40, func(e int64, i int) m {
		if i == 0 && e >= 5 && e <= 24 {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	f.metaFourth(40, func(e int64) m { return m{"meta_cluster_size": 4, "meta_leader": "nats-0"} })
	f.exec(`UPDATE servers SET first_epoch = 5, first_seen_at = ? WHERE server_id = 'S3'`, at(5))
	f.exec(`DELETE FROM server_stats WHERE server_id = 'S3' AND epoch < 5`)
	for e := int64(5); e <= 40; e++ {
		f.fires("system-004", e)
	}
	// A server present when nats-lens started is no newcomer.
	g := newFx(t)
	g.metaCluster(20, func(e int64, i int) m {
		if i == 0 {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	g.fires("system-004", 20, "")
}

// Behind on both sides of a gap of three hours is not behind for every
// collection of 5 minutes: the hold needs a collection that started
// between min_duration and twice min_duration ago.
func TestMetaGroupBehindAcrossLongGap(t *testing.T) {
	f := newFx(t)
	f.metaCluster(5, func(e int64, i int) m {
		if i == 0 {
			return m{"meta_peers_not_current": 1}
		}
		return nil
	})
	f.epochAt(6, at(5).Add(3*time.Hour), three)
	for i, id := range []string{"S0", "S1", "S2"} {
		cols := m{"meta_cluster_size": 3, "meta_leader": "nats-0", "meta_peers_offline": 0, "meta_peers_not_current": 0}
		if i == 0 {
			cols["meta_peers_not_current"] = 1
		}
		f.stats(6, id, cols)
	}
	f.fires("system-004", 6)
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
		cols := m{"meta_cluster_size": 3, "meta_leader": "nats-0", "meta_peers_offline": 0, "meta_peers_not_current": 0}
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
		f.stats(e, "S0", m{"meta_peers_not_current": 1})
	}
	f.fires("system-004", 16)
}
