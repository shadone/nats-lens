package checks

import (
	"strings"
	"testing"
	"time"
)

var three = []string{"nats-0", "nats-1", "nats-2"}

func TestServerNotReporting(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.server("S2", "nats-2", 1, 1, t0.Add(-time.Hour), "2.15.0")
	f.epoch(2, three, "nats-2")
	f.epoch(3, three, "nats-1", "nats-2")
	f.fires("server-001", 1)
	r := f.fires("server-001", 2, "nats-2")
	want(t, r["nats-2"], "warning", "1 of 3 expected servers missing", "last answered 2026-10-17 09:00:00 UTC")
	r = f.fires("server-001", 3, "nats-1", "nats-2")
	want(t, r["nats-1"], "critical", "never answered")
}

func TestHealthzNotOK(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	for i, name := range three {
		f.server("S"+string(rune('0'+i)), name, 1, 1, t0, "2.15.0")
	}
	f.row("health", m{"epoch": 1, "server_id": "S0", "status": "ok", "status_code": 200, "error_count": 0})
	f.row("health", m{"epoch": 1, "server_id": "S1", "status": "unavailable", "status_code": 503,
		"error": "JetStream stream 'APP > order-jobs' is not current", "error_count": 2,
		"errors": `[{"type":"STREAM","account":"APP","stream":"order-jobs","error":"not current"},{"type":"CONSUMER","account":"APP","stream":"order-jobs","consumer":"w","error":"x"}]`})
	f.row("health", m{"epoch": 1, "server_id": "S2", "status": "unavailable", "status_code": 503,
		"error": "JetStream is not current with the meta leader", "error_count": 1,
		"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
	r := f.fires("server-002", 1, "nats-1")
	want(t, r["nats-1"], "critical", "unavailable (503)", "order-jobs", "(+1 more)")
}

// A JetStream readiness error fires once it has lasted min_duration: at
// warning on a server that restarted within restart_window, else critical.
func TestJetStreamNotReadyLasting(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 12, three...)
	f.restarted(2, 12)
	f.server("S1", "nats-1", 1, 12, t0.Add(-24*time.Hour), "2.15.0")
	for e := int64(1); e <= 12; e++ {
		for _, id := range []string{"S0", "S1"} {
			if id == "S0" && e < 2 {
				continue
			}
			if id == "S1" && e < 3 {
				f.row("health", m{"epoch": e, "server_id": id, "status": "ok", "error_count": 0})
				continue
			}
			f.row("health", m{"epoch": e, "server_id": id, "status": "unavailable", "status_code": 503, "error_count": 1,
				"errors": `[{"type":"JETSTREAM","error":"JetStream is still recovering meta layer"}]`})
		}
	}
	f.fires("server-003", 6)
	r := f.fires("server-003", 9, "nats-0")
	want(t, r["nats-0"], "warning", "still recovering meta layer", "for 00:03:30")
	r = f.fires("server-003", 12, "nats-0", "nats-1")
	want(t, r["nats-1"], "critical", "for 00:04:30")
}

func TestJetStreamDisabled(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.server("S0", "nats-0", 1, 1, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 1, t0, "2.15.0")
	f.stats(1, "S0", m{"js_enabled": false})
	f.stats(1, "S1", m{"js_enabled": true})
	r := f.fires("server-003", 1, "nats-0")
	want(t, r["nats-0"], "critical", "JSZ reports it disabled")
}

func TestServerRestarted(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 200, three...)
	// nats-0 restarted once at epoch 101, nats-1 three times, nats-2 was
	// upgraded and nats-0's earlier restart at epoch 10 is outside the hour
	// of epoch 101 on.
	f.server("A0", "nats-0", 1, 9, t0.Add(-time.Hour), "2.15.0")
	f.server("B0", "nats-0", 10, 100, at(10), "2.15.0")
	f.server("C0", "nats-0", 101, 200, at(101).Add(-5*time.Second), "2.15.0")
	f.server("A1", "nats-1", 1, 100, t0.Add(-time.Hour), "2.15.0")
	for i, e := range []int64{101, 121, 141} {
		f.server("R1"+string(rune('a'+i)), "nats-1", e, e+19, at(e), "2.15.0")
	}
	f.server("A2", "nats-2", 1, 100, t0.Add(-time.Hour), "2.14.3")
	f.server("B2", "nats-2", 101, 200, at(101), "2.15.0")
	r := f.fires("server-004", 150, "nats-0", "nats-1", "nats-2")
	want(t, r["nats-0"], "info", "restarted at 09:49:55 UTC", "1 restart in the last 1h")
	want(t, r["nats-1"], "critical", "3 restarts")
	want(t, r["nats-2"], "info", "restarted for an upgrade from 2.14.3 to 2.15.0")
	r = f.fires("server-004", 50, "nats-0")
	want(t, r["nats-0"], "info")
	// A restart with a changed configuration is shown as one.
	g := newFx(t)
	g.epochs(1, 3, "nats-0")
	g.server("A0", "nats-0", 1, 1, t0.Add(-time.Hour), "2.15.0")
	g.server("B0", "nats-0", 2, 3, at(2), "2.15.0")
	g.stats(1, "A0", m{"config_digest": "aaa"})
	g.stats(2, "B0", m{"config_digest": "bbb"})
	r = g.fires("server-004", 3, "nats-0")
	want(t, r["nats-0"], "info", "restarted with a changed configuration")
}

func TestVersionDiffers(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.epoch(2, three)
	for i, v := range []string{"2.15.0", "2.15.0", "2.14.3"} {
		id := "S" + string(rune('0'+i))
		f.server(id, three[i], 1, 2, t0, v)
		f.stats(1, id, m{"version": v})
		f.stats(2, id, m{"version": "2.15.0"})
	}
	r := f.fires("server-005", 1, "nats-2")
	want(t, r["nats-2"], "info", "runs 2.14.3", "2 of 3 servers run 2.15.0")
	f.fires("server-005", 2)
}

func TestConfigReloaded(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.server("S0", "nats-0", 1, 1, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 1, t0, "2.15.0")
	start := t0.Add(-24 * time.Hour)
	f.stats(1, "S0", m{"start_time": start, "config_load_time": t0.Add(-10 * time.Minute), "config_digest": "abc"})
	f.stats(1, "S1", m{"start_time": start, "config_load_time": start})
	r := f.fires("server-006", 1, "nats-0")
	want(t, r["nats-0"], "info", "reloaded its configuration at 08:50:00 UTC", "abc")
	g := newFx(t)
	g.epoch(1, three)
	g.server("S0", "nats-0", 1, 1, t0, "2.15.0")
	g.stats(1, "S0", m{"start_time": start, "config_load_time": t0.Add(-2 * time.Hour)})
	g.fires("server-006", 1)
}

func TestStoreUsage(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	for i, used := range []int64{80, 95, 50} {
		id := "S" + string(rune('0'+i))
		f.server(id, three[i], 1, 1, t0, "2.15.0")
		f.stats(1, id, m{"js_storage": used << 30, "js_max_storage": int64(100) << 30, "js_memory": 0, "js_max_memory": int64(1) << 30})
	}
	r := f.fires("server-007", 1, "nats-0", "nats-1")
	want(t, r["nats-0"], "warning", "80.0%")
	want(t, r["nats-1"], "critical", "95.0%")
	if r["nats-1"].RelatedConfig["max_file_store"] == "" {
		t.Errorf("related config = %v", r["nats-1"].RelatedConfig)
	}
	r = f.eval("server-007", 1, map[string]string{"warn_ratio": "0.4"})
	if len(r) != 3 {
		t.Fatalf("with warn_ratio 0.4: %s", rowsText(r))
	}
}

func TestMemoryOutlier(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	for i, mem := range []int64{3 << 30, 1 << 30, 1 << 30} {
		id := "S" + string(rune('0'+i))
		f.server(id, three[i], 1, 1, t0, "2.15.0")
		f.stats(1, id, m{"mem_bytes": mem})
	}
	r := f.fires("server-008", 1, "nats-0")
	want(t, r["nats-0"], "warning", "3.0 times the median")
	f.fires("server-008", 1, "nats-0")
	if r := f.eval("server-008", 1, map[string]string{"min_bytes": "4GiB"}); len(r) != 0 {
		t.Fatalf("below min_bytes: %s", rowsText(r))
	}
}

func TestSlowConsumersAndStalls(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 40, three...)
	f.server("S0", "nats-0", 1, 40, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 40, t0, "2.15.0")
	f.server("S2", "nats-2", 1, 40, t0, "2.15.0")
	for e := int64(1); e <= 40; e++ {
		// nats-0: client slow consumers 5 to 8 in the last 15 minutes;
		// nats-1: one route slow consumer; nats-2: flat.
		sc0 := int64(5)
		if e >= 35 {
			sc0 = 8
		}
		route := int64(0)
		if e == 40 {
			route = 1
		}
		f.stats(e, "S0", m{"slow_consumers": sc0, "slow_consumers_clients": sc0, "stalled_clients": e})
		f.stats(e, "S1", m{"slow_consumers": route, "slow_consumers_routes": route, "stalled_clients": 0})
		f.stats(e, "S2", m{"slow_consumers": 2, "slow_consumers_clients": 2, "stalled_clients": 4})
	}
	r := f.fires("server-009", 40, "nats-0", "nats-1")
	want(t, r["nats-0"], "warning", "3 slow consumers in the last 15m", "3 clients")
	want(t, r["nats-1"], "critical", "1 routes")
	r = f.fires("server-010", 40, "nats-0")
	want(t, r["nats-0"], "warning", "30 client stalls in the last 15m")
}

func TestConnectionChurn(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 30, three...)
	f.server("S0", "nats-0", 1, 30, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 30, t0, "2.15.0")
	for e := int64(1); e <= 30; e++ {
		f.stats(e, "S0", m{"total_connections": 100 + 10*e, "connections": 10})
		f.stats(e, "S1", m{"total_connections": 1000 + 10*e, "connections": 500})
	}
	r := f.fires("server-011", 30, "nats-0")
	want(t, r["nats-0"], "warning", "accepted 200 connections in the last 10m and holds 10")
	// Accepting half as many connections as are open within 10 minutes is
	// churn too.
	g := newFx(t)
	g.epochs(1, 30, three...)
	g.server("S2", "nats-2", 1, 30, t0, "2.15.0")
	for e := int64(1); e <= 30; e++ {
		g.stats(e, "S2", m{"total_connections": 1000 + 3*e, "connections": 100})
	}
	g.fires("server-011", 30, "nats-2")
}

func TestRaftCatchingUp(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 20, three...)
	f.server("S1", "nats-1", 1, 20, t0, "2.15.0")
	f.row("raft_groups", m{"group_id": "S-R3F-abc", "kind": "stream", "account": "APP", "stream": "order-jobs",
		"first_epoch": 1, "last_epoch": 20})
	for e := int64(1); e <= 20; e++ {
		f.row("raft_stats", m{"epoch": e, "server_id": "S1", "group_id": "S-R3F-abc", "catching_up": e >= 3})
		f.row("raft_stats", m{"epoch": e, "server_id": "S1", "group_id": "C-R3F-def", "catching_up": e >= 18})
	}
	f.fires("server-012", 10)
	r := f.fires("server-012", 20, "nats-1")
	want(t, r["nats-1"], "warning", "1 raft group", "stream APP/order-jobs", "00:08:30")
}

func TestRaftWriteError(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.server("S0", "nats-0", 1, 1, t0, "2.15.0")
	f.row("raft_stats", m{"epoch": 1, "server_id": "S0", "group_id": "S-1", "wal_error": "{}"})
	f.row("raft_stats", m{"epoch": 1, "server_id": "S0", "group_id": "S-2"})
	r := f.fires("server-013", 1, "nats-0")
	want(t, r["nats-0"], "critical", "1 raft group: S-1")
}

func TestStoreFullSoon(t *testing.T) {
	f := newFx(t)
	names := []string{"nats-0", "nats-1"}
	for e := int64(1); e <= 7; e++ {
		f.epochAt(e, t0.Add(time.Duration(e-1)*time.Hour), names)
	}
	f.server("S0", "nats-0", 1, 3, t0, "2.15.0")
	f.server("S0b", "nats-0", 4, 7, t0.Add(3*time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 7, t0, "2.15.0")
	max := int64(100) << 30
	for e := int64(1); e <= 7; e++ {
		id := "S0"
		if e >= 4 {
			id = "S0b"
		}
		// nats-0 grows 2 GiB an hour across its restart: 40 GiB left at
		// epoch 7 is under a day. nats-1 does not grow.
		f.stats(e, id, m{"js_storage": (48 + 2*e) << 30, "js_max_storage": max})
		f.stats(e, "S1", m{"js_storage": int64(10) << 30, "js_max_storage": max})
	}
	r := f.fires("server-014", 7, "nats-0")
	want(t, r["nats-0"], "critical", "grows 48.0 GiB per day", "full in 19.0 hours")
	if r := f.eval("server-014", 7, map[string]string{"min_span": "12h"}); len(r) != 0 {
		t.Fatalf("with too little data: %s", rowsText(r))
	}
}

func TestStaleConnections(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 40, three...)
	f.server("S0", "nats-0", 1, 40, t0, "2.15.0")
	f.server("S1", "nats-1", 1, 40, t0, "2.15.0")
	for e := int64(1); e <= 40; e++ {
		stale := int64(1)
		if e >= 38 {
			stale = 3
		}
		f.stats(e, "S0", m{"stale_connections": stale})
		f.stats(e, "S1", m{"stale_connections": 1})
	}
	r := f.fires("server-015", 40, "nats-0")
	want(t, r["nats-0"], "info", "2 connections closed as stale in the last 15m")
}

func TestHealthzWithoutErrorText(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.server("S2", "nats-2", 1, 1, t0, "2.15.0")
	f.row("health", m{"epoch": 1, "server_id": "S2", "status": "error", "status_code": 503, "error": "", "error_count": 0})
	r := f.fires("server-002", 1, "nats-2")
	if d := r["nats-2"].Detail; d != "nats-2 healthz error (503)." {
		t.Fatalf("detail = %q", d)
	}
}

// A server that announced lame duck before it went away is restarting on
// purpose: info within restart_window, warning after it.
func TestServerNotReportingAfterLameDuck(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.server("S0", "nats-0", 1, 1, t0.Add(-time.Hour), "2.15.0")
	f.event(1, at(1).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-0"})
	f.epoch(2, three, "nats-0")
	f.epochAt(3, at(1).Add(11*time.Minute), three, "nats-0")
	r := f.fires("server-001", 2, "nats-0")
	want(t, r["nats-0"], "info", "announced lame duck at 09:00:05 UTC")
	r = f.fires("server-001", 3, "nats-0")
	want(t, r["nats-0"], "warning")
}

// restarted stores nats-0 as a process that replaced an earlier one at
// epoch e (start at(e)), answering from e to last.
func (f *fx) restarted(e, last int64) {
	f.server("OLD0", "nats-0", 1, e-1, t0.Add(-time.Hour), "2.15.0")
	f.server("S0", "nats-0", e, last, at(e), "2.15.0")
	for n := e; n <= last; n++ {
		f.stats(n, "S0", m{"start_time": at(e)})
	}
}

// A restarting server's catch-up errors on streams and consumers, and a
// failure without detail, are not a healthz finding; other errors are.
func TestHealthzIgnoresRestartingCatchUp(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 5, three...)
	f.restarted(4, 5)
	f.server("S1", "nats-1", 1, 5, t0.Add(-time.Hour), "2.15.0")
	f.row("health", m{"epoch": 5, "server_id": "S0", "status": "unavailable", "status_code": 503, "error": "not current",
		"error_count": 1, "errors": `[{"type":"STREAM","account":"APP","stream":"jobs","error":"not current"}]`})
	f.row("health", m{"epoch": 4, "server_id": "S0", "status": "error", "status_code": 503, "error": "", "error_count": 0})
	f.row("health", m{"epoch": 5, "server_id": "S1", "status": "unavailable", "status_code": 503, "error": "not current",
		"error_count": 1, "errors": `[{"type":"STREAM","account":"APP","stream":"jobs","error":"not current"}]`})
	f.fires("server-002", 5, "nats-1")
	f.fires("server-002", 4)
}

// A one-off step (a restore, an import) is not steady growth: the rate
// must be positive in both halves of the window, and the smaller counts.
func TestStoreFullSoonIgnoresStep(t *testing.T) {
	f := newFx(t)
	f.cluster(721)
	const gib = int64(1 << 30)
	for e := int64(1); e <= 721; e++ {
		used := 10 * gib
		if e > 600 {
			used = 30 * gib
		}
		f.stats(e, "S0", m{"js_storage": used, "js_max_storage": 100 * gib})
		f.stats(e, "S1", m{"js_storage": 50*gib + e*(gib/100), "js_max_storage": 100 * gib})
	}
	r := f.fires("server-014", 721, "nats-1")
	want(t, r["nats-1"], "critical", "grows 28.7 GiB per day")
}

// With half the servers gone, server-001 says how many replicated streams
// and consumers lost their quorum, so the overview leads with one cause.
func TestServerNotReportingCountsLostQuorum(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three, "nats-1", "nats-2")
	f.server("S0", "nats-0", 1, 1, t0.Add(-time.Hour), "2.15.0")
	for _, st := range []string{"a", "b"} {
		f.streamConfig("S0", st, 1, 1, nil)
		f.streamStats(1, "S0", st, m{"leader": ""})
	}
	f.streamConfig("S0", "r1", 1, 1, m{"replicas": 1})
	f.streamStats(1, "S0", "r1", m{"is_leader": true})
	f.consumerConfig("S0", "a", "w", 1, 1, nil)
	f.consumerStats(1, "S0", "a", "w", m{"leader": ""})
	r := f.fires("server-001", 1, "nats-1", "nats-2")
	want(t, r["nats-1"], "critical", "2 replicated streams and 1 consumer have fewer than a quorum of replicas answering")
}

// A restart that came with an upgrade and one that did not, within the
// lookback, are reported as restarts: no "without an upgrade" count while
// the restarts stay under max_restarts.
func TestServerRestartedMixedWording(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 30, three...)
	f.server("A0", "nats-0", 1, 9, t0.Add(-time.Hour), "2.14.3")
	f.server("B0", "nats-0", 10, 19, at(10), "2.15.0")
	f.server("C0", "nats-0", 20, 30, at(20), "2.15.0")
	r := f.fires("server-004", 30, "nats-0")
	want(t, r["nats-0"], "info", "2 restarts in the last 1h")
	if strings.Contains(r["nats-0"].Detail, "without an upgrade") {
		t.Errorf("detail labels a restart under max_restarts: %s", r["nats-0"].Detail)
	}
}

// Churn must hold for two epochs to fire, then holds until it drops below
// 0.8 of the thresholds, and names the clients that connected most.
func TestConnectionChurnHysteresisAndClients(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 30, three...)
	for i, id := range []string{"S0", "S1", "S2"} {
		f.server(id, three[i], 1, 30, t0, "2.15.0")
	}
	for e := int64(1); e <= 30; e++ {
		// nats-0: 60 in the window at epoch 30, 40 at epoch 29.
		f.stats(e, "S0", m{"total_connections": 1000 + 2*e + 20*(e/30), "connections": 10})
		// nats-1 and nats-2: 2.25 a epoch, 45 in the window: below 50, above 40.
		f.stats(e, "S1", m{"total_connections": 1000 + 9*e/4, "connections": 10})
		f.stats(e, "S2", m{"total_connections": 1000 + 9*e/4, "connections": 10})
	}
	// nats-1 fired at the previous epoch; nats-2 did not.
	f.row("findings", m{"id": "server-011:nats-1", "check_code": "server-011", "entity_type": "server", "entity_key": "nats-1",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
		"transitioned_at": at(20), "last_seen_epoch": 29, "updated_epoch": 29, "detail": "x"})
	for i := range 4 {
		f.event(int64(i+1), at(25).Add(time.Duration(i)*time.Second), "client.connect", m{"source": "sys",
			"client_name": "media-worker", "server_name": "nats-1"})
	}
	f.exec(`INSERT INTO events (id, time, received_at, source, kind, server_name, client_name, summary, raw)
VALUES (9, ?, ?, 'sys', 'client.churn', 'nats-1', 'jetstream-controller', 'churn', '{"connects": 28}')`, at(26), at(26))
	r := f.fires("server-011", 30, "nats-1")
	want(t, r["nats-1"], "warning", "Most connects: jetstream-controller 28, media-worker 4")
}

// Checks that know when their condition began return it: a restart its
// process start, a creation-time difference the newer creation time.
func TestConditionSince(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 200, three...)
	f.server("A0", "nats-0", 1, 100, t0.Add(-time.Hour), "2.15.0")
	f.server("C0", "nats-0", 101, 200, at(101).Add(-5*time.Second), "2.15.0")
	r := f.fires("server-004", 150, "nats-0")
	if got := r["nats-0"].ConditionSince; got == nil || !got.Equal(at(101).Add(-5*time.Second)) {
		t.Errorf("server-004 condition since %v", got)
	}
	g := newFx(t)
	g.cluster(1)
	g.streamConfig("S0", "jobs", 1, 1, m{"created": t0.Add(-48 * time.Hour)})
	g.streamConfig("S1", "jobs", 1, 1, m{"created": t0.Add(-time.Hour)})
	r = g.fires("stream-008", 1, "APP/jobs")
	if got := r["APP/jobs"].ConditionSince; got == nil || !got.Equal(t0.Add(-time.Hour)) {
		t.Errorf("stream-008 condition since %v", got)
	}
	g.consumerConfig("S0", "jobs", "w", 1, 1, m{"created": t0.Add(-48 * time.Hour)})
	g.consumerConfig("S1", "jobs", "w", 1, 1, m{"created": t0.Add(-2 * time.Hour)})
	r = g.fires("consumer-009", 1, "APP/jobs/w")
	if got := r["APP/jobs/w"].ConditionSince; got == nil || !got.Equal(t0.Add(-2*time.Hour)) {
		t.Errorf("consumer-009 condition since %v", got)
	}
}
