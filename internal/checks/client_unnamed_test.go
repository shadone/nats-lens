package checks

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// unnamedConn stores a client connection of epoch that started age before
// the epoch.
func (f *fx) unnamedConn(epoch int64, id string, cid int64, name any, ip string, age time.Duration, cols m) {
	f.t.Helper()
	f.row("connections", merge(m{"epoch": epoch, "server_id": id, "cid": cid, "name": name, "ip": ip,
		"start_time": at(epoch).Add(-age)}, cols))
}

// Connections without a name, or with nothing before the "@", that have
// been connected for min_age fire one finding keyed (unnamed), naming
// their accounts, users, addresses, libraries and servers; a named
// connection does not count.
func TestUnnamedConnections(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.unnamedConn(40, "S0", 1, nil, "10.0.0.5", time.Hour, m{"account": "APP", "authorized_user": "alice", "lang": "go", "version": "1.54.0"})
	f.unnamedConn(40, "S1", 2, "@pod-1", "10.0.0.6", time.Minute, m{"account": "APP", "lang": "nats.js", "version": "3.0.2"})
	f.unnamedConn(40, "S2", 3, "", "10.0.0.7", 2*time.Minute, nil)
	f.unnamedConn(40, "S2", 4, "order-service@os-1", "10.0.0.8", time.Hour, m{"account": "APP"})
	r := f.fires("client-007", 40, "(unnamed)")
	want(t, r["(unnamed)"], "info", "3 connections without a name, 1 connected for 10m or longer; accounts $G, APP, user alice, "+
		"from 10.0.0.5 (APP), 10.0.0.6 (APP), 10.0.0.7 ($G), go 1.54.0, nats.js 3.0.2 (nats-0, nats-1, nats-2).")
	if v := r["(unnamed)"].Value; v == nil || *v != 3 {
		t.Errorf("value %v, want 3", v)
	}
}

// A connection younger than min_age does not fire; one connected exactly
// min_age does, and the threshold is tunable.
func TestUnnamedConnectionsAge(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.unnamedConn(40, "S0", 1, nil, "10.0.0.5", 10*time.Minute-time.Second, nil)
	f.fires("client-007", 40)
	if got := f.eval("client-007", 40, map[string]string{"min_age": "5m"}); len(got) != 1 {
		t.Errorf("with min_age 5m: %v", rowsText(got))
	}
	f.unnamedConn(40, "S1", 2, nil, "10.0.0.6", 10*time.Minute, nil)
	r := f.fires("client-007", 40, "(unnamed)")
	want(t, r["(unnamed)"], "info", "2 connections without a name, 1 connected for 10m or longer; account $G, from 10.0.0.5, 10.0.0.6 (nats-0, nats-1).")
	// A connection without a start time is counted but never old enough.
	g := newFx(t)
	g.cluster(40)
	g.row("connections", m{"epoch": 40, "server_id": "S0", "cid": 1, "ip": "10.0.0.5"})
	g.fires("client-007", 40)
	// Only the epoch checked counts: an old unnamed connection of epoch 39 is gone.
	h := newFx(t)
	h.cluster(40)
	h.unnamedConn(39, "S0", 1, nil, "10.0.0.5", time.Hour, nil)
	h.fires("client-007", 40)
}

// A client on a loopback address (a port-forwarded CLI) is tooling and
// left out unless checks.include_tooling is set.
func TestUnnamedConnectionsTooling(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.unnamedConn(40, "S0", 1, nil, "127.0.0.1", time.Hour, nil)
	if got := evalTooling(t, f, "client-007", 40, config.Checks{}); len(got) != 0 {
		t.Errorf("loopback counted: %v", rowsText(got))
	}
	if got := evalTooling(t, f, "client-007", 40, config.Checks{IncludeTooling: true}); len(got) != 1 {
		t.Errorf("with tooling: %v", rowsText(got))
	}
}

// Once firing at the previous epoch, the finding stays while any unnamed
// connection is connected, however young (a client that reconnected), and
// resolves when none is left.
func TestUnnamedConnectionsHysteresis(t *testing.T) {
	f := newFx(t)
	f.cluster(41)
	f.row("findings", m{"id": "client-007:(unnamed)", "check_code": "client-007", "entity_type": "client", "entity_key": "(unnamed)",
		"severity": "info", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
		"transitioned_at": at(20), "last_seen_epoch": 39, "updated_epoch": 39, "detail": "x"})
	f.unnamedConn(40, "S0", 1, nil, "10.0.0.5", time.Minute, nil)
	r := f.fires("client-007", 40, "(unnamed)")
	want(t, r["(unnamed)"], "info", "1 connection without a name, 0 connected for 10m or longer;")
	// At epoch 41 the finding was last seen at 39, not the previous epoch.
	f.unnamedConn(41, "S0", 1, nil, "10.0.0.5", time.Minute, nil)
	f.fires("client-007", 41)
}

// An MQTT client has no NATS connection name, so it is left out; a
// websocket client can set one and counts.
func TestUnnamedConnectionsLeaveOutMQTT(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.unnamedConn(40, "S0", 1, nil, "10.0.0.5", time.Hour, m{"type": "mqtt", "kind": "Client"})
	f.fires("client-007", 40)
	f.unnamedConn(40, "S0", 2, nil, "10.0.0.6", time.Hour, m{"type": "websocket", "kind": "Client"})
	r := f.fires("client-007", 40, "(unnamed)")
	want(t, r["(unnamed)"], "info", "1 connection without a name, 1 connected for 10m or longer;", "from 10.0.0.6 (nats-0).")
}

// A firing finding is kept through a collection that may have missed the
// unnamed connections: a server did not answer, its CONNZ failed, or its
// list was cut at connz_limit. A whole collection without one resolves it.
func TestUnnamedConnectionsPartialCollection(t *testing.T) {
	firing := func(f *fx) {
		f.row("findings", m{"id": "client-007:(unnamed)", "check_code": "client-007", "entity_type": "client", "entity_key": "(unnamed)",
			"severity": "info", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
			"transitioned_at": at(20), "last_seen_epoch": 39, "updated_epoch": 39, "detail": "x"})
	}
	names := []string{"nats-0", "nats-1", "nats-2"}
	// nats-2 did not answer in epoch 40.
	f := newFx(t)
	f.epochs(1, 39, names...)
	f.epoch(40, names, "nats-2")
	firing(f)
	r := f.fires("client-007", 40, "(unnamed)")
	want(t, r["(unnamed)"], "info", "No connection without a name in this collection, which lacks some servers' connections.")
	// CONNZ of nats-1 answered with an error.
	g := newFx(t)
	g.cluster(40)
	g.exec(`UPDATE epoch_endpoints SET endpoint = 'CONNZ', state = 'error' WHERE epoch = 40 AND server_id = 'S1'`)
	firing(g)
	g.fires("client-007", 40, "(unnamed)")
	// nats-0's list was cut at connz_limit.
	h := newFx(t)
	h.cluster(40)
	h.stats(40, "S0", m{"connz_total": 5000, "connz_returned": 4096})
	firing(h)
	h.fires("client-007", 40, "(unnamed)")
	// Not firing before: a partial collection starts nothing.
	k := newFx(t)
	k.epochs(1, 39, names...)
	k.epoch(40, names, "nats-2")
	k.fires("client-007", 40)
	// A whole collection without one resolves it.
	w := newFx(t)
	w.cluster(40)
	firing(w)
	w.fires("client-007", 40)
}

// The hold through partial collections ends: the 10th partial collection
// in a row without an unnamed connection resolves the finding, as when
// connz_limit is below the client count or a server never answers again.
func TestUnnamedConnectionsPartialHoldEnds(t *testing.T) {
	names := []string{"nats-0", "nats-1", "nats-2"}
	build := func(lastUnnamed int64) *fx {
		f := newFx(t)
		f.epochs(1, 30, names...)
		for e := int64(31); e <= 40; e++ {
			f.epoch(e, names, "nats-2")
		}
		f.unnamedConn(lastUnnamed, "S0", 1, nil, "10.0.0.5", time.Hour, nil)
		f.row("findings", m{"id": "client-007:(unnamed)", "check_code": "client-007", "entity_type": "client", "entity_key": "(unnamed)",
			"severity": "info", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
			"transitioned_at": at(20), "last_seen_epoch": 39, "updated_epoch": 39, "detail": "x"})
		return f
	}
	// Epochs 31 to 40 are partial and empty: the 10th resolves.
	build(30).fires("client-007", 40)
	// An unnamed connection in epoch 31: 9 empty partial ones follow, held.
	build(31).fires("client-007", 40, "(unnamed)")
	// A connz_limit cut that never ends counts as partial too.
	f := newFx(t)
	f.cluster(40)
	for e := int64(31); e <= 40; e++ {
		f.stats(e, "S0", m{"connz_total": 5000, "connz_returned": 4096})
	}
	f.row("findings", m{"id": "client-007:(unnamed)", "check_code": "client-007", "entity_type": "client", "entity_key": "(unnamed)",
		"severity": "info", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
		"transitioned_at": at(20), "last_seen_epoch": 39, "updated_epoch": 39, "detail": "x"})
	f.fires("client-007", 40)
}
