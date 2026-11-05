package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// appConn is one connection in an app hours test: on server 0 or 1, with
// its four counters.
type appConn struct {
	server   int
	cid      uint64
	name, ip string
	account  string
	start    time.Time
	in, out  int64
	inB      int64
	outB     int64
}

// appSnapshot is the fixture's epoch at at with conns as the servers'
// connections and no micro services.
func appSnapshot(t *testing.T, at time.Time, conns ...appConn) *collect.Snapshot {
	t.Helper()
	snap := fixtureSnapshot(t, at)
	snap.Micro = nil
	ids := []string{"NFAKESERVER0ID", "NFAKESERVER1ID"}
	for _, id := range ids {
		snap.Servers[id].Connz.Conns = nil
	}
	for _, c := range conns {
		snap.Servers[ids[c.server]].Connz.Conns = append(snap.Servers[ids[c.server]].Connz.Conns, &server.ConnInfo{
			Cid: c.cid, Kind: "Client", Type: "nats", Name: c.name, Account: c.account, IP: c.ip, Start: c.start,
			InMsgs: c.in, OutMsgs: c.out, InBytes: c.inB, OutBytes: c.outB})
	}
	return snap
}

func writeAll(t *testing.T, s *Store, snaps ...*collect.Snapshot) {
	t.Helper()
	for _, snap := range snaps {
		if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
}

const appHoursRows = `SELECT strftime(hour, '%Y-%m-%d %H'), account, app, loopback, in_msgs, out_msgs, in_bytes, out_bytes, epochs
FROM client_app_hours ORDER BY hour, account, app, loopback`

// appHoursScenario writes four epochs around the hour 23:00 UTC:
//
//	22:59:00 every connection's first row, all started long before: nothing.
//	22:59:30 increases; orders-api@pod-c reconnected under a new cid at
//	         22:59:10 and @pod-z connected at 22:59:20 (both after the
//	         previous epoch started): their values count whole; the
//	         unnamed connection's first row was at 22:59:00: nothing yet.
//	23:00:00 pod-b's counters fell under the same start time: whole; the
//	         loopback CLI closed: its last interval is lost.
//	23:00:30 pod-a only.
func appHoursScenario(t *testing.T, s *Store) {
	t.Helper()
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	a := func(in, out, inB, outB int64) appConn {
		return appConn{server: 0, cid: 1, name: "orders-api@pod-a", ip: "10.0.0.1", account: "APP", start: old, in: in, out: out, inB: inB, outB: outB}
	}
	b := func(in, out, inB, outB int64) appConn {
		return appConn{server: 1, cid: 1, name: "orders-api@pod-b", ip: "10.0.0.2", account: "APP", start: old, in: in, out: out, inB: inB, outB: outB}
	}
	c := func(in, out, inB, outB int64) appConn {
		return appConn{server: 0, cid: 3, name: "orders-api@pod-c", ip: "10.0.0.3", account: "APP", start: h.Add(59*time.Minute + 10*time.Second),
			in: in, out: out, inB: inB, outB: outB}
	}
	cli := func(in, out, inB, outB int64) appConn {
		return appConn{server: 0, cid: 2, name: "nats-cli", ip: "127.0.0.1", account: "APP", start: old, in: in, out: out, inB: inB, outB: outB}
	}
	unnamed := func(in, out, inB, outB int64) appConn {
		return appConn{server: 1, cid: 9, ip: "10.0.0.9", start: old, in: in, out: out, inB: inB, outB: outB}
	}
	pod := appConn{server: 1, cid: 10, name: "@pod-z", ip: "10.0.0.10", start: h.Add(59*time.Minute + 20*time.Second), in: 1, out: 1, inB: 10, outB: 10}
	writeAll(t, s,
		appSnapshot(t, h.Add(59*time.Minute), a(100, 200, 1000, 2000), b(50, 70, 500, 700), cli(5, 6, 50, 60), unnamed(10, 0, 100, 0)),
		appSnapshot(t, h.Add(59*time.Minute+30*time.Second), a(110, 230, 1100, 2300), b(55, 80, 550, 800), cli(7, 9, 70, 90),
			c(4, 6, 40, 60), unnamed(10, 0, 100, 0), pod),
		appSnapshot(t, h.Add(time.Hour), a(120, 240, 1200, 2400), b(3, 4, 30, 40), c(10, 6, 100, 60), unnamed(20, 0, 200, 0)),
		appSnapshot(t, h.Add(time.Hour+30*time.Second), a(125, 245, 1250, 2450)),
	)
}

// The hourly traffic adds each connection's increase over its previous
// row to the hour of the later epoch, by account, application (the name
// before "@") and loopback: the counts of appHoursScenario.
func TestClientAppHours(t *testing.T) {
	s := openMem(t)
	appHoursScenario(t, s)
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		// @pod-z: 1, 1, 10, 10; the unnamed connection's first row adds nothing.
		"2026-10-16 22||(unnamed)|false|1|1|10|10|1",
		// pod-a +10 +30 +100 +300, pod-b +5 +10 +50 +100, pod-c 4 6 40 60 whole.
		"2026-10-16 22|APP|nats-cli|true|2|3|20|30|1",
		"2026-10-16 22|APP|orders-api|false|19|46|190|460|1",
		// The unnamed connection +10 +0 +100 +0.
		"2026-10-16 23||(unnamed)|false|10|0|100|0|1",
		// 23:00:00 pod-a +10 +10 +100 +100, pod-b 3 4 30 40 whole, pod-c +6 +0 +60 +0;
		// 23:00:30 pod-a +5 +5 +50 +50.
		"2026-10-16 23|APP|orders-api|false|24|19|240|190|2",
	})
}

// A gap between two of a connection's rows: rows 55 minutes apart pair
// and the whole increase goes to the later row's hour; rows more than an
// hour apart do not pair, and the later one counts as a first row: nothing
// for a connection that started before the previous epoch, its values for
// one that started after.
func TestClientAppHoursGap(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	a := func(n int64) appConn {
		return appConn{server: 0, cid: 1, name: "orders-api@pod-a", ip: "10.0.0.1", account: "APP", start: old, in: n, out: n, inB: n, outB: n}
	}
	late := appConn{server: 1, cid: 4, name: "billing@pod-1", ip: "10.0.0.4", account: "APP", start: h.Add(time.Hour + 40*time.Minute),
		in: 7, out: 8, inB: 70, outB: 80}
	writeAll(t, s,
		appSnapshot(t, h.Add(10*time.Minute), a(100)),
		// Five minutes later: more than three 30-second intervals.
		appSnapshot(t, h.Add(15*time.Minute), a(150)),
		appSnapshot(t, h.Add(time.Hour+10*time.Minute), a(200)),
		appSnapshot(t, h.Add(2*time.Hour+30*time.Minute), a(260), late),
	)
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		"2026-10-16 22|APP|orders-api|false|50|50|50|50|1",
		"2026-10-16 23|APP|orders-api|false|50|50|50|50|1",
		"2026-10-17 00|APP|billing|false|7|8|70|80|1",
	})
}

// A connection is (server, cid, start time): the same cid on another
// server, or on the same server with another start time (a restarted
// server reusing it), is another connection, whose first row counts by
// the first-row rule; a connection listed twice in one epoch counts once.
func TestClientAppHoursConnectionKey(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	old := h.Add(-24 * time.Hour)
	conn := func(srv int, start time.Time, n int64) appConn {
		return appConn{server: srv, cid: 5, name: "orders-api", ip: "10.0.0.5", account: "APP", start: start, in: n, out: n, inB: n, outB: n}
	}
	restart := h.Add(10*time.Minute + 15*time.Second)
	writeAll(t, s,
		appSnapshot(t, h.Add(10*time.Minute), conn(0, old, 100)),
		// Server 0 restarted at 22:10:15 and gave cid 5 again; server 1
		// has a cid 5 that started long ago; the restarted one is listed
		// twice.
		appSnapshot(t, h.Add(10*time.Minute+30*time.Second), conn(0, restart, 3), conn(0, restart, 3), conn(1, old, 1000)),
		appSnapshot(t, h.Add(11*time.Minute), conn(0, restart, 5), conn(0, restart, 5), conn(1, old, 1004)),
	)
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		// 3 whole, then +2 and +4.
		"2026-10-16 22|APP|orders-api|false|9|9|9|9|2",
	})
}

// The hourly traffic and its sweep do not depend on the database's time
// zone: with TimeZone set west and east of UTC, appHoursScenario gives the
// same rows as in UTC, and the sweep keeps the same hours.
func TestClientAppHoursAnyTimeZone(t *testing.T) {
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	run := func(zone string) ([]string, []string) {
		s := openMem(t)
		if _, err := s.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
		appHoursScenario(t, s)
		before := rowStrings(t, s.DB(), appHoursRows)
		if _, err := s.Sweep(context.Background(), h.Add(time.Hour+30*time.Minute), 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		return before, rowStrings(t, s.DB(), appHoursRows)
	}
	wantBefore, wantAfter := run("UTC")
	if len(wantBefore) != 5 || len(wantAfter) != 2 {
		t.Fatalf("UTC: rows %v, after the sweep %v", wantBefore, wantAfter)
	}
	for _, zone := range []string{"America/Los_Angeles", "Pacific/Auckland"} {
		before, after := run(zone)
		wantRows(t, zone+" rows", before, wantBefore)
		wantRows(t, zone+" rows after the sweep", after, wantAfter)
	}
}

// Migration 0034 applies to a store that holds connections, and the
// first epoch written after it pairs its rows with the rows written
// before it.
func TestClientAppHoursMigrationOnAStoreWithData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	conn := func(n int64) appConn {
		return appConn{server: 0, cid: 1, name: "orders-api@pod-a", ip: "10.0.0.1", account: "APP", start: h.Add(-time.Hour),
			in: n, out: n, inB: n, outB: n}
	}
	writeAll(t, s, appSnapshot(t, h.Add(10*time.Minute), conn(100)))
	// Undo migrations 0034 and later by hand: the store as the version
	// before wrote it.
	for _, q := range []string{`DROP TABLE client_app_hours`, `DROP TABLE notify_channels`, `DROP TABLE notify_state`,
		`DROP TABLE notify_outbox`, `DROP TABLE notify_identity`, `DROP TABLE notify_delivery_rate`, `DROP SEQUENCE notify_outbox_seq`,
		`DROP TABLE acknowledgements`, `DROP SEQUENCE ack_seq`, `ALTER TABLE archive_hours DROP COLUMN ack_changes`,
		`DELETE FROM migrations WHERE version >= 34`} {
		if _, err := s.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	if s, err = OpenPath(ctx, path); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()
	if n := count(t, s.DB(), `SELECT count(*) FROM client_app_hours`); n != 0 {
		t.Fatalf("client_app_hours after the migration = %d rows, want 0", n)
	}
	writeAll(t, s, appSnapshot(t, h.Add(10*time.Minute+30*time.Second), conn(130)))
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		"2026-10-16 22|APP|orders-api|false|30|30|30|30|1",
	})
}

// A connection from ::1 is a loopback connection as one from 127.0.0.1 is.
func TestClientAppHoursIPv6Loopback(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	cli := func(n int64) appConn {
		return appConn{server: 0, cid: 2, name: "nats-cli", ip: "::1", account: "APP", start: h.Add(-time.Hour), in: n, out: n, inB: n, outB: n}
	}
	writeAll(t, s, appSnapshot(t, h.Add(10*time.Minute), cli(5)), appSnapshot(t, h.Add(10*time.Minute+30*time.Second), cli(8)))
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		"2026-10-16 22|APP|nats-cli|true|3|3|3|3|1",
	})
}

// A fall of out_bytes alone, the other counters rising, makes the row
// count whole.
func TestClientAppHoursOutBytesFell(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	conn := func(n, outB int64) appConn {
		return appConn{server: 0, cid: 1, name: "orders-api@pod-a", ip: "10.0.0.1", account: "APP", start: h.Add(-time.Hour),
			in: n, out: n, inB: n, outB: outB}
	}
	writeAll(t, s, appSnapshot(t, h.Add(10*time.Minute), conn(100, 500)),
		appSnapshot(t, h.Add(10*time.Minute+30*time.Second), conn(110, 40)))
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		"2026-10-16 22|APP|orders-api|false|110|110|110|40|1",
	})
}

// A connection whose start time is exactly the previous epoch's start did
// not start after it: its first row counts nothing.
func TestClientAppHoursStartAtPreviousEpoch(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	prev := h.Add(10 * time.Minute)
	other := appConn{server: 1, cid: 9, name: "billing", ip: "10.0.0.9", account: "APP", start: h.Add(-time.Hour), in: 1, out: 1, inB: 1, outB: 1}
	conn := appConn{server: 0, cid: 1, name: "orders-api@pod-a", ip: "10.0.0.1", account: "APP", start: prev, in: 7, out: 7, inB: 7, outB: 7}
	writeAll(t, s, appSnapshot(t, prev, other), appSnapshot(t, prev.Add(30*time.Second), other, conn))
	wantRows(t, "client_app_hours", rowStrings(t, s.DB(), appHoursRows), []string{
		"2026-10-16 22|APP|billing|false|0|0|0|0|1",
	})
}

// A failed lookback names the rollup it was for.
func TestLookbackErrorNamesTheRollup(t *testing.T) {
	s := openMem(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	w := newWriter(context.Background(), tx, 1, appSnapshot(t, time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)))
	w.lookback("client app", appLookback)
	if w.err == nil || !strings.HasPrefix(w.err.Error(), "client app hours lookback: ") {
		t.Errorf("lookback error = %v, want one that starts with %q", w.err, "client app hours lookback: ")
	}
}
