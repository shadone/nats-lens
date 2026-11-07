package storeprov_test

import (
	"fmt"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// The applications store holds three epochs: E1 26 hours before last, E2
// 3 hours before it and E3 at last, the newest. Its tooling names are
// natslens-apps and natslens-apps-events.
const (
	appsE1, appsE2, appsE3 = int64(1), int64(2), int64(3)
)

var appsTooling = []string{"natslens-apps", "natslens-apps-events"}

// appsConn is one connection of the applications store.
type appsConn struct {
	server  string
	cid     int64
	name    string
	account string
	ip      string
	lang    string
	version string
	rttMs   int64
	subs    int64
	pending int64
}

// appsConnsE3 are the connections of the newest epoch. Without tooling:
// resize-worker has 5 connections of 4 pods on 3 servers; callback-
// dispatcher 2 without a pod, on two library versions; order-service 5,
// 4 of them on nats-0; two unnamed (one empty, one "@pod-z"); ops-monitor
// in SYS. nats-0 holds 8 of the 15. Tooling: natslens-apps in SYS and
// nats-cli from a loopback address in the global account.
var appsConnsE3 = []appsConn{
	{"S0", 1, "resize-worker@rw-a", "APP", "10.1.0.1", "go", "1.54.0", 2, 4, 0},
	{"S0", 2, "resize-worker@rw-b", "APP", "10.1.0.2", "go", "1.54.0", 4, 4, 300},
	{"S1", 3, "resize-worker@rw-c", "APP", "10.1.0.3", "go", "1.54.0", 3, 4, 0},
	{"S2", 4, "resize-worker@rw-d", "APP", "10.1.0.4", "go", "1.54.0", 5, 4, 0},
	{"S2", 5, "resize-worker@rw-d", "APP", "10.1.0.4", "go", "1.54.0", 6, 2, 0},
	{"S0", 6, "callback-dispatcher", "APP", "10.1.0.6", "nats.js", "3.0.2", 1, 2, 0},
	{"S0", 7, "callback-dispatcher", "APP", "10.1.0.7", "nats.js", "2.29.3", 1, 2, 0},
	{"S0", 8, "order-service", "APP", "10.1.0.8", "nats.js", "3.0.2", 1, 1, 0},
	{"S0", 9, "order-service", "APP", "10.1.0.9", "nats.js", "3.0.2", 1, 1, 0},
	{"S0", 10, "order-service", "APP", "10.1.0.10", "nats.js", "3.0.2", 1, 1, 0},
	{"S0", 17, "order-service", "APP", "10.1.0.17", "nats.js", "3.0.2", 1, 1, 0},
	{"S1", 11, "order-service", "APP", "10.1.0.11", "nats.js", "3.0.2", 1, 1, 0},
	{"S1", 12, "", "APP", "10.1.0.12", "python3", "2.7.0", 9, 0, 0},
	{"S2", 13, "@pod-z", "APP", "10.1.0.13", "python3", "2.7.0", 9, 0, 0},
	{"S2", 16, "ops-monitor", "SYS", "10.1.0.16", "go", "1.54.0", 1, 1, 0},
	{"S0", 14, "natslens-apps", "SYS", "10.1.0.14", "go", "1.54.0", 1, 3, 0},
	{"S1", 15, "nats-cli", "", "127.0.0.1", "go", "1.54.0", 1, 0, 0},
}

// appsConnsE2 are the connections three hours before, when nats-2 did
// not answer: resize-worker had two pods, and nats-0 holds two of the
// three connections.
var appsConnsE2 = []appsConn{
	{"S0", 1, "resize-worker@rw-a", "APP", "10.1.0.1", "go", "1.54.0", 2, 4, 0},
	{"S1", 3, "resize-worker@rw-c", "APP", "10.1.0.3", "go", "1.54.0", 3, 4, 0},
	{"S0", 6, "callback-dispatcher", "APP", "10.1.0.6", "nats.js", "3.0.2", 1, 2, 0},
}

// appsEvent is one client event: its time before last, kind, name,
// account, host and reason; churn is a client.churn event's connects.
type appsEvent struct {
	before                            time.Duration
	kind, name, account, host, reason string
	churn                             int
}

// appsEvents are the client events of the store. In the 24 hours before
// last: callback-dispatcher dropped six times as a stale connection and
// came back each time; resize-worker dropped once as a slow consumer
// (rw-a, which came back) and once stale (rw-old, a pod gone by E3, whose
// replacement rw-b connected: no reconnect, as drops and connects pair
// per name);
// order-service's servers shut down (not reconnects) and it opened 12
// short-lived connections; nats-cli reconnected 3 times from a loopback
// address; natslens-apps reconnected; ops-monitor in SYS dropped and came
// back. Outside it: a stale drop of callback-dispatcher 25 hours before
// last, one exactly 24 hours before (left out: the day excludes its
// start), and one after last.
func appsEvents() []appsEvent {
	var out []appsEvent
	for k := range 6 {
		at := 20*time.Hour - time.Duration(k)*3*time.Hour
		out = append(out, appsEvent{at, "client.disconnect", "callback-dispatcher", "APP", "10.1.0.6", "Stale Connection", 0},
			appsEvent{at - 5*time.Second, "client.connect", "callback-dispatcher", "APP", "10.1.0.6", "", 0})
	}
	out = append(out,
		appsEvent{25 * time.Hour, "client.disconnect", "callback-dispatcher", "APP", "10.1.0.6", "Stale Connection", 0},
		appsEvent{24 * time.Hour, "client.disconnect", "callback-dispatcher", "APP", "10.1.0.6", "Stale Connection", 0},
		appsEvent{-time.Minute, "client.disconnect", "callback-dispatcher", "APP", "10.1.0.6", "Stale Connection", 0},
		appsEvent{2 * time.Hour, "client.disconnect", "resize-worker@rw-a", "APP", "10.1.0.1", "Slow Consumer (Write Deadline)", 0},
		appsEvent{2*time.Hour - 3*time.Second, "client.connect", "resize-worker@rw-a", "APP", "10.1.0.1", "", 0},
		appsEvent{5 * time.Hour, "client.disconnect", "resize-worker@rw-old", "APP", "10.1.0.30", "Stale Connection", 0},
		appsEvent{5*time.Hour - 3*time.Second, "client.connect", "resize-worker@rw-b", "APP", "10.1.0.2", "", 0},
		appsEvent{6 * time.Hour, "client.disconnect", "order-service", "APP", "10.1.0.8", "Server Shutdown", 0},
		appsEvent{6*time.Hour - 9*time.Second, "client.connect", "order-service", "APP", "10.1.0.8", "", 0},
		appsEvent{time.Hour, "client.churn", "order-service", "APP", "10.1.0.40", "Client Closed", 12},
		appsEvent{4 * time.Hour, "client.disconnect", "ops-monitor", "SYS", "10.1.0.16", "Read Error", 0},
		appsEvent{4*time.Hour - 2*time.Second, "client.connect", "ops-monitor", "SYS", "10.1.0.16", "", 0},
		appsEvent{7 * time.Hour, "client.disconnect", "natslens-apps", "SYS", "10.1.0.14", "Read Error", 0},
		appsEvent{7*time.Hour - 2*time.Second, "client.connect", "natslens-apps", "SYS", "10.1.0.14", "", 0},
	)
	for k := range 3 {
		at := 30*time.Minute - time.Duration(k)*time.Minute
		out = append(out, appsEvent{at, "client.connect", "nats-cli", "$G", "127.0.0.1", "", 0},
			appsEvent{at - 10*time.Second, "client.disconnect", "nats-cli", "$G", "127.0.0.1", "", 0})
	}
	return out
}

// appsHour is one row of client_app_hours: the hour by its offset from
// the hour of last, application, account, loopback and messages in (out
// is twice, bytes 100 times each).
type appsHour struct {
	back     int
	app      string
	account  string
	loopback bool
	in       int64
}

// appsHours: resize-worker has the 8 hours before last's hour, 1000 times
// the offset; callback-dispatcher every hour from 25 back to last's own
// (10 each); order-service 50 in the hour before, and 5 more from a
// loopback address; nats-cli from a loopback address in $G; natslens-apps
// in SYS.
func appsHours() []appsHour {
	var out []appsHour
	for b := 1; b <= 8; b++ {
		out = append(out, appsHour{b, "resize-worker", "APP", false, int64(1000 * b)})
	}
	for b := 0; b <= 25; b++ {
		out = append(out, appsHour{b, "callback-dispatcher", "APP", false, 10})
	}
	return append(out, appsHour{1, "order-service", "APP", false, 50}, appsHour{1, "order-service", "APP", true, 5},
		appsHour{2, "nats-cli", "", true, 7}, appsHour{2, "natslens-apps", "SYS", false, 70})
}

// appsStore writes the applications store whose newest epoch starts at
// last. nats-2's connection list in E3 was cut at the connz limit.
// Findings: client-003 on resize-worker@rw-gone fired at E1 and resolved
// at E2; client-002 on resize-worker@rw-c fires from E2; client-004 on
// callback-dispatcher and client-001 on order-service fire at E3, the
// latter muted (appsProvider).
func appsStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	for i, id := range []string{"S0", "S1", "S2"} {
		r.add("servers", map[string]any{"server_id": id, "server_name": fmt.Sprintf("nats-%d", i), "first_epoch": appsE1,
			"last_epoch": appsE3, "first_seen_at": last.Add(-26 * time.Hour), "last_seen_at": last})
	}
	at := map[int64]time.Time{appsE1: last.Add(-26 * time.Hour), appsE2: last.Add(-3 * time.Hour), appsE3: last}
	for e := appsE1; e <= appsE3; e++ {
		r.epoch(e, at[e], "S0", "S1", "S2")
	}
	// nats-2 did not answer E2.
	if _, err := st.DB().Exec(`UPDATE epochs SET partial = true, missing_names = ['nats-2'] WHERE epoch = ?`, appsE2); err != nil {
		t.Fatal(err)
	}
	write := func(e int64, cs []appsConn) {
		for _, c := range cs {
			r.add("connections", map[string]any{"epoch": e, "server_id": c.server, "cid": c.cid, "name": c.name, "account": c.account,
				"ip": c.ip, "lang": c.lang, "version": c.version, "rtt_ns": c.rttMs * int64(time.Millisecond), "subscriptions": c.subs,
				"pending_bytes": c.pending, "start_time": at[appsE1]})
		}
	}
	write(appsE2, appsConnsE2)
	write(appsE3, appsConnsE3)
	r.add("server_stats", map[string]any{"epoch": appsE3, "server_id": "S2", "connz_total": 40, "connz_returned": 4})
	r.add("server_stats", map[string]any{"epoch": appsE2, "server_id": "S2", "connz_total": 4, "connz_returned": 4})
	for i, e := range appsEvents() {
		when := last.Add(-e.before)
		var raw any
		if e.churn > 0 {
			raw = fmt.Sprintf(`{"connects":%d}`, e.churn)
		}
		r.add("events", map[string]any{"id": 100 + i, "time": when, "received_at": when, "source": "sys", "kind": e.kind,
			"client_name": e.name, "account": e.account, "client_host": e.host, "reason": e.reason, "summary": e.kind + " " + e.name, "raw": raw})
	}
	// The connection that captures system events lost its server for a
	// minute 10 hours before last.
	for i, kind := range []string{"collector.disconnected", "collector.reconnected"} {
		when := last.Add(-10*time.Hour + time.Duration(i)*time.Minute)
		r.add("events", map[string]any{"id": 90 + i, "time": when, "received_at": when, "source": "collector", "kind": kind,
			"summary": kind + " sys", "raw": `{"connection":"sys"}`})
	}
	hour := last.Truncate(time.Hour)
	for _, h := range appsHours() {
		r.add("client_app_hours", map[string]any{"hour": hour.Add(-time.Duration(h.back) * time.Hour), "account": h.account, "app": h.app,
			"loopback": h.loopback, "in_msgs": h.in, "out_msgs": 2 * h.in, "in_bytes": 100 * h.in, "out_bytes": 200 * h.in, "epochs": 12})
	}
	finding := func(code, key string) store.CheckRun {
		return store.CheckRun{Code: code, EntityType: "client", Status: store.RunOK,
			Rows: []store.FindingRow{{EntityKey: key, Severity: "warning", Detail: code + " on " + key}}}
	}
	none := func(code string) store.CheckRun {
		return store.CheckRun{Code: code, EntityType: "client", Status: store.RunOK}
	}
	for i, runs := range [][]store.CheckRun{
		{finding("client-003", "resize-worker@rw-gone"), none("client-002"), none("client-004")},
		{none("client-003"), finding("client-002", "resize-worker@rw-c"), none("client-004")},
		{none("client-003"), finding("client-002", "resize-worker@rw-c"), finding("client-004", "callback-dispatcher"),
			finding("client-001", "order-service")},
	} {
		if _, err := st.ApplyCheckRuns(ctx, appsE1+int64(i), runs); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// appsProvider serves appsStore with its clock 30 seconds after last, with
// client-001 on order-service muted.
func appsProvider(t *testing.T, last time.Time) *storeprov.Provider {
	t.Helper()
	clock := last.Add(30 * time.Second)
	p := &storeprov.Provider{St: appsStore(t, last), Interval: 5 * time.Minute, Live: true, Clock: func() time.Time { return clock },
		Retention: 14 * day, ToolingNames: appsTooling}
	if _, err := p.MuteFinding(ctx, &v1.MuteFindingRequest{CheckCode: "client-001",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: "order-service"}, Reason: "Its office instances are far away.",
		MutedBy: "ops@example.com", ExpiresAt: timestamppb.New(clock.Add(24 * time.Hour))}); err != nil {
		t.Fatal(err)
	}
	return p
}
