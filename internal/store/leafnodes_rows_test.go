package store

import (
	"database/sql"
	"math"
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// writeLeafnodesByRow is the writer's earlier leaf node write, one INSERT
// per row, kept as the reference the JSON batch must match.
func (w *writer) writeLeafnodesByRow(v *collect.ServerView) {
	cols := []string{
		"epoch", "server_id", "lid", "name", "is_spoke", "account", "ip", "port", "rtt_ns",
		"in_msgs", "out_msgs", "in_bytes", "out_bytes", "subscriptions", "compression",
	}
	for _, l := range v.Leafz.Leafs {
		if l == nil {
			continue
		}
		w.insert("leafnodes", cols, w.epoch, v.Info.ID, i64(l.ID), l.Name, l.IsSpoke, l.Account, l.IP, l.Port,
			durNS(l.RTT), l.InMsgs, l.OutMsgs, l.InBytes, l.OutBytes, int64(l.NumSubs), l.Compression)
	}
}

// edgeLeafzView holds leaf nodes whose values a JSON round trip could
// change: a spoke and a hub, integers above 2^53, at the int64 limits and
// a lid beyond it, strings with quotes, backslashes, unicode and control
// characters (NUL too), empty strings, an IPv6 address, a missing and an
// unparsable RTT, and a null entry.
func edgeLeafzView() *collect.ServerView {
	return &collect.ServerView{
		Info: server.ServerInfo{ID: "NHUB'0\"\\"},
		Leafz: &server.Leafz{Leafs: []*server.LeafInfo{
			{
				ID: above53, Name: `shop "east" \ 'q' été-日本-` + "\U0001D11E", IsSpoke: true,
				Account: "APP\x00\t\n", IP: "2001:db8::1", Port: 65535, RTT: "1.234567ms",
				InMsgs: above53, OutMsgs: math.MaxInt64, InBytes: above53 + 2, OutBytes: math.MaxInt64 - 1,
				NumSubs: math.MaxUint32, Compression: "s2_best",
			},
			nil,
			{ID: math.MaxUint64, RTT: "not a duration", InMsgs: -1, OutBytes: math.MinInt64},
			{ID: 1, Name: "hub-1", IsSpoke: false, Account: "", IP: "10.0.0.7", Port: 7422, RTT: "",
				Compression: "off"},
		}},
	}
}

// TestLeafnodeRowsMatchRowByRow writes the same leaf nodes with the JSON
// batch (epoch 2) and with one INSERT per row (epoch 1), and requires
// every column of every row to be the same, NULLs included. The second
// server has more leaf nodes than one batch holds.
func TestLeafnodeRowsMatchRowByRow(t *testing.T) {
	s := openMem(t)
	views := []*collect.ServerView{edgeLeafzView(), leafzView("NHUB1", jsonBatch+3)}
	for _, v := range views {
		writeWith(t, s, 1, v, (*writer).writeLeafnodesByRow)
		writeWith(t, s, 2, v, (*writer).writeLeafnodes)
	}
	db := s.DB()
	if n := count(t, db, `SELECT count(*) FROM leafnodes WHERE epoch = 2`); n != jsonBatch+6 {
		t.Fatalf("batch rows = %d, want %d", n, jsonBatch+6)
	}
	for _, q := range []string{
		`SELECT * EXCLUDE (epoch) FROM leafnodes WHERE epoch = 1 EXCEPT ALL SELECT * EXCLUDE (epoch) FROM leafnodes WHERE epoch = 2`,
		`SELECT * EXCLUDE (epoch) FROM leafnodes WHERE epoch = 2 EXCEPT ALL SELECT * EXCLUDE (epoch) FROM leafnodes WHERE epoch = 1`,
	} {
		if n := count(t, db, `SELECT count(*) FROM (`+q+`)`); n != 0 {
			t.Errorf("%d rows differ: %s", n, q)
		}
	}
}

// leafnodeStored is a leafnodes row as read back.
type leafnodeStored struct {
	serverID, name, account, ip, compression string
	lid, inMsgs, outMsgs, inBytes, outBytes  int64
	subs, port                               int64
	rtt                                      sql.NullInt64
	isSpoke                                  bool
}

// TestLeafnodeRowsEdgeValues reads the batch's edge rows back and
// compares them with fixed expected rows, so that a fault shared by both
// writes is caught too.
func TestLeafnodeRowsEdgeValues(t *testing.T) {
	s := openMem(t)
	v := edgeLeafzView()
	writeWith(t, s, 1, v, (*writer).writeLeafnodes)
	rows, err := s.DB().Query(`SELECT server_id, lid, name, is_spoke, account, ip, port, rtt_ns, in_msgs, out_msgs,
  in_bytes, out_bytes, subscriptions, compression FROM leafnodes ORDER BY lid, in_msgs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []leafnodeStored
	for rows.Next() {
		var r leafnodeStored
		if err := rows.Scan(&r.serverID, &r.lid, &r.name, &r.isSpoke, &r.account, &r.ip, &r.port, &r.rtt,
			&r.inMsgs, &r.outMsgs, &r.inBytes, &r.outBytes, &r.subs, &r.compression); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	id := v.Info.ID
	want := []leafnodeStored{
		{serverID: id, lid: 1, name: "hub-1", ip: "10.0.0.7", port: 7422, compression: "off"},
		{serverID: id, lid: above53, name: v.Leafz.Leafs[0].Name, isSpoke: true, account: "APP\x00\t\n",
			ip: "2001:db8::1", port: 65535, rtt: sql.NullInt64{Int64: 1_234_567, Valid: true},
			inMsgs: above53, outMsgs: math.MaxInt64, inBytes: above53 + 2, outBytes: math.MaxInt64 - 1,
			subs: math.MaxUint32, compression: "s2_best"},
		{serverID: id, lid: math.MaxInt64, inMsgs: -1, outBytes: math.MinInt64},
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
