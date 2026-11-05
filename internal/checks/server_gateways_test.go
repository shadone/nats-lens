package checks

import (
	"fmt"
	"testing"
)

// supercluster stores clusters us (us-0, us-1, ids U0, U1) and eu (eu-0,
// ids E0) answering epochs 1 to n with GATEWAYZ ok, each server with an
// outbound gateway to the other cluster, except in the epochs cut lists
// per server name.
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
		}
	}
}

// A server without the gateway its peers hold fires after min_epochs, at
// warning; a remote cluster no server reaches is critical.
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
	g.supercluster(5, map[string][]int64{"us-0": span(4, 5), "us-1": span(4, 5), "eu-0": span(4, 5)})
	r = g.fires("server-019", 5, "us-0", "us-1", "eu-0")
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

// A remote cluster is known for the lookback after the last outbound
// gateway to it; later its absence fires nothing.
func TestGatewayMissingLookback(t *testing.T) {
	f := newFx(t)
	f.supercluster(4, map[string][]int64{"us-0": span(2, 4), "us-1": span(2, 4)})
	f.fires("server-019", 4, "us-0", "us-1")
	if got := f.eval("server-019", 4, map[string]string{"lookback": "30s"}); len(got) != 0 {
		t.Errorf("lookback 30s: %s", rowsText(got))
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
