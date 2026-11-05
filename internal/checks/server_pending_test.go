package checks

import (
	"testing"
)

const mib = 1 << 20

// pendingOn sets the bytes pending on server id's route to peer at epoch e.
func (f *fx) pendingOn(e int64, id, peer string, bytes int64) {
	f.t.Helper()
	f.exec(`UPDATE routes SET pending_bytes = ? WHERE epoch = ? AND server_id = ? AND remote_name = ?`, bytes, e, id, peer)
}

// limits stores max_pending 64 MiB for every server of routed from epoch
// 1 to n.
func (f *fx) limits(n int64, ids ...string) {
	for e := int64(1); e <= n; e++ {
		for _, id := range ids {
			f.stats(e, id, m{"max_pending": 64 * mib})
		}
	}
}

// Bytes at or above a quarter of max_pending in three collections in a row
// fire, at half of it critical; two collections, or bytes just below the
// level, do not.
func TestLinkPending(t *testing.T) {
	f := newFx(t)
	f.routed(6, three, nil)
	f.limits(6, "R0", "R1", "R2")
	for e := int64(4); e <= 6; e++ {
		f.pendingOn(e, "R0", "nats-2", 16*mib)
		f.pendingOn(e, "R1", "nats-2", 16*mib-1)
	}
	f.pendingOn(3, "R2", "nats-0", 40*mib)
	f.pendingOn(5, "R2", "nats-0", 40*mib)
	f.pendingOn(6, "R2", "nats-0", 40*mib)
	f.fires("server-018", 5)
	r := f.fires("server-018", 6, "nats-0")
	want(t, r["nats-0"], "warning", "nats-0: 16.0 MiB on the route to nats-2 (25%) waiting to be written, of max_pending 64.0 MiB.")
	f.pendingOn(4, "R2", "nats-0", 40*mib)
	r = f.fires("server-018", 6, "nats-0", "nats-2")
	want(t, r["nats-2"], "critical", "40.0 MiB on the route to nats-0 (62.5%)")
}

// Gateways count by direction and cluster; route pools by remote, the
// largest connection.
func TestLinkPendingGatewaysAndPools(t *testing.T) {
	f := newFx(t)
	f.routed(3, three, nil)
	f.limits(3, "R0", "R1", "R2")
	for e := int64(1); e <= 3; e++ {
		f.row("gateways", m{"epoch": e, "server_id": "R1", "gateway": "eu", "direction": "outbound", "cid": 7, "pending_bytes": 20 * mib})
		f.row("gateways", m{"epoch": e, "server_id": "R1", "gateway": "eu", "direction": "inbound", "cid": 8, "pending_bytes": 0})
		f.row("routes", m{"epoch": e, "server_id": "R1", "rid": 50, "remote_id": "R0", "remote_name": "nats-0", "pending_bytes": 30 * mib})
	}
	r := f.fires("server-018", 3, "nats-1")
	want(t, r["nats-1"], "warning", "nats-1: 30.0 MiB on the route to nats-0 (46.9%), 20.0 MiB on the outbound gateway to eu (31.3%) waiting")
}

// A collection whose VARZ failed keeps the process's max_pending; a
// process that never reported one is not judged; a restart keeps the run,
// as the link is the same.
func TestLinkPendingLimitSources(t *testing.T) {
	f := newFx(t)
	f.routed(3, three, nil)
	f.stats(1, "R0", m{"max_pending": 64 * mib})
	for e := int64(1); e <= 3; e++ {
		f.pendingOn(e, "R0", "nats-1", 20*mib)
		f.pendingOn(e, "R1", "nats-0", 20*mib)
	}
	f.fires("server-018", 3, "nats-0")
	// nats-0 restarts at epoch 3: process R7 reports the same link.
	f.row("servers", m{"server_id": "R7", "server_name": "nats-0", "cluster": "lens", "first_epoch": 3, "last_epoch": 3,
		"first_seen_at": at(3), "last_seen_at": at(3)})
	f.exec(`UPDATE routes SET server_id = 'R7' WHERE server_id = 'R0' AND epoch = 3`)
	f.stats(3, "R7", m{"max_pending": 64 * mib})
	f.fires("server-018", 3, "nats-0")
}

// A firing finding stays while the queue holds half the warning level and
// resolves below it.
func TestLinkPendingResolve(t *testing.T) {
	f := newFx(t)
	f.routed(5, three, nil)
	f.limits(5, "R0", "R1", "R2")
	f.pendingOn(4, "R0", "nats-1", 8*mib)
	f.pendingOn(5, "R0", "nats-1", 8*mib-1)
	f.fires("server-018", 4)
	f.row("findings", m{"id": "server-018:nats-0", "check_code": "server-018", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(2), "since_epoch": 2,
		"transitioned_at": at(2), "last_seen_epoch": 3, "updated_epoch": 3, "detail": "x"})
	r := f.fires("server-018", 4, "nats-0")
	want(t, r["nats-0"], "warning", "8.0 MiB on the route to nats-1")
	f.exec(`UPDATE findings SET last_seen_epoch = 4`)
	f.fires("server-018", 5)
}

// A collection in which the server's ROUTEZ did not answer breaks the run.
func TestLinkPendingGap(t *testing.T) {
	f := newFx(t)
	f.routed(4, three, nil)
	f.limits(4, "R0", "R1", "R2")
	for e := int64(1); e <= 4; e++ {
		f.pendingOn(e, "R0", "nats-1", 20*mib)
	}
	f.exec(`DELETE FROM routes WHERE server_id = 'R0' AND epoch = 3`)
	f.exec(`UPDATE epoch_endpoints SET state = 'missing' WHERE server_id = 'R0' AND epoch = 3`)
	f.fires("server-018", 2)
	f.fires("server-018", 4)
}
