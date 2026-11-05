package checks

import (
	"testing"
	"time"
)

// metaFourth adds nats-3 (S3), answering epochs 1 to n with JetStream
// and the meta columns cols.
func (f *fx) metaFourth(n int64, cols func(e int64) m) {
	f.t.Helper()
	f.server("S3", "nats-3", 1, n, t0.Add(-time.Hour), "2.15.0")
	for e := int64(1); e <= n; e++ {
		f.stats(e, "S3", cols(e))
	}
}

// A fourth JetStream server answers but is no peer of the meta group of
// three: it fires after min_duration (10 minutes, twenty collections),
// first at epoch 25 when the condition starts at epoch 5.
func TestMetaGroupSizeDiffers(t *testing.T) {
	f := newFx(t)
	f.metaCluster(26, nil)
	f.metaFourth(26, func(e int64) m {
		if e < 5 {
			return m{"js_enabled": false}
		}
		return m{"meta_cluster_size": 1, "meta_leader": "nats-3"}
	})
	f.fires("system-005", 24)
	r := f.fires("system-005", 25, "")
	want(t, r[""], "warning", "The meta group has 3 peers in the view of its leader nats-0 and 4 JetStream servers answer: nats-3 reports a meta group of 1 led by nats-3.",
		"every collection for at least 10 minutes")
	g := newFx(t)
	g.metaCluster(26, nil)
	g.fires("system-005", 26)
}

// More servers answering than the leader counts fires even when every
// server reports the leader's size.
func TestMetaGroupSizeMoreServersThanPeers(t *testing.T) {
	f := newFx(t)
	f.metaCluster(26, func(e int64, i int) m { return m{"meta_cluster_size": 2} })
	r := f.fires("system-005", 26, "")
	want(t, r[""], "warning", "has 2 peers in the view of its leader nats-0 and 3 JetStream servers answer.")
}

// A server that does not answer is server-001's: the group of three with
// two answering does not fire. Nor does a server without a meta group
// (JetStream not clustered) or with JetStream disabled.
func TestMetaGroupSizeLeavesMissingServersOut(t *testing.T) {
	f := newFx(t)
	f.metaCluster(26, nil)
	f.exec(`DELETE FROM server_stats WHERE server_id = 'S2'`)
	f.metaFourth(26, func(e int64) m { return m{"js_enabled": e%2 == 0} })
	f.fires("system-005", 26)
}

// Servers are compared within their JetStream domain: a leaf node domain
// with a meta group of one is no fault of the hub's group.
func TestMetaGroupSizeByDomain(t *testing.T) {
	f := newFx(t)
	f.metaCluster(26, nil)
	f.metaFourth(26, func(e int64) m { return m{"meta_cluster_size": 1, "meta_leader": "nats-3"} })
	f.exec(`UPDATE servers SET domain = 'edge' WHERE server_id = 'S3'`)
	f.fires("system-005", 26)
	f.exec(`UPDATE servers SET domain = 'hub' WHERE server_id <> 'S3'`)
	f.exec(`UPDATE server_stats SET meta_cluster_size = 2 WHERE server_id = 'S1'`)
	r := f.fires("system-005", 26, "")
	want(t, r[""], "warning", "In JetStream domain hub, the meta group has 3 peers", "nats-1 reports a meta group of 2.")
}

// During a rolling restart the restarted server reports its meta group
// late; the collections within checks.restart_window of its start do not
// count.
func TestMetaGroupSizeDuringRestart(t *testing.T) {
	f := newFx(t)
	f.metaCluster(50, func(e int64, i int) m {
		if i == 1 && e >= 10 && e <= 30 {
			return m{"meta_cluster_size": 1, "meta_leader": ""}
		}
		return nil
	})
	f.restartAt(10)
	for e := int64(10); e <= 50; e++ {
		f.fires("system-005", e)
	}
}

// A firing finding resolves once no collection of the last min_duration
// differed.
func TestMetaGroupSizeResolves(t *testing.T) {
	f := newFx(t)
	f.metaCluster(60, func(e int64, i int) m {
		if i == 2 && e <= 31 {
			return m{"meta_cluster_size": 4}
		}
		return nil
	})
	f.firingSystem("system-005", 49)
	r := f.fires("system-005", 50, "")
	want(t, r[""], "warning", "At 09:15:00 UTC: The meta group has 3 peers", "nats-2 reports a meta group of 4.",
		"resolves once no collection of the last 10 minutes differs")
	f.exec(`UPDATE findings SET last_seen_epoch = 50`)
	f.fires("system-005", 51)
}

// A collection in which no server reports itself as meta leader is
// unknown: it does not break the hold.
func TestMetaGroupSizeLeaderReportMissing(t *testing.T) {
	f := newFx(t)
	f.metaCluster(26, func(e int64, i int) m { return m{"meta_cluster_size": 2} })
	f.exec(`UPDATE server_stats SET meta_leader = '' WHERE epoch IN (10, 18)`)
	f.exec(`DELETE FROM server_stats WHERE server_id = 'S0' AND epoch = 22`)
	r := f.fires("system-005", 26, "")
	want(t, r[""], "warning", "every collection for at least 10 minutes")
}

// Differing on both sides of a gap of three hours does not count as
// held.
func TestMetaGroupSizeAcrossLongGap(t *testing.T) {
	f := newFx(t)
	f.metaCluster(5, func(e int64, i int) m { return m{"meta_cluster_size": 2} })
	f.epochAt(6, at(5).Add(3*time.Hour), three)
	for _, id := range []string{"S0", "S1", "S2"} {
		f.stats(6, id, m{"meta_cluster_size": 2, "meta_leader": "nats-0"})
	}
	f.fires("system-005", 6)
}

// A server new to nats-lens joins: for restart_window its own report may
// lag, and those collections do not count.
func TestMetaGroupSizeNewServerJoining(t *testing.T) {
	f := newFx(t)
	f.metaCluster(50, nil)
	f.metaFourth(50, func(e int64) m {
		if e < 25 {
			return m{"meta_cluster_size": 1, "meta_leader": "nats-3"}
		}
		return m{"meta_cluster_size": 4, "meta_leader": "nats-0"}
	})
	f.exec(`UPDATE server_stats SET meta_cluster_size = 4 WHERE server_id <> 'S3' AND epoch >= 25`)
	f.exec(`UPDATE servers SET first_epoch = 5, first_seen_at = ? WHERE server_id = 'S3'`, at(5))
	f.exec(`DELETE FROM server_stats WHERE server_id = 'S3' AND epoch < 5`)
	// With min_duration at 5 minutes, the 10 minutes of the join would fire.
	for e := int64(5); e <= 50; e++ {
		if r := f.eval("system-005", e, map[string]string{"min_duration": "5m"}); len(r) != 0 {
			t.Fatalf("epoch %d: %s", e, rowsText(r))
		}
	}
}
