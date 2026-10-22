package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/micro"

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
			TLSCertNotAfter: start.Add(16 * 24 * time.Hour).Truncate(time.Second),
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
		mustApply(t, snap, collect.Subsz, envelope(t, s.name, s.id, fixtureSubsz(s.id, i)))
		mustApply(t, snap, collect.Ipqueuesz, envelope(t, s.name, s.id, map[string]any{
			"/stream/APP/order-jobs/inflight": map[string]any{"pending": 3, "in_progress": 1},
		}))
	}
	snap.AccountDetails = []collect.AccountDetails{
		{Account: "AFAKEAPPACCOUNT", LookedUpAt: start.Add(100 * time.Millisecond), ServerID: "NFAKESERVER0ID",
			Name: "APP", Imports: 1, Exports: 2, HasJWT: true, JWTIssuedAt: start.Add(-30 * 24 * time.Hour),
			JWTExpires: start.Add(12 * 24 * time.Hour), Limits: collect.AccountLimits{Connections: 2000, LeafNodes: -1,
				Subscriptions: -1, Data: -1, Payload: -1, JSDisk: 500 << 30, JSMemory: 1 << 30, JSStreams: -1, JSConsumers: -1}},
		{Account: "SYS", LookedUpAt: start.Add(100 * time.Millisecond), ServerID: "NFAKESERVER1ID", Name: "SYS", IsSystem: true},
	}
	snap.Micro = fixtureMicro(start)
	snap.Expected = []string{"nats-0", "nats-1", "nats-2"}
	snap.FinishedAt = start.Add(250 * time.Millisecond)
	return snap
}

// fixtureMicro is what two account connections asked. APP answered INFO
// and STATS: the service orders has two instances, each with the
// endpoints get and create; the second instance's create has a last
// error and custom stats. OPS may not publish to $SRV.STATS.
func fixtureMicro(start time.Time) []collect.MicroAccount {
	id := micro.ServiceIdentity{Name: "orders", Version: "1.4.2"}
	ids := []string{"ORD1", "ORD2"}
	app := collect.MicroAccount{Label: "APP", Account: "AFAKEAPPACCOUNT", Asked: []collect.MicroAsked{
		{Request: collect.MicroInfo, State: collect.MicroOK, Replies: 2},
		{Request: collect.MicroStats, State: collect.MicroOK, Replies: 2},
	}}
	for i, instance := range ids {
		id.ID = instance
		app.Infos = append(app.Infos, micro.Info{ServiceIdentity: id, Type: micro.InfoResponseType,
			Description: "Order intake", Endpoints: []micro.EndpointInfo{
				{Name: "create", Subject: "orders.create", QueueGroup: "q"},
				{Name: "get", Subject: "orders.get.*", QueueGroup: "q", Metadata: map[string]string{"cache": "on"}},
			}})
		create := &micro.EndpointStats{Name: "create", Subject: "orders.create", QueueGroup: "q",
			NumRequests: 10 * (i + 1), ProcessingTime: time.Duration(10*(i+1)) * time.Millisecond,
			AverageProcessingTime: time.Millisecond}
		if i == 1 {
			create.NumErrors, create.LastError = 1, "409 duplicate order id"
			create.Data = json.RawMessage(`{"retries":3}`)
		}
		app.Stats = append(app.Stats, micro.Stats{ServiceIdentity: id, Type: micro.StatsResponseType,
			Started: start.Add(-time.Duration(i+1) * time.Hour), Endpoints: []*micro.EndpointStats{create,
				{Name: "get", Subject: "orders.get.*", QueueGroup: "q", NumRequests: 100, ProcessingTime: 50 * time.Millisecond,
					AverageProcessingTime: 500 * time.Microsecond}}})
	}
	ops := collect.MicroAccount{Label: "OPS", Account: "OPS", Asked: []collect.MicroAsked{
		{Request: collect.MicroInfo, State: collect.MicroError,
			Error: `nats: permissions violation: Permissions Violation for Publish to "$SRV.INFO"`},
		{Request: collect.MicroStats, State: collect.MicroError,
			Error: `nats: permissions violation: Permissions Violation for Publish to "$SRV.STATS"`},
	}}
	return []collect.MicroAccount{app, ops}
}

// fixtureSubsz is a SUBSZ reply. nats-0 lists 5 of its 7 subscriptions:
// the media-worker connection (cid 7) holds two subscriptions on one
// subject and queue group, one plain and one inbox subscription, and the
// server's own JetStream client (cid 2, not in CONNZ) one. nats-1 lists
// its one subscription.
func fixtureSubsz(id string, i int) *server.Subsz {
	if i == 0 {
		return &server.Subsz{ID: id, SublistStats: &server.SublistStats{NumSubs: 40}, Total: 7, Limit: 5,
			Subs: []server.SubDetail{
				{Account: "APP", Subject: "media.resize.>", Queue: "media-worker", Sid: "1", Msgs: 3, Cid: 7},
				{Account: "APP", Subject: "media.resize.>", Queue: "media-worker", Sid: "2", Msgs: 4, Cid: 7},
				{Account: "APP", Subject: "media.status", Sid: "3", Msgs: 1, Cid: 7},
				{Account: "APP", Subject: "_INBOX.k3J9.*", Sid: "4", Cid: 7},
				{Account: "APP", Subject: "$JS.API.>", Sid: "5", Cid: 2},
			}}
	}
	return &server.Subsz{ID: id, SublistStats: &server.SublistStats{NumSubs: 40}, Total: 1, Limit: 5,
		Subs: []server.SubDetail{{Account: "APP", Subject: "media.status", Sid: "1", Cid: 7}}}
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}
