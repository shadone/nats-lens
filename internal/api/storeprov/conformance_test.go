package storeprov_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/store"
)

// The conformance store holds 8 hours of epochs, 5 minutes apart, from
// confStart; the provider's clock stands 30 seconds after the last.
var confStart = time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC)

const (
	confEpochs = 97
	confStep   = 5 * time.Minute
)

func confAt(e int64) time.Time { return confStart.Add(time.Duration(e-1) * confStep) }

// confInventory lists a consumer and a KV bucket that no server reports.
const confInventory = `accounts:
  - name: APP
    streams:
      - name: orders
        consumers:
          - name: fast
          - name: auditor
    kv:
      - name: labels
`

// conformanceStore writes a store in which each state of the conformance
// table can be found. Its newest epoch missed nats-4. Streams:
//   - orders: nats-0 leads, nats-1 in sync, nats-2 behind; grows 1 MB
//     every epoch.
//   - payments: nats-0 leads, nats-1 has another configuration, nats-2
//     offline.
//   - returns: nats-1 leads, nats-2 listed and not reporting, nats-3 in
//     sync.
//   - wishlist: three replicas, none leads, each reporting another size.
//   - KV_sessions: R1 on nats-2, shrinking 100 kB every epoch.
//
// Consumers on orders: fast delivers 1 a second, planner is paused, fresh
// appeared in the newest epoch, auditor is expected and missing; settler
// on payments has another configuration and creation time on nats-1. Findings cover every
// state, stream-007 on payments is muted, and stream-005 on wishlist is
// accepted as by design.
func conformanceStore(t *testing.T) (*store.Store, *checks.Set) {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	ids := []string{"S0", "S1", "S2", "S3"}
	for i, id := range ids {
		r.add("servers", map[string]any{"server_id": id, "server_name": fmt.Sprintf("nats-%d", i), "first_epoch": 1,
			"last_epoch": confEpochs, "first_seen_at": confStart, "last_seen_at": confAt(confEpochs)})
	}
	for e := int64(1); e <= confEpochs; e++ {
		missing, partial := "[]", false
		if e == confEpochs {
			missing, partial = "['nats-4']", true
		}
		if _, err := st.DB().Exec(fmt.Sprintf(`INSERT INTO epochs VALUES (?, ?, ?, 0, 5, 4, [], [], %s, 0, ?, [], NULL)`, missing),
			e, confAt(e), confAt(e).Add(time.Second), partial); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			// Each answered VARZ too, as a server that answers JSZ does.
			r.add("server_stats", map[string]any{"epoch": e, "server_id": id, "meta_leader": "nats-0"})
		}
		// Growth: orders grows and KV_sessions shrinks over every epoch.
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "orders",
			"is_leader": true, "leader": "nats-0", "messages": 1000 * e, "bytes": 1_000_000_000 + 1_000_000*e})
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S2", "account": "APP", "stream": "KV_sessions",
			"is_leader": true, "leader": "nats-2", "messages": 5000 - e, "bytes": 50_000_000 - 100_000*e})
	}
	last := int64(confEpochs)
	// Each server has one connection in the newest epoch.
	for i, id := range ids {
		r.add("connections", map[string]any{"epoch": last, "server_id": id, "cid": int64(10 + i), "name": "order-service",
			"account": "APP", "ip": fmt.Sprintf("10.0.0.%d", 10+i)})
	}
	stream := func(name string) {
		r.add("streams", map[string]any{"account": "APP", "stream": name, "first_epoch": 1, "last_epoch": last})
	}
	configure := func(id, name, hash string, replicas int32) {
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": name, "first_epoch": 1,
			"last_epoch": last, "config_hash": hash, "replicas": replicas})
	}
	report := func(id, name, leader string, isLeader bool) {
		r.add("stream_stats", map[string]any{"epoch": last, "server_id": id, "account": "APP", "stream": name,
			"is_leader": isLeader, "leader": leader, "messages": 10, "bytes": 1000})
	}
	listed := func(leaderID, name, peer string, current, offline bool, lag int64) {
		r.add("stream_replicas", map[string]any{"epoch": last, "server_id": leaderID, "account": "APP", "stream": name,
			"peer_name": peer, "current": current, "offline": offline, "lag": lag})
	}

	stream("orders")
	for _, id := range []string{"S0", "S1", "S2"} {
		configure(id, "orders", "h", 3)
	}
	report("S1", "orders", "nats-0", false)
	report("S2", "orders", "nats-0", false)
	listed("S0", "orders", "nats-1", true, false, 0)
	listed("S0", "orders", "nats-2", false, false, 500)

	stream("payments")
	configure("S0", "payments", "h", 3)
	configure("S1", "payments", "other", 3)
	report("S0", "payments", "nats-0", true)
	report("S1", "payments", "nats-0", false)
	listed("S0", "payments", "nats-1", true, false, 0)
	listed("S0", "payments", "nats-2", false, true, 0)

	stream("returns")
	configure("S1", "returns", "h", 3)
	configure("S3", "returns", "h", 3)
	report("S1", "returns", "nats-1", true)
	report("S3", "returns", "nats-1", false)
	listed("S1", "returns", "nats-2", true, false, 0)
	listed("S1", "returns", "nats-3", true, false, 0)

	stream("wishlist")
	// No leader, and the replicas report different sizes: every page reads
	// the largest.
	for i, id := range []string{"S0", "S1", "S2"} {
		configure(id, "wishlist", "h", 3)
		r.add("stream_stats", map[string]any{"epoch": last, "server_id": id, "account": "APP", "stream": "wishlist",
			"is_leader": false, "leader": "", "messages": 10 + i, "bytes": 1000 * (i + 1)})
	}

	stream("KV_sessions")
	configure("S2", "KV_sessions", "h", 1)

	consumer := func(st, name string, hashes map[string]string) {
		r.add("consumers", map[string]any{"account": "APP", "stream": st, "consumer": name, "first_epoch": 1, "last_epoch": last})
		for id, h := range hashes {
			r.add("consumer_config", map[string]any{"server_id": id, "account": "APP", "stream": st, "consumer": name,
				"first_epoch": 1, "last_epoch": last, "config_hash": h, "durable_name": name})
		}
	}
	consumerStats := func(e int64, id, st, name string, isLeader bool, seq int64, paused bool) {
		r.add("consumer_stats", map[string]any{"epoch": e, "server_id": id, "account": "APP", "stream": st, "consumer": name,
			"is_leader": isLeader, "leader": "nats-0", "delivered_consumer_seq": seq, "num_pending": 3, "num_ack_pending": 1,
			"paused": paused})
	}
	consumer("orders", "fast", map[string]string{"S0": "c"})
	consumer("orders", "planner", map[string]string{"S0": "c"})
	consumer("orders", "fresh", map[string]string{"S0": "c"})
	consumer("payments", "settler", map[string]string{"S0": "c", "S1": "c2"})
	for e := last - 3; e <= last; e++ {
		consumerStats(e, "S0", "orders", "fast", true, int64(confAt(e).Sub(confStart).Seconds()), false)
		consumerStats(e, "S0", "orders", "planner", true, 400, true)
	}
	consumerStats(last, "S0", "orders", "fresh", true, 0, false)
	consumerStats(last, "S0", "payments", "settler", true, 50, false)
	consumerStats(last, "S1", "payments", "settler", false, 50, false)
	// nats-1 rebuilt its copy of settler: another creation time. The
	// leader lists it as current.
	if _, err := st.DB().Exec(`UPDATE consumer_stats SET created = CASE server_id WHEN 'S0' THEN TIMESTAMP '2026-10-01 08:00:00'
  ELSE TIMESTAMP '2026-10-15 09:30:00' END WHERE consumer = 'settler'`); err != nil {
		t.Fatal(err)
	}
	r.add("consumer_replicas", map[string]any{"epoch": last, "server_id": "S0", "account": "APP", "stream": "payments",
		"consumer": "settler", "peer_name": "nats-1", "current": true, "offline": false, "lag": 0})

	p := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(p, []byte(confInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, inv, p, confStart); err != nil {
		t.Fatal(err)
	}

	set, err := checks.Load(config.Checks{Timeout: config.Duration(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	// The findings of the last 14 epochs: stream-002 fires from 90 and
	// stream-007 with it; server-001 fires as info at 92 and changes to
	// warning at 94; consumer-002 fires and resolves every other epoch from
	// 86 to 94 (flapping); stream-001 fires at 85 and resolves at 87.
	for e := last - 13; e <= last; e++ {
		found := map[string][]store.FindingRow{}
		if e >= 90 {
			found["stream-002"] = []store.FindingRow{{EntityKey: "APP/payments", Severity: "warning", Detail: "Leader nats-0 reports 1 replica offline: nats-2."}}
			found["stream-007"] = []store.FindingRow{{EntityKey: "APP/payments", Severity: "warning", Detail: "2 configurations (nats-0 | nats-1); differing: max_age."}}
			found["stream-005"] = []store.FindingRow{{EntityKey: "APP/wishlist", Severity: "info", Detail: "wishlist has no limits."}}
		}
		if e >= 92 {
			sev := "info"
			if e >= 94 {
				sev = "warning"
			}
			found["server-001"] = []store.FindingRow{{EntityKey: "nats-4", Severity: sev, Detail: "nats-4 answered no monitoring request."}}
		}
		if e >= 86 && (e%2 == 0 || e > 94) {
			found["consumer-002"] = []store.FindingRow{{EntityKey: "APP/orders/fast", Severity: "warning", Detail: "Backlog growing."}}
		}
		if e == 85 || e == 86 {
			found["stream-001"] = []store.FindingRow{{EntityKey: "APP/wishlist", Severity: "critical", Detail: "No leader."}}
		}
		var runs []store.CheckRun
		for _, c := range set.Checks() {
			runs = append(runs, store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK, Rows: found[c.Code]})
		}
		if _, err := st.ApplyCheckRuns(ctx, e, runs); err != nil {
			t.Fatal(err)
		}
	}
	return st, set
}

// closedStore is a system whose collector's connection closed after its
// only epoch.
func closedStore(t *testing.T, clock time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, clock.Add(-time.Hour), "S0")
	if err := st.WriteEvents(ctx, []events.Event{{Time: clock.Add(-50 * time.Minute), ReceivedAt: clock.Add(-50 * time.Minute),
		Source: events.SourceCollector, Kind: "collector.connection_closed", Reason: "nats: authorization violation",
		Summary: "collector connection collector closed", Raw: json.RawMessage(`{"connection":"collector"}`)}}); err != nil {
		t.Fatal(err)
	}
	return st
}

// replayConformance is the replay fixture of replay_test.go with what
// the Replay rows read from the system's newest Kubernetes window: pod
// events of nats-1 and nats-2 that stretch the window from 09:02 to
// 09:22:10, and the assets of epoch 3 and 40 again at epochs 5 and 45,
// the collections read as A and B. Its clock is clock.
func replayConformance(t *testing.T, clock time.Time) *storeprov.Provider {
	t.Helper()
	p, r := replayStore(t)
	p.Clock = func() time.Time { return clock }
	for i, e := range []struct {
		at           time.Time
		kind, server string
	}{
		{rEpoch(12), "k8s.pod.started", "nats-1"},
		{rEpoch(20).Add(10 * time.Second), "k8s.pod.killing", "nats-2"},
		{rEpoch(45).Add(10 * time.Second), "k8s.pod.started", "nats-2"},
	} {
		r.add("events", map[string]any{"id": 500 + i, "time": e.at, "received_at": e.at, "source": "k8s", "kind": e.kind,
			"server_name": e.server, "summary": e.kind + " " + e.server})
	}
	copyStats := func(from, to int) {
		for _, table := range []string{"stream_stats", "consumer_stats"} {
			if _, err := p.St.DB().Exec(fmt.Sprintf(`INSERT INTO %[1]s BY NAME (SELECT * REPLACE (%[2]d AS epoch) FROM %[1]s WHERE epoch = %[3]d)`,
				table, to, from)); err != nil {
				t.Fatal(err)
			}
		}
	}
	copyStats(3, 5)
	// At B nats-1 holds what it held at epoch 40; nats-2 does not answer.
	copyStats(40, 45)
	return p
}

// The store provider shows every state of the conformance table; the fake
// runs the same table over the demo.
func TestStoreConformance(t *testing.T) {
	clock := confAt(confEpochs).Add(30 * time.Second)
	st, set := conformanceStore(t)
	at := func() time.Time { return clock }
	main := &storeprov.Provider{St: st, Interval: confStep, Live: true, Checks: set, Clock: at, Retention: 14 * 24 * time.Hour}
	closed := &storeprov.Provider{St: closedStore(t, clock), Interval: confStep, Live: true, Clock: at}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.Systems{
		{Ref: &v1.SystemRef{Id: "main", Name: "main"}, Provider: main},
		{Ref: &v1.SystemRef{Id: "closed", Name: "closed"}, Provider: closed},
		{Ref: &v1.SystemRef{Id: "replay", Name: "replay"}, Provider: replayConformance(t, clock)},
		{Ref: &v1.SystemRef{Id: "topology", Name: "topology"}, Provider: &storeprov.Provider{St: topologyStore(t, clock.Add(-30*time.Second)),
			Interval: 30 * time.Second, Live: true, Clock: at, Retention: 14 * 24 * time.Hour}},
		{Ref: &v1.SystemRef{Id: "topology-single", Name: "topology-single"}, Provider: &storeprov.Provider{St: topologySingle(t, clock.Add(-30*time.Second)),
			Interval: 30 * time.Second, Live: true, Clock: at, Retention: 14 * 24 * time.Hour}},
		{Ref: &v1.SystemRef{Id: "accounts", Name: "accounts"}, Provider: &storeprov.Provider{St: accountsStore(t, clock.Add(-30*time.Second), false),
			Interval: 30 * time.Second, Live: true, Clock: at, Retention: 14 * 24 * time.Hour}},
		{Ref: &v1.SystemRef{Id: "accounts-config", Name: "accounts-config"}, Provider: &storeprov.Provider{St: accountsConfigStore(t, clock.Add(-30*time.Second)),
			Interval: 30 * time.Second, Live: true, Clock: at, Retention: 14 * 24 * time.Hour}},
		{Ref: &v1.SystemRef{Id: "capacity", Name: "capacity"}, Provider: capacityProvider(t, clock.Add(-30*time.Second))},
	}})
	if _, err := c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{System: "main", CheckCode: "stream-007",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/payments"}, Reason: "Update in review.", MutedBy: "ops",
		ExpiresAt: timestamppb.New(clock.Add(24 * time.Hour))})); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{System: "main", CheckCode: "stream-005", Accept: true,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/wishlist"}, Reason: "Wishes are kept for the account's life.",
		MutedBy: "ops@example.com", ExpiresAt: timestamppb.New(clock.Add(180 * 24 * time.Hour))})); err != nil {
		t.Fatal(err)
	}
	apitest.RunConformance(t, apitest.Env{Client: c, Systems: []string{"main", "closed", "replay", "topology", "topology-single", "accounts", "accounts-config", "capacity"}, Now: clock})
}
