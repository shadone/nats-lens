package checks

import (
	"fmt"
	"testing"
	"time"
)

// routed stores servers names (ids R0, R1, ...) of cluster lens answering
// epochs 1 to n with ROUTEZ ok, each with a route to every other one,
// except the pairs cut maps to the epochs in which the first lists no
// route to the second.
func (f *fx) routed(n int64, names []string, cut map[[2]string][]int64) {
	f.t.Helper()
	f.epochs(1, n, names...)
	for i, name := range names {
		f.row("servers", m{"server_id": fmt.Sprintf("R%d", i), "server_name": name, "cluster": "lens",
			"start_time": t0.Add(-time.Hour), "first_epoch": 1, "last_epoch": n, "first_seen_at": at(1), "last_seen_at": at(n)})
	}
	for e := int64(1); e <= n; e++ {
		for i, name := range names {
			id := fmt.Sprintf("R%d", i)
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "ROUTEZ", "state": "ok"})
			for j, peer := range names {
				if i == j || containsEpoch(cut[[2]string{name, peer}], e) {
					continue
				}
				f.row("routes", m{"epoch": e, "server_id": id, "rid": int64(j), "remote_id": fmt.Sprintf("R%d", j),
					"remote_name": peer, "pending_bytes": 0})
			}
		}
	}
}

func containsEpoch(es []int64, e int64) bool {
	for _, x := range es {
		if x == e {
			return true
		}
	}
	return false
}

func span(from, to int64) []int64 {
	var out []int64
	for e := from; e <= to; e++ {
		out = append(out, e)
	}
	return out
}

// A route missing for min_epochs collections fires for the server that
// lacks it; the peer that lists its route does not.
func TestRouteMissing(t *testing.T) {
	f := newFx(t)
	f.routed(6, three, map[[2]string][]int64{{"nats-0", "nats-2"}: span(4, 6)})
	f.fires("server-017", 5)
	r := f.fires("server-017", 6, "nats-0")
	want(t, r["nats-0"], "warning", "nats-0 has no route to nats-2, while both answered.")
	if v := r["nats-0"].Value; v == nil || *v != 1 {
		t.Errorf("value %v", v)
	}
}

// A server with no route to any peer is critical.
func TestRouteMissingToEveryPeer(t *testing.T) {
	f := newFx(t)
	f.routed(3, three, map[[2]string][]int64{{"nats-1", "nats-0"}: span(1, 3), {"nats-1", "nats-2"}: span(1, 3)})
	r := f.fires("server-017", 3, "nats-1")
	want(t, r["nats-1"], "critical", "no route to any of the 2 other servers of cluster lens (nats-0, nats-2), while all answered.")
}

// A peer that did not answer is server-001's: no route to it is not a
// finding, and a collection without it breaks the run.
func TestRouteMissingPeerNotAnswering(t *testing.T) {
	f := newFx(t)
	f.routed(6, three, map[[2]string][]int64{{"nats-0", "nats-2"}: span(1, 6)})
	f.exec(`DELETE FROM epoch_endpoints WHERE epoch = 5 AND server_id = 'R2'`)
	f.fires("server-017", 4, "nats-0")
	f.fires("server-017", 5)
	f.fires("server-017", 6)
}

// A server whose ROUTEZ failed is not judged: its list is not whole.
func TestRouteMissingRoutezFailed(t *testing.T) {
	f := newFx(t)
	f.routed(4, three, nil)
	f.exec(`DELETE FROM routes WHERE server_id = 'R0'`)
	f.exec(`UPDATE epoch_endpoints SET state = 'missing' WHERE server_id = 'R0' AND epoch = 4`)
	f.fires("server-017", 3, "nats-0")
	f.fires("server-017", 4)
}

// A firing finding resolves in the first collection the route is back, and
// stays while it is missing even when the run is shorter than min_epochs.
func TestRouteMissingResolve(t *testing.T) {
	f := newFx(t)
	f.routed(8, three, map[[2]string][]int64{{"nats-0", "nats-2"}: {3, 4, 5, 7}})
	f.fires("server-017", 5, "nats-0")
	f.fires("server-017", 6)
	f.fires("server-017", 7)
	f.row("findings", m{"id": "server-017:nats-0", "check_code": "server-017", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(5), "since_epoch": 5,
		"transitioned_at": at(5), "last_seen_epoch": 6, "updated_epoch": 6, "detail": "x"})
	f.fires("server-017", 7, "nats-0")
	f.fires("server-017", 8)
}

// A restarted peer is matched by name, so the new process's route counts;
// servers of another cluster and those without a cluster are not peers.
func TestRouteMissingRestartAndClusters(t *testing.T) {
	f := newFx(t)
	f.routed(3, three, nil)
	// nats-2 restarted at epoch 3: its new process R9 answers, and nats-0's
	// route names it by name only.
	f.row("servers", m{"server_id": "R9", "server_name": "nats-2", "cluster": "lens", "start_time": at(3),
		"first_epoch": 3, "last_epoch": 3, "first_seen_at": at(3), "last_seen_at": at(3)})
	f.exec(`UPDATE servers SET last_epoch = 2 WHERE server_id = 'R2'`)
	f.exec(`UPDATE epoch_endpoints SET server_id = 'R9' WHERE server_id = 'R2' AND epoch = 3`)
	f.exec(`UPDATE routes SET server_id = 'R9' WHERE server_id = 'R2' AND epoch = 3`)
	f.exec(`UPDATE routes SET remote_id = 'R9' WHERE remote_id = 'R2' AND epoch = 3 AND server_id = 'R1'`)
	f.exec(`UPDATE routes SET remote_id = 'other' WHERE remote_id = 'R2' AND epoch = 3 AND server_id = 'R0'`)
	for e := int64(1); e <= 3; e++ {
		f.row("epoch_endpoints", m{"epoch": e, "server_id": "X0", "endpoint": "ROUTEZ", "state": "ok"})
		f.row("epoch_endpoints", m{"epoch": e, "server_id": "L0", "endpoint": "ROUTEZ", "state": "ok"})
	}
	f.row("servers", m{"server_id": "X0", "server_name": "edge-0", "cluster": "edge", "first_epoch": 1, "last_epoch": 3,
		"first_seen_at": at(1), "last_seen_at": at(3)})
	f.row("servers", m{"server_id": "L0", "server_name": "solo", "cluster": "", "first_epoch": 1, "last_epoch": 3,
		"first_seen_at": at(1), "last_seen_at": at(3)})
	f.fires("server-017", 3)
}

// The threshold is the number of collections: one fewer does not fire.
func TestRouteMissingThreshold(t *testing.T) {
	f := newFx(t)
	f.routed(5, three, map[[2]string][]int64{{"nats-0", "nats-2"}: span(4, 5)})
	if got := f.eval("server-017", 5, map[string]string{"min_epochs": "2"}); len(got) != 1 {
		t.Errorf("min_epochs 2: %s", rowsText(got))
	}
	f.fires("server-017", 5)
}
