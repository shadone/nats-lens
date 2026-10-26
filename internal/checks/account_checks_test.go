package checks

import (
	"fmt"
	"testing"
	"time"
)

// account stores an account listed from epoch first to last.
func (f *fx) account(id, name string, first, last int64) {
	f.row("accounts", m{"account": id, "account_name": name, "is_system": false, "first_epoch": first, "last_epoch": last})
}

// lookup stores an account_details row of an account with a JWT, read at
// when in the epoch; cols add to and override the defaults.
func (f *fx) lookup(epoch int64, account string, when time.Time, cols m) {
	f.row("account_details", merge(m{"epoch": epoch, "server_id": "S0", "account": account, "looked_up_at": when,
		"is_system": false, "expired": false, "imports": 0, "exports": 0, "has_jwt": true}, cols))
}

// accountStats stores one server's account_stats row.
func (f *fx) accountStats(epoch int64, id, account string, cols m) {
	f.row("account_stats", merge(m{"epoch": epoch, "server_id": id, "account": account}, cols))
}

// lookupEpochs stores epoch 1 25 hours before t0, epoch 2 23 hours before
// and epoch 3 at t0, so a lookup in epoch 1 is too old for epoch 3 and one
// in epoch 2 is not.
func (f *fx) lookupEpochs(expected []string, missing ...string) {
	f.epochAt(1, t0.Add(-25*time.Hour), expected)
	f.epochAt(2, t0.Add(-23*time.Hour), expected)
	f.epochAt(3, t0, expected, missing...)
}

func TestAccountConnectionLimit(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0", "nats-1"})
	now := t0.Add(time.Second)
	// Each account: its limit, its connections on S0 and S1.
	for _, a := range []struct {
		id      string
		limit   any
		s0, s1  int
		details string // "" in epoch 3, "old" in epoch 2, "stale" in epoch 1, "nojwt", "none"
	}{
		{"NEAR", int64(100), 50, 40, ""},      // exactly 90%: warning
		{"FULL", int64(50), 30, 20, ""},       // at the limit: critical
		{"OK", int64(100), 50, 39, ""},        // 89%
		{"UNL", int64(-1), 3000, 2000, ""},    // no limit
		{"ZERO", int64(0), 1, 0, ""},          // a limit of 0 is left out
		{"CFG", nil, 10, 10, "nojwt"},         // config mode, no JWT
		{"STALE", int64(10), 10, 10, "stale"}, // lookup older than 24 hours
		{"OLD", int64(10), 5, 5, "old"},       // lookup 23 hours old still counts
		{"NEVER", nil, 10, 10, "none"},        // never looked up
	} {
		f.account(a.id, "name-"+a.id, 1, 3)
		f.accountStats(3, "S0", a.id, m{"conns": a.s0})
		f.accountStats(3, "S1", a.id, m{"conns": a.s1})
		switch a.details {
		case "":
			f.lookup(3, a.id, now, m{"limit_connections": a.limit})
		case "old":
			f.lookup(2, a.id, t0.Add(-23*time.Hour+time.Second), m{"limit_connections": a.limit})
		case "stale":
			f.lookup(1, a.id, t0.Add(-25*time.Hour+time.Second), m{"limit_connections": a.limit})
		case "nojwt":
			f.lookup(3, a.id, now, m{"has_jwt": false})
		}
	}
	// The newest lookup is read: an older one with a low limit is not.
	f.account("RAISED", "", 1, 3)
	f.accountStats(3, "S0", "RAISED", m{"conns": 20})
	f.lookup(2, "RAISED", t0.Add(-23*time.Hour+time.Second), m{"limit_connections": int64(10)})
	f.lookup(3, "RAISED", now, m{"limit_connections": int64(1000)})
	// An account the epoch does not list is not reported.
	f.account("GONE", "", 1, 2)
	f.accountStats(3, "S0", "GONE", m{"conns": 20})
	f.lookup(3, "GONE", now, m{"limit_connections": int64(10)})

	// Leaf nodes count on the server that holds them: nats-0 counts its 8
	// with every server's clients, 85 + 8 of 100. LEAFSUM's leaf nodes
	// spread over two servers: 80 + 6, though their sum would reach 92.
	f.server("S0", "nats-0", 1, 3, t0.Add(-48*time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 3, t0.Add(-48*time.Hour), "2.15.0")
	f.account("LEAF", "", 1, 3)
	f.accountStats(3, "S0", "LEAF", m{"conns": 40, "leafnodes": 8})
	f.accountStats(3, "S1", "LEAF", m{"conns": 45, "leafnodes": 3})
	f.lookup(3, "LEAF", now, m{"limit_connections": int64(100)})
	f.account("LEAFSUM", "", 1, 3)
	f.accountStats(3, "S0", "LEAFSUM", m{"conns": 40, "leafnodes": 6})
	f.accountStats(3, "S1", "LEAFSUM", m{"conns": 40, "leafnodes": 6})
	f.lookup(3, "LEAFSUM", now, m{"limit_connections": int64(100)})

	r := f.fires("account-001", 3, "NEAR", "FULL", "OLD", "LEAF")
	want(t, r["LEAF"], "warning", "LEAF: 93 of 100 connections (93.0%): 85 client connections and 8 leaf node connections on nats-0, "+
		"which counts its own leaf nodes against the limit.")
	want(t, r["NEAR"], "warning", "name-NEAR: 90 of 100 client connections (90.0%).")
	want(t, r["FULL"], "critical", "50 of 50 client connections (100.0%); at the limit, new client connections are refused.")
	want(t, r["OLD"], "critical", "10 of 10")
	if v := r["NEAR"].Value; v == nil || *v != 0.9 {
		t.Errorf("NEAR value = %v, want 0.9", v)
	}
	if r["FULL"].RelatedConfig["conn"] != "50" {
		t.Errorf("FULL related config = %v", r["FULL"].RelatedConfig)
	}
	r = f.eval("account-001", 3, map[string]string{"warn_ratio": "0.5", "crit_ratio": "0.95"})
	if len(r) != 6 || r["OK"].Severity != "warning" || r["NEAR"].Severity != "warning" {
		t.Fatalf("with warn_ratio 0.5: %s", rowsText(r))
	}
}

func TestAccountDiskLimit(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0", "nats-1"})
	now := t0.Add(time.Second)
	gib := int64(1) << 30
	for _, a := range []struct {
		id     string
		limit  any
		s0, s1 int64
		jwt    bool
	}{
		{"WARN", 100 * gib, 95 * gib, 80 * gib, true}, // the largest report, 95%; a sum would be over the limit
		{"EDGE", 100 * gib, 90 * gib, 90 * gib, true}, // exactly 90%
		{"FULL", 10 * gib, 10 * gib, 10 * gib, true},  // at the limit
		{"OK", 100 * gib, 89 * gib, 89 * gib, true},
		{"OFF", int64(0), 1 * gib, 1 * gib, true}, // JetStream disk off
		{"UNL", int64(-1), 900 * gib, 900 * gib, true},
		{"CFG", nil, 9 * gib, 9 * gib, false},
	} {
		f.account(a.id, "", 1, 3)
		f.accountStats(3, "S0", a.id, m{"js_storage": a.s0})
		f.accountStats(3, "S1", a.id, m{"js_storage": a.s1})
		f.lookup(3, a.id, now, m{"limit_js_disk": a.limit, "has_jwt": a.jwt})
	}
	f.account("STALE", "", 1, 3)
	f.accountStats(3, "S0", "STALE", m{"js_storage": 20 * gib})
	f.lookup(1, "STALE", t0.Add(-25*time.Hour+time.Second), m{"limit_js_disk": 10 * gib})

	r := f.fires("account-002", 3, "WARN", "EDGE", "FULL")
	want(t, r["WARN"], "warning", "WARN: JetStream file storage 95.0 GiB of 100.0 GiB (95.0%).")
	want(t, r["EDGE"], "warning", "(90.0%)")
	want(t, r["FULL"], "critical", "(100.0%); at the limit, new messages to file streams are refused.")
	if r["WARN"].RelatedConfig["disk_storage"] != "100.0 GiB" {
		t.Errorf("WARN related config = %v", r["WARN"].RelatedConfig)
	}
}

// inZones runs fn with DuckDB's session time zone set to zones west and
// east of UTC, since a time shifted by the zone would move an expiry by
// hours.
func inZones(t *testing.T, f *fx, fn func(zone string)) {
	t.Helper()
	for _, zone := range []string{"UTC", "America/Los_Angeles", "Pacific/Auckland"} {
		f.exec(`SET GLOBAL TimeZone = '` + zone + `'`)
		fn(zone)
	}
	f.exec(`SET GLOBAL TimeZone = 'UTC'`)
}

func TestAccountJWTExpiry(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0"})
	now := t0.Add(time.Second)
	const d = 24 * time.Hour
	for _, a := range []struct {
		id      string
		expires any
		expired bool
		jwt     bool
	}{
		{"D13", t0.Add(13 * d), false, true},           // warning
		{"D15", t0.Add(15 * d), false, true},           // not yet
		{"EDGE", t0.Add(14 * d), false, true},          // exactly 14 days: warning
		{"IN", t0.Add(14*d - time.Hour), false, true},  // an hour inside
		{"OUT", t0.Add(14*d + time.Hour), false, true}, // an hour outside
		{"D2", t0.Add(2 * d), false, true},             // critical
		{"D3", t0.Add(3 * d), false, true},             // exactly 3 days: critical
		{"PAST", t0.Add(-d), true, true},               // expired
		{"FLAG", nil, true, true},                      // the server says expired, no time
		{"NEVER", nil, false, true},                    // never expires
		{"CFG", nil, false, false},                     // no JWT
	} {
		f.account(a.id, "", 1, 3)
		f.lookup(3, a.id, now, m{"jwt_expires": a.expires, "expired": a.expired, "has_jwt": a.jwt})
	}
	f.account("STALE", "", 1, 3)
	f.lookup(1, "STALE", t0.Add(-25*time.Hour+time.Second), m{"jwt_expires": t0.Add(d)})

	inZones(t, f, func(zone string) {
		r := f.eval("account-003", 3)
		got := map[string]string{}
		for k, row := range r {
			got[k] = row.Severity
		}
		wantSev := map[string]string{"D13": "warning", "EDGE": "warning", "IN": "warning", "D2": "critical",
			"D3": "critical", "PAST": "critical", "FLAG": "critical"}
		if fmt.Sprint(got) != fmt.Sprint(wantSev) {
			t.Fatalf("%s: got %s, want %v", zone, rowsText(r), wantSev)
		}
		want(t, r["D13"], "warning", "D13: account JWT expires 2026-10-30 09:00 UTC, in 13.0 days.")
		want(t, r["PAST"], "critical", "account JWT expired 2026-10-16 09:00 UTC, 1.0 days ago.")
		want(t, r["FLAG"], "critical", "account JWT expired, as the server reports it.")
		if v := r["D13"].Value; v == nil || *v != 13 {
			t.Errorf("%s: D13 value = %v, want 13", zone, v)
		}
		if r["FLAG"].Value != nil {
			t.Errorf("%s: FLAG value = %v, want none", zone, *r["FLAG"].Value)
		}
	})
}

func TestUserJWTExpiry(t *testing.T) {
	f := newFx(t)
	f.epoch(1, three)
	f.epoch(2, three)
	const d = 24 * time.Hour
	f.account("SHOP", "SHOP", 1, 2)
	f.account("OTHER", "", 1, 2)
	cid := int64(0)
	conn := func(epoch int64, account, user string, expires any) {
		cid++
		f.row("connections", m{"epoch": epoch, "server_id": "S0", "cid": cid, "account": account, "authorized_user": user,
			"user_jwt": true, "jwt_expires": expires})
	}
	for range 3 {
		conn(2, "SHOP", "pos-terminal", t0.Add(5*d))
	}
	conn(2, "SHOP", "pos-terminal", t0.Add(20*d)) // a later JWT of the same user
	conn(2, "SHOP", "ops", t0.Add(2*d))
	conn(2, "SHOP", "app", nil)             // never expires
	conn(2, "OTHER", "batch", t0.Add(15*d)) // not yet
	conn(2, "OTHER", "edge", t0.Add(14*d+time.Hour))
	conn(1, "OTHER", "old", t0.Add(d)) // an earlier epoch's connection
	f.row("connections", m{"epoch": 2, "server_id": "S1", "cid": 1, "account": "OTHER", "authorized_user": "pw",
		"user_jwt": false})
	// More than five users are counted, not named.
	f.account("MANY", "", 1, 2)
	for i := range 7 {
		conn(2, "MANY", fmt.Sprintf("u%d", i), t0.Add(time.Duration(10+i)*time.Hour))
	}

	inZones(t, f, func(zone string) {
		r := f.eval("account-004", 2)
		if len(r) != 2 {
			t.Fatalf("%s: %s, want SHOP and MANY", zone, rowsText(r))
		}
		want(t, r["SHOP"], "critical", "SHOP: user ops expires 2026-10-19 09:00 UTC, in 2.0 days (1 connection); "+
			"user pos-terminal expires 2026-10-22 09:00 UTC, in 5.0 days (3 connections).")
		want(t, r["MANY"], "critical", "user u0 expires 2026-10-17 19:00 UTC, in 10.0 hours (1 connection)", "user u4",
			"; and 2 more users.")
		if v := r["SHOP"].Value; v == nil || *v != 2 {
			t.Errorf("%s: SHOP value = %v, want 2", zone, v)
		}
	})
	r := f.eval("account-004", 2, map[string]string{"crit_within": "1d", "warn_within": "16d"})
	want(t, r["SHOP"], "warning")
	want(t, r["OTHER"], "warning", "OTHER: user edge expires", "user batch expires")
}
