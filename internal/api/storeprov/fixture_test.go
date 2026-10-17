package storeprov_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

// t0 is the first fixture epoch; the fixture holds three epochs 30
// seconds apart and the provider's clock stands 30 seconds after the last.
var t0 = time.Date(2026, 10, 16, 23, 17, 0, 0, time.UTC)

const account = "AFAKEAPPACCOUNT"

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

// snapshot builds epoch i in the way the store package's tests do: the
// trimmed JSZ replies of nats-0 and nats-1 from a dev cluster, and small hand-made
// replies for the other endpoints. nats-2 is expected and missing. Traffic
// counters grow by 300 messages per epoch per server, and nats-1 restarts
// before the third epoch with a new server ID and counters from zero.
func snapshot(t *testing.T, i int) *collect.Snapshot {
	t.Helper()
	start := t0.Add(time.Duration(i) * 30 * time.Second)
	snap := collect.NewSnapshot(start)
	apply := func(e collect.Endpoint, b []byte) {
		if err := snap.Apply(e, b); err != nil {
			t.Fatal(err)
		}
	}
	for n, name := range []string{"nats-0", "nats-1"} {
		id, began, in := "NFAKESERVER0ID", start.Add(-time.Hour), int64(100+300*i)
		if n == 1 {
			id = "NFAKESERVER1ID"
			if i == 2 {
				id, began, in = "NFAKESERVER1NEW", start.Add(-10*time.Second), 50
			}
		}
		jsz, err := os.ReadFile("../../collect/testdata/jsz-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(jsz, &m); err != nil {
			t.Fatal(err)
		}
		if id == "NFAKESERVER1NEW" {
			m["server"].(map[string]any)["id"] = id
			m["data"].(map[string]any)["server_id"] = id
		}
		// catalog-writer is led by nats-2, which never answers: both
		// rows are followers' rows, which report no backlog.
		for _, acc := range m["data"].(map[string]any)["account_details"].([]any) {
			for _, st := range acc.(map[string]any)["stream_detail"].([]any) {
				cons, _ := st.(map[string]any)["consumer_detail"].([]any)
				for _, c := range cons {
					if c := c.(map[string]any); c["name"] == "catalog-writer" {
						c["cluster"].(map[string]any)["leader"] = "nats-2"
					}
				}
			}
		}
		if jsz, err = json.Marshal(m); err != nil {
			t.Fatal(err)
		}
		apply(collect.Jsz, jsz)
		apply(collect.Varz, envelope(t, name, id, server.Varz{
			ID: id, Name: name, Version: "2.15.0", Start: began, Mem: 1 << 28, CPU: 2.5, Connections: 2,
			Subscriptions: 40, InMsgs: in, OutMsgs: 2 * in, InBytes: 1000 * in, OutBytes: 2000 * in, SlowConsumers: int64(i), SystemAccount: "SYS",
			JetStream: server.JetStreamVarz{Config: &server.JetStreamConfig{MaxStore: 10 << 30}, Stats: &server.JetStreamStats{Store: 1 << 30},
				Meta: &server.MetaClusterInfo{Leader: "nats-1"}},
		}))
		apply(collect.Connz, envelope(t, name, id, server.Connz{ID: id, NumConns: 2, Total: 2, Conns: []*server.ConnInfo{
			{Cid: 7, Name: "media-worker", Lang: "nats.js", Account: "APP", IP: "10.0.0.7", Port: 40000, Start: start.Add(-time.Minute), RTT: "1.5ms"},
			{Cid: 9, Name: "ledger-service", Lang: "go", Account: "APP", IP: "10.0.0.9", Port: 40001, Start: start.Add(-time.Minute), RTT: "2ms"},
		}}))
		apply(collect.Healthz, envelope(t, name, id, server.HealthStatus{Status: "ok", StatusCode: 200}))
	}
	snap.Expected = []string{"nats-0", "nats-1", "nats-2"}
	snap.FinishedAt = start.Add(250 * time.Millisecond)
	return snap
}

// fixtureStore writes the three epochs and a few events.
func fixtureStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for i := range 3 {
		if _, err := st.WriteSnapshot(ctx, snapshot(t, i)); err != nil {
			t.Fatal(err)
		}
	}
	ev := func(at time.Duration, source, kind string, f func(*events.Event)) events.Event {
		e := events.Event{Time: t0.Add(at), ReceivedAt: t0.Add(at + time.Millisecond), Source: source, Kind: kind,
			Summary: kind, Raw: json.RawMessage(`{"k":"` + kind + `"}`)}
		if f != nil {
			f(&e)
		}
		return e
	}
	evs := []events.Event{
		ev(5*time.Second, events.SourceCollector, "collector.started", nil),
		ev(20*time.Second, events.SourceJetStream, "consumer.deleted", func(e *events.Event) {
			e.Account, e.Stream, e.Consumer, e.ClientName, e.ServerName = "APP", "order-jobs", "resize-worker", "load-churn", "nats-0"
		}),
		ev(21*time.Second, events.SourceJetStream, "consumer.created", func(e *events.Event) {
			e.Account, e.Stream, e.Consumer, e.ClientName, e.ServerName = "APP", "order-jobs", "resize-worker", "load-churn", "nats-0"
		}),
		ev(40*time.Second, events.SourceSys, "client.disconnect", func(e *events.Event) {
			e.ClientName, e.Reason, e.ServerName, e.Account = "media-worker", "Slow Consumer (Write Deadline)", "nats-1", "APP"
			e.Raw = json.RawMessage(`{"client":{"id":7,"name":"media-worker"}}`)
		}),
		ev(50*time.Second, events.SourceSys, "server.shutdown", func(e *events.Event) { e.ServerName = "nats-1" }),
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	return st
}

// addEpochAt stores a bare epoch row, for check runs without snapshot data.
func addEpochAt(t *testing.T, st *store.Store, epoch int64, at time.Time) {
	t.Helper()
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`, epoch, at, at); err != nil {
		t.Fatal(err)
	}
}

// httpGet is http.Get, named for the tests' readability.
var httpGet = http.Get
