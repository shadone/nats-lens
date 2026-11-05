package checks

import (
	"fmt"
	"testing"
)

// supercluster stores clusters us (us-0, us-1, ids U0, U1) and eu (eu-0,
// ids E0) answering epochs 1 to n with GATEWAYZ ok, each server with an
// outbound gateway to the other cluster and the remote end's inbound
// gateway for it, except in the epochs cut lists per server name.
func (f *fx) supercluster(n int64, cut map[string][]int64) {
	f.t.Helper()
	names := []string{"us-0", "us-1", "eu-0"}
	f.epochs(1, n, names...)
	type srv struct{ id, name, cluster, remote string }
	servers := []srv{{"U0", "us-0", "us", "eu"}, {"U1", "us-1", "us", "eu"}, {"E0", "eu-0", "eu", "us"}}
	for _, s := range servers {
		f.row("servers", m{"server_id": s.id, "server_name": s.name, "cluster": s.cluster, "first_epoch": 1,
			"last_epoch": n, "first_seen_at": at(1), "last_seen_at": at(n)})
	}
	for e := int64(1); e <= n; e++ {
		for i, s := range servers {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": s.id, "endpoint": "GATEWAYZ", "state": "ok"})
			if containsEpoch(cut[s.name], e) {
				continue
			}
			f.row("gateways", m{"epoch": e, "server_id": s.id, "gateway": s.remote, "direction": "outbound",
				"cid": int64(100*i) + 1, "pending_bytes": 0, "ip": fmt.Sprintf("10.0.0.%d", i)})
			// The other end: eu-0 receives us-0's and us-1's, us-0 receives eu-0's.
			to := "E0"
			if s.cluster == "eu" {
				to = "U0"
			}
			f.row("gateways", m{"epoch": e, "server_id": to, "gateway": s.cluster, "direction": "inbound",
				"cid": int64(100*i) + 2, "pending_bytes": 0})
		}
	}
}

// A server without the gateway its peers hold fires after min_epochs, at
// warning; a remote cluster that still connects in but that no server of
// the cluster reaches is critical.
func TestGatewayMissing(t *testing.T) {
	f := newFx(t)
	f.supercluster(5, map[string][]int64{"us-0": span(4, 5)})
	f.fires("server-019", 4)
	r := f.fires("server-019", 5, "us-0")
	want(t, r["us-0"], "warning", "us-0 has no outbound gateway to cluster eu (1 of 2 servers of us have one).")
	if v := r["us-0"].Value; v == nil || *v != 1 {
		t.Errorf("value %v", v)
	}
	g := newFx(t)
	g.supercluster(5, map[string][]int64{"us-0": span(4, 5), "us-1": span(4, 5)})
	r = g.fires("server-019", 5, "us-0", "us-1")
	want(t, r["us-1"], "critical", "us-1 has no outbound gateway to cluster eu (no server of us has one).")
}

// A server whose GATEWAYZ did not answer is not judged, and a collection
// without its answer breaks the run.
func TestGatewayMissingNotAnswering(t *testing.T) {
	f := newFx(t)
	f.supercluster(5, map[string][]int64{"us-0": span(3, 5)})
	f.exec(`UPDATE epoch_endpoints SET state = 'error' WHERE server_id = 'U0' AND epoch = 4`)
	f.fires("server-019", 4)
	f.fires("server-019", 5)
	f.exec(`DELETE FROM epoch_endpoints WHERE server_id = 'U0' AND epoch = 5`)
	f.fires("server-019", 5)
}

// A remote cluster removed on purpose: no gateway in either direction for
// min_epochs collections, while the servers answer, makes it gone, so
// nothing fires.
func TestGatewayMissingRemovedCluster(t *testing.T) {
	f := newFx(t)
	f.supercluster(10, map[string][]int64{"us-0": span(6, 10), "us-1": span(6, 10), "eu-0": span(6, 10)})
	for e := int64(6); e <= 10; e++ {
		f.fires("server-019", e)
	}
	// A firing finding holds while the cluster's servers did not answer
	// in every one of the collections: the remote is not yet gone.
	for _, failed := range []bool{false, true} {
		g := newFx(t)
		g.supercluster(8, map[string][]int64{"us-0": span(6, 8), "us-1": span(6, 8), "eu-0": span(6, 8)})
		if failed {
			g.exec(`UPDATE epoch_endpoints SET state = 'error' WHERE server_id = 'U1' AND epoch = 7`)
			g.exec(`UPDATE epoch_endpoints SET state = 'error' WHERE server_id = 'U0' AND epoch = 7`)
		}
		g.row("findings", m{"id": "server-019:us-0", "check_code": "server-019", "entity_type": "server", "entity_key": "us-0",
			"severity": "critical", "state": "firing", "last_transition": "firing", "since": at(6), "since_epoch": 6,
			"transitioned_at": at(6), "last_seen_epoch": 7, "updated_epoch": 7, "detail": "x"})
		if failed {
			g.fires("server-019", 8, "us-0")
		} else {
			g.fires("server-019", 8)
		}
	}
}

// A firing finding stays while the gateway is missing and resolves when it
// is back; a restarted server is judged by name.
func TestGatewayMissingResolveAndRestart(t *testing.T) {
	f := newFx(t)
	f.supercluster(6, map[string][]int64{"us-0": {3, 5}})
	f.fires("server-019", 5)
	f.row("findings", m{"id": "server-019:us-0", "check_code": "server-019", "entity_type": "server", "entity_key": "us-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(3), "since_epoch": 3,
		"transitioned_at": at(3), "last_seen_epoch": 4, "updated_epoch": 4, "detail": "x"})
	f.fires("server-019", 5, "us-0")
	f.exec(`UPDATE findings SET last_seen_epoch = 5`)
	f.fires("server-019", 6)
	// us-1 restarts at epoch 6 as U9, with its gateway: nothing fires.
	f.row("servers", m{"server_id": "U9", "server_name": "us-1", "cluster": "us", "first_epoch": 6, "last_epoch": 6,
		"first_seen_at": at(6), "last_seen_at": at(6)})
	f.exec(`UPDATE epoch_endpoints SET server_id = 'U9' WHERE server_id = 'U1' AND epoch = 6`)
	f.exec(`UPDATE gateways SET server_id = 'U9' WHERE server_id = 'U1' AND epoch = 6`)
	f.fires("server-019", 6)
}

// A restarted process connects its gateways some time after it answers:
// it is not judged while it restarts (restart_window), and fires once past
// it if the gateway never came.
func TestGatewayMissingAfterRestart(t *testing.T) {
	f := newFx(t)
	f.supercluster(30, map[string][]int64{"us-0": span(5, 30)})
	f.row("servers", m{"server_id": "U9", "server_name": "us-0", "cluster": "us", "first_epoch": 5, "last_epoch": 30,
		"first_seen_at": at(5), "last_seen_at": at(30), "start_time": at(5)})
	f.exec(`UPDATE servers SET last_epoch = 4 WHERE server_id = 'U0'`)
	f.exec(`UPDATE epoch_endpoints SET server_id = 'U9' WHERE server_id = 'U0' AND epoch >= 5`)
	f.exec(`UPDATE gateways SET server_id = 'U9' WHERE server_id = 'U0' AND epoch >= 5`)
	for e := int64(5); e <= 30; e++ {
		f.stats(e, "U9", m{"start_time": at(5)})
	}
	f.fires("server-019", 6)
	f.fires("server-019", 25)
	// at(26) is 10.5 minutes after the start, past restart_window.
	f.fires("server-019", 27, "us-0")
}
