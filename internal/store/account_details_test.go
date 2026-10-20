package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// The epoch's ACCOUNTZ lookups are stored one row each: a JWT account with
// its claims and limits, a config-mode account with NULL claims.
func TestAccountDetailsStored(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	snap.AccountDetails = append(snap.AccountDetails, collect.AccountDetails{
		Account: "APP", LookedUpAt: t0, ServerID: "NFAKESERVER0ID", Name: "APP", Imports: 0, Exports: 1,
	})
	epoch, err := s.WriteSnapshot(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM account_details WHERE epoch = ?`, epoch); n != 3 {
		t.Fatalf("account_details rows = %d, want 3", n)
	}
	var (
		name                     string
		system, expired, hasJWT  bool
		imports, exports         int
		issued, expires          sql.NullTime
		conns, leaf, disk, strms sql.NullInt64
	)
	if err := s.DB().QueryRow(`SELECT account_name, is_system, expired, imports, exports, has_jwt, jwt_issued_at, jwt_expires,
    limit_connections, limit_leafnodes, limit_js_disk, limit_js_streams
FROM account_details WHERE account = 'AFAKEAPPACCOUNT'`).
		Scan(&name, &system, &expired, &imports, &exports, &hasJWT, &issued, &expires, &conns, &leaf, &disk, &strms); err != nil {
		t.Fatal(err)
	}
	if name != "APP" || system || expired || imports != 1 || exports != 2 || !hasJWT ||
		!expires.Time.Equal(t0.Add(12*24*time.Hour)) || !issued.Time.Equal(t0.Add(-30*24*time.Hour)) ||
		conns.Int64 != 2000 || leaf.Int64 != -1 || disk.Int64 != 500<<30 || strms.Int64 != -1 {
		t.Errorf("JWT account row: %q %v %v %d %d %v %v %v %v %v %v %v",
			name, system, expired, imports, exports, hasJWT, issued, expires, conns, leaf, disk, strms)
	}
	// Without a JWT the claims and limits are not known: NULL, not 0.
	if n := count(t, s.DB(), `SELECT count(*) FROM account_details WHERE account = 'APP' AND NOT has_jwt
  AND jwt_issued_at IS NULL AND jwt_expires IS NULL AND limit_connections IS NULL AND limit_leafnodes IS NULL
  AND limit_subscriptions IS NULL AND limit_data IS NULL AND limit_payload IS NULL AND limit_js_disk IS NULL
  AND limit_js_memory IS NULL AND limit_js_streams IS NULL AND limit_js_consumers IS NULL AND exports = 1`); n != 1 {
		t.Errorf("config-mode row with NULL claims = %d, want 1", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM account_details WHERE account = 'SYS' AND is_system`); n != 1 {
		t.Errorf("system account rows = %d, want 1", n)
	}
	// A lookup also names and flags the account.
	if got := accountName(t, s, "AFAKEAPPACCOUNT"); got != "APP" {
		t.Errorf("account name = %q", got)
	}
	// An epoch without lookups has no rows.
	snap = fixtureSnapshot(t, t0.Add(time.Minute))
	snap.AccountDetails = nil
	epoch, err = s.WriteSnapshot(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM account_details WHERE epoch = ?`, epoch); n != 0 {
		t.Errorf("rows in an epoch without lookups = %d", n)
	}
}

// account_details goes with its epoch in a retention sweep.
func TestAccountDetailsSwept(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 2 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*24*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Sweep(ctx, t0.Add(24*time.Hour+time.Minute), time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM account_details WHERE epoch = 1`); n != 0 {
		t.Errorf("swept epoch keeps %d account_details rows", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM account_details WHERE epoch = 2`); n != 2 {
		t.Errorf("kept epoch has %d account_details rows, want 2", n)
	}
}

// userToken builds a JWT-shaped token; the signature is not checked.
func userToken(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"typ":"JWT","alg":"ed25519-nkey"}`)) + "." + enc([]byte(claims)) + "." + enc([]byte("sig"))
}

// A connection's user JWT is stored as a flag and its expiry; one that
// does not decode keeps the flag with a NULL expiry; the JWT itself is
// stored nowhere.
func TestConnectionUserJWT(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	exp := t0.Add(5 * 24 * time.Hour).Truncate(time.Second)
	good := userToken(`{"exp":` + strconv.FormatInt(exp.Unix(), 10) + `,"name":"pos-terminal"}`)
	mustApply(t, snap, collect.Connz, envelope(t, "nats-0", "NJWTSERVER0", server.Connz{ID: "NJWTSERVER0", Conns: []*server.ConnInfo{
		{Cid: 1, Account: "APP", JWT: good},
		{Cid: 2, Account: "APP", JWT: "eyJub3QiOiJqc29u.bm90LWpzb24.c2ln"},
		{Cid: 3, Account: "APP"},
	}}))
	if _, err := s.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB().Query(`SELECT cid, user_jwt, jwt_expires FROM connections ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		jwt bool
		exp sql.NullTime
	}
	got := map[int64]row{}
	for rows.Next() {
		var cid int64
		var r row
		if err := rows.Scan(&cid, &r.jwt, &r.exp); err != nil {
			t.Fatal(err)
		}
		got[cid] = r
	}
	if r := got[1]; !r.jwt || !r.exp.Time.Equal(exp) {
		t.Errorf("cid 1 = %+v, want a JWT expiring %s", r, exp)
	}
	if r := got[2]; !r.jwt || r.exp.Valid {
		t.Errorf("cid 2 = %+v, want a JWT with NULL expiry", r)
	}
	if r := got[3]; r.jwt || r.exp.Valid {
		t.Errorf("cid 3 = %+v, want no JWT", r)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM connections WHERE strpos(CAST(connections AS VARCHAR), 'eyJ') > 0`); n != 0 {
		t.Errorf("%d connection rows hold a JWT", n)
	}
}
