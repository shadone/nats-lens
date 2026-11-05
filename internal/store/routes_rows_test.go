package store

import (
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// writeRoutesByRow and writeGatewaysByRow are the writer's earlier route
// and gateway writes, one INSERT per row, kept as the reference the JSON
// batches must match.
func (w *writer) writeRoutesByRow(v *collect.ServerView) {
	cols := []string{
		"epoch", "server_id", "rid", "remote_id", "remote_name", "did_solicit", "is_configured", "ip", "port",
		"start_time", "last_activity", "rtt_ns", "pending_bytes", "in_msgs", "out_msgs", "in_bytes", "out_bytes",
		"subscriptions", "account", "compression",
	}
	for _, r := range v.Routez.Routes {
		if r == nil {
			continue
		}
		w.insert("routes", cols, w.epoch, v.Info.ID, i64(r.Rid), r.RemoteID, r.RemoteName, r.DidSolicit,
			r.IsConfigured, r.IP, r.Port, ts(r.Start), ts(r.LastActivity), durNS(r.RTT), r.Pending,
			r.InMsgs, r.OutMsgs, r.InBytes, r.OutBytes, int64(r.NumSubs), r.Account, r.Compression)
	}
}

func (w *writer) writeGatewaysByRow(v *collect.ServerView) {
	cols := []string{
		"epoch", "server_id", "gateway", "direction", "is_configured", "cid", "ip", "port", "rtt_ns",
		"pending_bytes", "in_msgs", "out_msgs", "in_bytes", "out_bytes",
	}
	one := func(name, dir string, g *server.RemoteGatewayz) {
		if g == nil {
			return
		}
		vals := []any{w.epoch, v.Info.ID, name, dir, g.IsConfigured}
		if c := g.Connection; c != nil {
			vals = append(vals, i64(c.Cid), c.IP, c.Port, durNS(c.RTT), c.Pending, c.InMsgs, c.OutMsgs, c.InBytes, c.OutBytes)
		} else {
			vals = append(vals, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		w.insert("gateways", cols, vals...)
	}
	for _, name := range sortedKeys(v.Gatewayz.OutboundGateways) {
		one(name, "outbound", v.Gatewayz.OutboundGateways[name])
	}
	for _, name := range sortedKeys(v.Gatewayz.InboundGateways) {
		for _, g := range v.Gatewayz.InboundGateways[name] {
			one(name, "inbound", g)
		}
	}
}

var routeNanos = time.Date(2026, 10, 16, 23, 17, 46, 123456789, time.FixedZone("IST", 5*3600+1800))

// edgeRoutezView holds routes whose values a JSON round trip or a session
// time zone could change: times with nanoseconds, in another zone and
// before 1970, zero times, integers above 2^53 and at the int64 limits, a
// rid beyond it, odd strings, an IPv6 address, a missing and an unparsable
// RTT, and a null entry. The second view has more routes than one batch.
func edgeRoutezView() *collect.ServerView {
	return &collect.ServerView{
		Info: server.ServerInfo{ID: "NROUTE'0\"\\"},
		Routez: &server.Routez{Routes: []*server.RouteInfo{
			{
				Rid: above53, RemoteID: "NREMOTE\x00'q", RemoteName: `nats "1" \ été`, DidSolicit: true,
				IsConfigured: true, IP: "2001:db8::2", Port: 6222, Start: routeNanos,
				LastActivity: routeNanos.Add(999 * time.Nanosecond), RTT: "812.345µs", Pending: math.MaxInt32,
				InMsgs: above53, OutMsgs: math.MaxInt64, InBytes: above53 + 2, OutBytes: math.MaxInt64 - 1,
				NumSubs: math.MaxUint32, Account: "APP", Compression: "s2_fast",
			},
			nil,
			{Rid: math.MaxUint64, RTT: "not a duration", InMsgs: -1, OutBytes: math.MinInt64,
				Start: time.Date(1969, 12, 31, 23, 59, 59, 999999999, time.UTC), LastActivity: time.Unix(0, 1)},
			{Rid: 1, RemoteName: "nats-2", IP: "10.0.0.2", Port: 6222},
		}},
	}
}

func manyRoutesView(id string, n int) *collect.ServerView {
	v := &collect.ServerView{Info: server.ServerInfo{ID: id}, Routez: &server.Routez{}}
	for i := range n {
		v.Routez.Routes = append(v.Routez.Routes, &server.RouteInfo{
			Rid: uint64(i + 1), RemoteID: fmt.Sprintf("NR%d", i%9), RemoteName: fmt.Sprintf("nats-%d", i%9),
			DidSolicit: i%2 == 0, IP: "10.0.0.3", Port: 6222, Start: t0.Add(-time.Duration(i) * time.Second),
			LastActivity: t0, RTT: "1ms", InMsgs: int64(i), NumSubs: uint32(i), Account: fmt.Sprintf("ACC%d", i%4),
		})
	}
	return v
}

// edgeGatewayzView holds gateways with and without a connection, null
// entries, an empty connection IP and an unparsable RTT.
func edgeGatewayzView() *collect.ServerView {
	return &collect.ServerView{
		Info: server.ServerInfo{ID: "NGW'0\"\\"},
		Gatewayz: &server.Gatewayz{
			OutboundGateways: map[string]*server.RemoteGatewayz{
				`east "q" \`: {IsConfigured: true, Connection: &server.ConnInfo{Cid: math.MaxUint64, IP: "2001:db8::3",
					Port: 7222, RTT: "2.5ms", Pending: math.MaxInt32, InMsgs: above53, OutMsgs: math.MaxInt64,
					InBytes: -1, OutBytes: math.MinInt64}},
				"west": {IsConfigured: false},
				"gone": nil,
			},
			InboundGateways: map[string][]*server.RemoteGatewayz{
				"east":  {nil, {Connection: &server.ConnInfo{Cid: above53, RTT: "not a duration"}}},
				"north": {{IsConfigured: true, Connection: &server.ConnInfo{Cid: 3, IP: "10.0.0.4", Port: 7222}}},
			},
		},
	}
}

// requireSameRows fails when the rows of table in epochs 1 and 2 differ in
// any column but the epoch, NULLs included.
func requireSameRows(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	a := `SELECT * EXCLUDE (epoch) FROM ` + table + ` WHERE epoch = 1`
	b := `SELECT * EXCLUDE (epoch) FROM ` + table + ` WHERE epoch = 2`
	for _, q := range []string{a + ` EXCEPT ALL ` + b, b + ` EXCEPT ALL ` + a} {
		if n := count(t, db, `SELECT count(*) FROM (`+q+`)`); n != 0 {
			t.Errorf("%d rows differ: %s", n, q)
		}
	}
}

// TestRouteRowsMatchRowByRow writes the same routes with the JSON batch
// (epoch 2) and with one INSERT per row (epoch 1). Times must land as the
// same instant whatever the session time zone: run it with a non-UTC TZ.
func TestRouteRowsMatchRowByRow(t *testing.T) {
	s := openMem(t)
	for _, v := range []*collect.ServerView{edgeRoutezView(), manyRoutesView("NROUTE1", jsonBatch+3)} {
		writeWith(t, s, 1, v, (*writer).writeRoutesByRow)
		writeWith(t, s, 2, v, (*writer).writeRoutes)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM routes WHERE epoch = 2`); n != jsonBatch+6 {
		t.Fatalf("batch rows = %d, want %d", n, jsonBatch+6)
	}
	requireSameRows(t, s.DB(), "routes")
}

// TestRouteRowsEdgeValues compares the batch's edge rows with fixed
// expected values, so that a fault shared by both writes is caught too.
func TestRouteRowsEdgeValues(t *testing.T) {
	s := openMem(t)
	v := edgeRoutezView()
	writeWith(t, s, 1, v, (*writer).writeRoutes)
	type stored struct {
		serverID, remoteID, remoteName, ip, account, compression     string
		rid, port, pending, inMsgs, outMsgs, inBytes, outBytes, subs int64
		start, last                                                  sql.NullTime
		rtt                                                          sql.NullInt64
		solicit, configured                                          bool
	}
	rows, err := s.DB().Query(`SELECT server_id, rid, remote_id, remote_name, did_solicit, is_configured, ip, port,
  start_time, last_activity, rtt_ns, pending_bytes, in_msgs, out_msgs, in_bytes, out_bytes, subscriptions, account,
  compression FROM routes ORDER BY rid, in_msgs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []stored
	for rows.Next() {
		var r stored
		if err := rows.Scan(&r.serverID, &r.rid, &r.remoteID, &r.remoteName, &r.solicit, &r.configured, &r.ip,
			&r.port, &r.start, &r.last, &r.rtt, &r.pending, &r.inMsgs, &r.outMsgs, &r.inBytes, &r.outBytes, &r.subs,
			&r.account, &r.compression); err != nil {
			t.Fatal(err)
		}
		// Compare instants, not zones.
		r.start.Time, r.last.Time = r.start.Time.UTC(), r.last.Time.UTC()
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	at := func(tm time.Time) sql.NullTime { return sql.NullTime{Time: tm.UTC(), Valid: true} }
	id := v.Info.ID
	want := []stored{
		{serverID: id, rid: 1, remoteName: "nats-2", ip: "10.0.0.2", port: 6222},
		{serverID: id, rid: above53, remoteID: "NREMOTE\x00'q", remoteName: `nats "1" \ été`, solicit: true,
			configured: true, ip: "2001:db8::2", port: 6222, start: at(routeNanos.Truncate(time.Microsecond)),
			last: at(routeNanos.Add(999 * time.Nanosecond).Truncate(time.Microsecond)),
			rtt:  sql.NullInt64{Int64: 812_345, Valid: true}, pending: math.MaxInt32, inMsgs: above53,
			outMsgs: math.MaxInt64, inBytes: above53 + 2, outBytes: math.MaxInt64 - 1, subs: math.MaxUint32,
			account: "APP", compression: "s2_fast"},
		// Before 1970: truncated towards the earlier microsecond, as the driver does.
		{serverID: id, rid: math.MaxInt64, inMsgs: -1, outBytes: math.MinInt64,
			start: at(time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC)), last: at(time.Unix(0, 0))},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// TestGatewayRowsMatchRowByRow writes the same gateways with the JSON
// batch (epoch 2) and with one INSERT per row (epoch 1).
func TestGatewayRowsMatchRowByRow(t *testing.T) {
	s := openMem(t)
	v := edgeGatewayzView()
	writeWith(t, s, 1, v, (*writer).writeGatewaysByRow)
	writeWith(t, s, 2, v, (*writer).writeGateways)
	if n := count(t, s.DB(), `SELECT count(*) FROM gateways WHERE epoch = 2`); n != 4 {
		t.Fatalf("batch rows = %d, want 4", n)
	}
	requireSameRows(t, s.DB(), "gateways")
}

// TestGatewayRowsEdgeValues compares the batch's rows with fixed expected
// values.
func TestGatewayRowsEdgeValues(t *testing.T) {
	s := openMem(t)
	v := edgeGatewayzView()
	writeWith(t, s, 1, v, (*writer).writeGateways)
	got := map[string]string{}
	rows, err := s.DB().Query(`SELECT server_id, gateway, direction, is_configured, cid, ip, port, rtt_ns,
  pending_bytes, in_msgs, out_msgs, in_bytes, out_bytes FROM gateways`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			serverID, gateway, dir string
			configured             bool
			ip                     sql.NullString
			cid, port, rtt, pend   sql.NullInt64
			in, out, inB, outB     sql.NullInt64
		)
		if err := rows.Scan(&serverID, &gateway, &dir, &configured, &cid, &ip, &port, &rtt, &pend, &in, &out,
			&inB, &outB); err != nil {
			t.Fatal(err)
		}
		if serverID != v.Info.ID {
			t.Errorf("server_id = %q, want %q", serverID, v.Info.ID)
		}
		n := func(x sql.NullInt64) string {
			if !x.Valid {
				return "NULL"
			}
			return fmt.Sprint(x.Int64)
		}
		ipText := "NULL"
		if ip.Valid {
			ipText = fmt.Sprintf("%q", ip.String)
		}
		key := dir + " " + gateway
		if _, dup := got[key]; dup {
			key += " (2)"
		}
		got[key] = fmt.Sprintf("%v %s %s %s %s %s %s %s %s %s", configured, n(cid), ipText, n(port), n(rtt), n(pend),
			n(in), n(out), n(inB), n(outB))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		`outbound east "q" \`: fmt.Sprintf(`true %d "2001:db8::3" 7222 2500000 %d %d %d -1 %d`,
			int64(math.MaxInt64), math.MaxInt32, above53, int64(math.MaxInt64), int64(math.MinInt64)),
		"outbound west": "false NULL NULL NULL NULL NULL NULL NULL NULL NULL",
		"inbound east":  fmt.Sprintf(`false %d "" 0 NULL 0 0 0 0 0`, above53),
		"inbound north": `true 3 "10.0.0.4" 7222 NULL 0 0 0 0 0`,
	}
	if len(got) != len(want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s:\n got %s\nwant %s", k, got[k], w)
		}
	}
}
