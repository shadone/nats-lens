package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// jszWithNulls is a JSZ reply with a null entry in every list of pointers
// the writer reads: accounts, the meta group's replicas, a stream's and a
// consumer's cluster replicas, consumers, and a stream's sources, in its
// state and its configuration.
const jszWithNulls = `{
  "server_id": "NNULLS0", "streams": 1, "consumers": 1,
  "meta_cluster": {"name": "example-dev", "leader": "nats-0", "cluster_size": 2,
    "replicas": [null, {"name": "nats-1", "current": false, "offline": true}]},
  "account_details": [null, {"name": "APP", "id": "APP", "stream_detail": [{
    "name": "orders", "created": "2026-10-16T23:00:00Z",
    "cluster": {"name": "example-dev", "raft_group": "S-orders", "leader": "nats-0",
      "replicas": [null, {"name": "nats-1", "current": true, "peer": "p1"}]},
    "config": {"name": "orders", "storage": "file", "subjects": ["orders.>"], "sources": [null, {"name": "legacy"}]},
    "sources": [null, {"name": "legacy", "lag": 4}],
    "state": {"messages": 1},
    "consumer_detail": [null, {"stream_name": "orders", "name": "worker", "created": "2026-10-16T23:00:00Z",
      "config": {"durable_name": "worker"},
      "cluster": {"name": "example-dev", "raft_group": "C-worker", "leader": "nats-0", "replicas": [null]}}]
  }]}]
}`

// TestJSZNullEntriesAreSkipped writes an epoch from a JSZ reply with null
// list entries: it is stored without them, and the replica counts count// the replica rows written.
func TestJSZNullEntriesAreSkipped(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Jsz, envelope(t, "nats-0", "NNULLS0", json.RawMessage(jszWithNulls)))
	mustApply(t, snap, collect.Varz, envelope(t, "nats-0", "NNULLS0", server.Varz{ID: "NNULLS0", Name: "nats-0"}))
	snap.FinishedAt = t0
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM stream_replicas`, 1},
		{`SELECT count(*) FROM stream_stats WHERE replicas_reported = 1 AND replicas_current = 1 AND sources_count = 1 AND sources_max_lag = 4`, 1},
		{`SELECT count(*) FROM stream_config WHERE sources = ['legacy']`, 1},
		{`SELECT count(*) FROM consumer_replicas`, 0},
		{`SELECT count(*) FROM consumer_stats WHERE replicas_reported = 0`, 1},
		{`SELECT count(*) FROM server_stats WHERE meta_peers_offline = 1 AND meta_peers_not_current = 1`, 1},
	} {
		if got := count(t, db, c.q); got != c.want {
			t.Errorf("%s = %d, want %d", c.q, got, c.want)
		}
	}
}
