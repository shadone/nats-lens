package checks

import (
	"fmt"
	"testing"
	"time"
)

// lensCluster stores nats-0..2 (S0..S2) in cluster lens; epoch 2 has
// nats-1 missing.
func (f *fx) lensCluster() {
	f.epoch(1, three)
	f.epoch(2, three, "nats-1")
	for i, name := range three {
		id := fmt.Sprintf("S%d", i)
		last := int64(2)
		if name == "nats-1" {
			last = 1
		}
		f.server(id, name, 1, last, t0.Add(-time.Hour), "2.15.0")
		for e := int64(1); e <= last; e++ {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
		}
	}
	f.exec(`UPDATE servers SET cluster = 'lens'`)
}

// asset stores an asset's configuration run on server id and its identity.
func (f *fx) asset(id, stream string, first, last int64, cols m) {
	f.streamConfig(id, stream, first, last, cols)
	f.row("streams", m{"account": "APP", "stream": stream, "first_epoch": first, "last_epoch": last})
}

func TestSingleReplica(t *testing.T) {
	f := newFx(t)
	f.lensCluster()
	r1 := m{"replicas": 1}
	f.asset("S0", "order-cache", 1, 2, r1)
	f.asset("S0", "OBJ_image-input", 1, 2, r1)
	f.asset("S0", "KV_worker-presence", 1, 2, r1)
	f.asset("S0", "order-archive", 1, 2, merge(r1, m{"mirror": "order-events"}))
	f.asset("S0", "order-jobs", 1, 2, nil)
	// The only server of this one is missing in epoch 2.
	f.asset("S1", "media-cache", 1, 1, r1)
	// This one's server answers in epoch 2 without it: it is gone.
	f.asset("S2", "old-cache", 1, 1, r1)
	f.row("inventory", m{"account": "APP", "kind": "kv", "name": "worker-presence", "stream": "KV_worker-presence",
		"replicas": 1, "source": "inventory.yaml", "loaded_at": t0})
	r := f.fires("stream-018", 2, "APP/order-cache", "APP/OBJ_image-input", "APP/order-archive", "APP/media-cache")
	want(t, r["APP/order-cache"], "info",
		"Stream order-cache has 1 replica, on nats-0, in cluster lens of 3 JetStream servers; its messages are lost with that server's disk.")
	want(t, r["APP/OBJ_image-input"], "warning", "Object store image-input has 1 replica", "its objects are lost")
	want(t, r["APP/order-archive"], "info", "it mirrors order-events, which holds the data")
	want(t, r["APP/media-cache"], "info", "on nats-1", "(that server is not ready now)")
	if v := r["APP/order-cache"].Value; v == nil || *v != 3 {
		t.Errorf("value = %v", v)
	}
	if r := f.eval("stream-018", 2, map[string]string{"min_servers": "4"}); len(r) != 0 {
		t.Fatalf("min_servers 4: %s", rowsText(r))
	}
	// One server, or a server without a cluster: nothing to spread over.
	g := newFx(t)
	g.epoch(1, []string{"nats-0"})
	g.server("S0", "nats-0", 1, 1, t0.Add(-time.Hour), "2.15.0")
	g.asset("S0", "order-cache", 1, 1, r1)
	g.fires("stream-018", 1)
	g.exec(`UPDATE servers SET cluster = 'lens'`)
	g.fires("stream-018", 1)
	// The inventory may name the account by its readable name, and a
	// replicas other than 1 there leaves the difference to stream-012.
	f.row("accounts", m{"account": "ACC1", "account_name": "SHOP", "is_system": false, "first_epoch": 1, "last_epoch": 2})
	f.asset("S0", "order-tmp", 1, 2, r1)
	f.exec(`UPDATE stream_config SET account = 'ACC1' WHERE stream = 'order-tmp'`)
	f.exec(`UPDATE streams SET account = 'ACC1' WHERE stream = 'order-tmp'`)
	f.row("inventory", m{"account": "SHOP", "kind": "stream", "name": "order-tmp", "stream": "order-tmp",
		"replicas": 3, "source": "inventory.yaml", "loaded_at": t0})
	if r := f.eval("stream-018", 2); len(r) != 4 {
		t.Fatalf("declared under the readable name: %s", rowsText(r))
	}
}
