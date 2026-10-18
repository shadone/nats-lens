package checks

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// churnEvent stores a client.churn summary received at when, counting
// connects pairs, with the connects per server in raw.servers. server_name
// is set only when all of them were on one server, as the collector does.
func (f *fx) churnEvent(id int64, when time.Time, name, host string, connects int, servers map[string]int) {
	f.t.Helper()
	srv := ""
	if len(servers) == 1 {
		for s := range servers {
			srv = s
		}
	}
	raw, err := json.Marshal(map[string]any{"client_name": name, "client_host": host, "connects": connects,
		"disconnects": connects, "servers": servers})
	if err != nil {
		f.t.Fatal(err)
	}
	f.event(id, when, "client.churn", m{"source": "sys", "client_name": name, "client_host": host, "server_name": srv,
		"reason": "Client Closed", "raw": string(raw)})
}

// A name whose folded connects within the lookback reach the threshold
// fires, with the count, the rate, its hosts and servers; one below it
// does not.
func TestShortLivedConnections(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	// order-sync: 12 a minute for the 10 minutes, on two servers: 120.
	for i := range 10 {
		f.churnEvent(int64(i+1), at(22+2*int64(i)), "order-sync", "10.0.0.5", 12, map[string]int{"nats-0": 7, "nats-1": 5})
	}
	// stock-poller: 19 in the lookback, one short of the threshold.
	f.churnEvent(20, at(30), "stock-poller", "10.0.0.6", 10, map[string]int{"nats-2": 10})
	f.churnEvent(21, at(32), "stock-poller", "10.0.0.6", 9, map[string]int{"nats-2": 9})
	r := f.fires("client-005", 40, "order-sync")
	want(t, r["order-sync"], "warning",
		"120 connections closed by the client within seconds in the last 10m (about 12 a minute) from 10.0.0.5 (nats-0, nats-1).")
	if got := r["order-sync"]; got.Value == nil || *got.Value != 120 || got.CauseEventID != 10 {
		t.Errorf("value %v, cause %d; want 120 and the latest summary, 10", got.Value, got.CauseEventID)
	}
	// The threshold is tunable.
	if got := f.eval("client-005", 40, map[string]string{"min_short_connections": "19"}); len(got) != 2 {
		t.Errorf("with min_short_connections 19: %v", rowsText(got))
	}
}

// Summaries received before the lookback do not count, whatever their
// size; nor does one received at the window's start.
func TestShortLivedConnectionsOnlyInLookback(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.churnEvent(1, at(5), "report-builder", "10.0.0.7", 300, map[string]int{"nats-0": 300})
	f.churnEvent(2, at(20), "report-builder", "10.0.0.7", 40, map[string]int{"nats-0": 40})
	f.churnEvent(3, at(35), "report-builder", "10.0.0.7", 5, map[string]int{"nats-0": 5})
	f.fires("client-005", 40)
	// The same summaries in the lookback of a later epoch fire.
	g := newFx(t)
	g.cluster(40)
	g.churnEvent(2, at(21), "report-builder", "10.0.0.7", 40, map[string]int{"nats-0": 40})
	r := g.fires("client-005", 40, "report-builder")
	want(t, r["report-builder"], "warning", "40 connections", "(about 4 a minute) from 10.0.0.7 (nats-0).")
}

// Tooling (nats-lens's own connections, and a CLI on a loopback address)
// is left out; a client without a name is grouped as (unnamed), its hosts
// listed, and a summary without raw.servers names its server_name.
func TestShortLivedConnectionsToolingAndUnnamed(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.churnEvent(1, at(30), "natslens-events", "10.0.0.1", 50, map[string]int{"nats-0": 50})
	f.churnEvent(2, at(30), "NATS CLI Version 0.5.0", "127.0.0.1", 50, map[string]int{"nats-0": 50})
	f.churnEvent(3, at(30), "", "10.0.0.9", 15, map[string]int{"nats-1": 15})
	f.event(4, at(31), "client.churn", m{"source": "sys", "client_host": "10.0.0.8", "server_name": "nats-2",
		"reason": "Client Closed", "raw": `{"connects": 15}`})
	r := evalTooling(t, f, "client-005", 40, config.Checks{})
	if len(r) != 1 {
		t.Fatalf("client-005 returned %v, want (unnamed) only", rowsText(r))
	}
	want(t, r["(unnamed)"], "warning", "30 connections", "from 10.0.0.8, 10.0.0.9 (nats-1, nats-2).")
	if got := evalTooling(t, f, "client-005", 40, config.Checks{IncludeTooling: true}); len(got) != 3 {
		t.Errorf("with tooling: %v", rowsText(got))
	}
}

// Many instances under one name: the detail lists five hosts and counts
// the rest.
func TestShortLivedConnectionsManyHosts(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i := range 8 {
		f.churnEvent(int64(i+1), at(30), "catalog-api", "10.0.1."+string(rune('1'+i)), 3, map[string]int{"nats-0": 3})
	}
	r := f.fires("client-005", 40, "catalog-api")
	want(t, r["catalog-api"], "warning",
		"24 connections", "from 10.0.1.1, 10.0.1.2, 10.0.1.3, 10.0.1.4, 10.0.1.5 and 3 more hosts (nats-0).")
}
