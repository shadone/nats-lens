package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

var t0 = time.Date(2026, 10, 16, 23, 17, 46, 0, time.UTC)

func openMem(t *testing.T) *Store {
	t.Helper()
	s, err := OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func envelope(t *testing.T, name, id string, data any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"server": server.ServerInfo{Name: name, ID: id, Cluster: "example-dev", Version: "2.15.0", JetStream: true},
		"data":   data,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustApply(t *testing.T, snap *collect.Snapshot, e collect.Endpoint, b []byte) {
	t.Helper()
	if err := snap.Apply(e, b); err != nil {
		t.Fatal(err)
	}
}

// fixtureSnapshot builds an epoch from the trimmed JSZ replies of
// nats-0 and nats-1 from a dev cluster, plus small hand-made replies for the other endpoints.
// nats-2 is expected and missing.
func fixtureSnapshot(t *testing.T, start time.Time) *collect.Snapshot {
	t.Helper()
	snap := collect.NewSnapshot(start)
	servers := []struct{ name, id string }{{"nats-0", "NFAKESERVER0ID"}, {"nats-1", "NFAKESERVER1ID"}}
	for i, s := range servers {
		jsz, err := os.ReadFile("../collect/testdata/jsz-nats-" + string(rune('0'+i)) + ".json")
		if err != nil {
			t.Fatal(err)
		}
		mustApply(t, snap, collect.Jsz, jsz)
		mustApply(t, snap, collect.Varz, envelope(t, s.name, s.id, server.Varz{
			ID: s.id, Name: s.name, Version: "2.15.0", Start: start.Add(-time.Hour), Mem: 1 << 28, CPU: 2.5,
			Connections: 2, Subscriptions: 40, InMsgs: 100, OutMsgs: 200, SystemAccount: "SYS",
		}))
		mustApply(t, snap, collect.Connz, envelope(t, s.name, s.id, server.Connz{
			ID: s.id, NumConns: 1, Total: 1, Conns: []*server.ConnInfo{{
				Cid: 7, Kind: "Client", Type: "nats", Name: "media-worker", Lang: "nats.js", Version: "3.0.0",
				Account: "APP", IP: "10.0.0.7", Port: 40000, Start: start.Add(-time.Minute), RTT: "1.5ms",
				Pending: 0, InMsgs: 3, OutMsgs: 4, NumSubs: 5,
			}},
		}))
		mustApply(t, snap, collect.Routez, envelope(t, s.name, s.id, server.Routez{
			ID: s.id, Name: s.name, NumRoutes: 1,
			Routes: []*server.RouteInfo{{Rid: 1, RemoteName: "nats-2", RemoteID: "NFAKESERVER2ID", RTT: "300µs"}},
		}))
		mustApply(t, snap, collect.Accountz, envelope(t, s.name, s.id, server.Accountz{
			ID: s.id, SystemAccount: "SYS", Accounts: []string{"SYS", "AFAKEAPPACCOUNT"},
		}))
		mustApply(t, snap, collect.Healthz, envelope(t, s.name, s.id, server.HealthStatus{Status: "ok", StatusCode: 200}))
		mustApply(t, snap, collect.Raftz, envelope(t, s.name, s.id, map[string]map[string]any{
			"SYS": {"_meta_": map[string]any{"id": "_meta_", "state": "FOLLOWER", "size": 3, "leader": "nats-1",
				"peers": map[string]any{"yrzKKRBu": map[string]any{"name": "nats-1", "known": true}}}},
		}))
		mustApply(t, snap, collect.Ipqueuesz, envelope(t, s.name, s.id, map[string]any{
			"/stream/APP/order-jobs/inflight": map[string]any{"pending": 3, "in_progress": 1},
		}))
	}
	snap.Expected = []string{"nats-0", "nats-1", "nats-2"}
	snap.FinishedAt = start.Add(250 * time.Millisecond)
	return snap
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}
