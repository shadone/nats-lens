package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// writeConnectionsByRow is the writer's earlier connection write, one
// INSERT per row, kept as the reference the JSON batch must match.
func (w *writer) writeConnectionsByRow(v *collect.ServerView) {
	cols := []string{
		"epoch", "server_id", "cid", "kind", "type", "name", "lang", "version", "account", "authorized_user",
		"ip", "port", "start_time", "last_activity", "rtt_ns", "pending_bytes", "in_msgs", "out_msgs",
		"in_bytes", "out_bytes", "stalls", "subscriptions", "tls_version", "name_tag", "user_jwt", "jwt_expires",
	}
	for _, c := range v.Connz.Conns {
		j, hasJWT := v.UserJWTs[c.Cid]
		w.insert("connections", cols, w.epoch, v.Info.ID, i64(c.Cid), c.Kind, c.Type, c.Name, c.Lang,
			c.Version, c.Account, c.AuthorizedUser, c.IP, c.Port, ts(c.Start), ts(c.LastActivity), durNS(c.RTT),
			c.Pending, c.InMsgs, c.OutMsgs, c.InBytes, c.OutBytes, c.Stalls, int64(c.NumSubs), c.TLSVersion, c.NameTag,
			hasJWT, ts(j.Expires))
	}
}

const above53 = 1<<53 + 1

// edgeConnzView holds connections whose values a JSON round trip could
// change: integers above 2^53 and at the int64 limit, a cid above it,
// strings with quotes, backslashes, HTML characters, unicode and control
// characters (NUL too), empty strings, zero times, times with nanoseconds,
// in another zone and before 1970, a missing and an unparsable RTT, and
// the three user JWT cases.
func edgeConnzView() *collect.ServerView {
	kolkata := time.FixedZone("IST", 5*3600+1800)
	nanos := time.Date(2026, 10, 16, 23, 17, 46, 123456789, kolkata)
	v := &collect.ServerView{
		Info: server.ServerInfo{ID: "NSERVER'0"},
		Connz: &server.Connz{Conns: []*server.ConnInfo{
			{
				Cid: above53, Kind: "Client", Type: "nats", Name: `say "hi" \ back\\slash 'single'`,
				Lang: "go", Version: "1.37.0", Account: "été-日本-\U0001D11E",
				AuthorizedUser: "tab\there\nnew line\r\x00\x01\x1f\x7f", IP: "<b>&amp;</b>", Port: 65535,
				Start: nanos, LastActivity: nanos.Add(999 * time.Nanosecond), RTT: "1.234567ms",
				Pending: math.MaxInt32, InMsgs: above53, OutMsgs: math.MaxInt64, InBytes: above53 + 2,
				OutBytes: math.MaxInt64 - 1, Stalls: above53, NumSubs: math.MaxUint32,
				TLSVersion: "1.3", NameTag: "tag     line separators",
			},
			{Cid: math.MaxUint64, RTT: "not a duration"},
			{Cid: math.MaxInt64, Start: time.Date(1969, 12, 31, 23, 59, 59, 999999999, time.UTC),
				LastActivity: time.Unix(0, 1), InMsgs: -1, Stalls: math.MinInt64},
			{Cid: 1, Name: "", Start: t0, LastActivity: t0},
		}},
		UserJWTs: map[uint64]collect.UserJWT{
			above53:       {Decoded: true, Expires: nanos.Add(24 * time.Hour)},
			math.MaxInt64: {Decoded: false},
		},
	}
	return v
}

func writeWith(t *testing.T, s *Store, epoch int64, v *collect.ServerView, write func(*writer, *collect.ServerView)) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := newWriter(ctx, tx, epoch, collect.NewSnapshot(t0))
	write(w, v)
	w.closeStatements()
	if w.err != nil {
		_ = tx.Rollback()
		t.Fatal(w.err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestConnectionRowsMatchRowByRow writes the same connections with the
// JSON batch (epoch 2) and with one INSERT per row (epoch 1), and
// requires every column of every row to be the same, NULLs included. The
// second server has more connections than one batch holds.
func TestConnectionRowsMatchRowByRow(t *testing.T) {
	s := openMem(t)
	views := []*collect.ServerView{edgeConnzView(), connzView("NSERVER1", connectionBatch+3, t0)}
	for _, v := range views {
		writeWith(t, s, 1, v, (*writer).writeConnectionsByRow)
		writeWith(t, s, 2, v, (*writer).writeConnections)
	}
	db := s.DB()
	if n := count(t, db, `SELECT count(*) FROM connections WHERE epoch = 2`); n != connectionBatch+7 {
		t.Fatalf("batch rows = %d, want %d", n, connectionBatch+7)
	}
	for _, q := range []string{
		`SELECT * EXCLUDE (epoch) FROM connections WHERE epoch = 1 EXCEPT ALL SELECT * EXCLUDE (epoch) FROM connections WHERE epoch = 2`,
		`SELECT * EXCLUDE (epoch) FROM connections WHERE epoch = 2 EXCEPT ALL SELECT * EXCLUDE (epoch) FROM connections WHERE epoch = 1`,
	} {
		if n := count(t, db, `SELECT count(*) FROM (`+q+`)`); n != 0 {
			t.Errorf("%d rows differ: %s", n, q)
		}
	}
}

// TestConnectionRowsEdgeValues reads the batch's edge rows back and
// compares them with the values given, so that a fault shared by both
// writes is caught too. Times must land as the same UTC instant whatever
// the session time zone: run it with a non-UTC TZ as well.
func TestConnectionRowsEdgeValues(t *testing.T) {
	s := openMem(t)
	v := edgeConnzView()
	writeWith(t, s, 1, v, (*writer).writeConnections)
	db := s.DB()

	c := v.Connz.Conns[0]
	var (
		serverID, name, account, user, ip, nameTag            string
		cid, inMsgs, outMsgs, inBytes, outBytes, stalls, subs int64
		pending, port                                         int64
		rtt                                                   int64
		start, last, expires                                  time.Time
		userJWT                                               bool
	)
	if err := db.QueryRow(`SELECT server_id, cid, name, account, authorized_user, ip, name_tag, port, start_time,
  last_activity, rtt_ns, pending_bytes, in_msgs, out_msgs, in_bytes, out_bytes, stalls, subscriptions, user_jwt, jwt_expires
FROM connections WHERE cid = ?`, int64(above53)).Scan(&serverID, &cid, &name, &account, &user, &ip, &nameTag, &port,
		&start, &last, &rtt, &pending, &inMsgs, &outMsgs, &inBytes, &outBytes, &stalls, &subs, &userJWT, &expires); err != nil {
		t.Fatal(err)
	}
	ints := []struct {
		col       string
		got, want int64
	}{
		{"cid", cid, above53}, {"port", port, 65535}, {"rtt_ns", rtt, 1_234_567}, {"pending_bytes", pending, math.MaxInt32},
		{"in_msgs", inMsgs, above53}, {"out_msgs", outMsgs, math.MaxInt64}, {"in_bytes", inBytes, above53 + 2},
		{"out_bytes", outBytes, math.MaxInt64 - 1}, {"stalls", stalls, above53}, {"subscriptions", subs, math.MaxUint32},
	}
	for _, x := range ints {
		if x.got != x.want {
			t.Errorf("%s = %d, want %d", x.col, x.got, x.want)
		}
	}
	strs := []struct{ col, got, want string }{
		{"server_id", serverID, v.Info.ID}, {"name", name, c.Name}, {"account", account, c.Account},
		{"authorized_user", user, c.AuthorizedUser}, {"ip", ip, c.IP}, {"name_tag", nameTag, c.NameTag},
	}
	for _, x := range strs {
		if x.got != x.want {
			t.Errorf("%s = %q, want %q", x.col, x.got, x.want)
		}
	}
	times := []struct {
		col       string
		got, want time.Time
	}{
		{"start_time", start, c.Start}, {"last_activity", last, c.LastActivity},
		{"jwt_expires", expires, v.UserJWTs[above53].Expires},
	}
	for _, x := range times {
		if want := x.want.Truncate(time.Microsecond); !x.got.Equal(want) {
			t.Errorf("%s = %v, want %v", x.col, x.got.UTC(), want.UTC())
		}
	}
	if !userJWT {
		t.Error("user_jwt = false, want true")
	}

	// The capped cid, the unparsable RTT, empty strings and zero times.
	var nulls int
	if err := db.QueryRow(`SELECT count(*) FROM connections WHERE cid = ? AND rtt_ns IS NULL AND kind = '' AND name = ''
  AND start_time IS NULL AND last_activity IS NULL AND user_jwt = false AND jwt_expires IS NULL`,
		int64(math.MaxInt64)).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 1 {
		t.Errorf("rows with cid MaxInt64 and NULL fields = %d, want 1 (the capped MaxUint64 cid)", nulls)
	}
	// Before 1970: truncated towards the earlier microsecond, as the driver does.
	var pre time.Time
	var jwt bool
	var noExpiry *time.Time
	if err := db.QueryRow(`SELECT start_time, user_jwt, jwt_expires FROM connections WHERE cid = ? AND in_msgs = -1`,
		int64(math.MaxInt64)).Scan(&pre, &jwt, &noExpiry); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC); !pre.Equal(want) {
		t.Errorf("start_time before 1970 = %v, want %v", pre.UTC(), want)
	}
	if !jwt || noExpiry != nil {
		t.Errorf("JWT that did not decode: user_jwt = %v, jwt_expires = %v, want true and NULL", jwt, noExpiry)
	}
}
