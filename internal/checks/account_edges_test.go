package checks

import (
	"testing"
	"time"
)

// Cases a reviewer of the account checks and server-016 wrote.

// A server gone for longer than 24 hours is no longer reported; one gone
// 23 hours is.
func TestCertificateOfServerGone(t *testing.T) {
	f := newFx(t)
	f.epochAt(1, t0.Add(-25*time.Hour), []string{"nats-0", "nats-1"})
	f.epochAt(2, t0.Add(-23*time.Hour), []string{"nats-0", "nats-1"})
	f.epochAt(3, t0, []string{"nats-0", "nats-1"}, "nats-0", "nats-1")
	f.server("S0", "nats-0", 1, 1, t0.Add(-48*time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 2, t0.Add(-48*time.Hour), "2.15.0")
	f.row("epoch_endpoints", m{"epoch": 1, "server_id": "S0", "endpoint": "VARZ", "state": "ok"})
	f.stats(1, "S0", m{"tls_cert_not_after": t0.Add(24 * time.Hour)})
	f.row("epoch_endpoints", m{"epoch": 2, "server_id": "S1", "endpoint": "VARZ", "state": "ok"})
	f.stats(2, "S1", m{"mqtt_tls_cert_not_after": t0.Add(-time.Minute)})
	r := f.fires("server-016", 3, "nats-1")
	want(t, r["nats-1"], "critical", "nats-1: mqtt certificate expired 2026-10-17 08:59 UTC, 1 minute ago (as VARZ reported it at 2026-10-16 10:00 UTC).")
}

// The server's expired flag with a future exp, a system account, and an
// account the epoch lists through connections only.
func TestAccountCheckEdges(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0"})
	now := t0.Add(time.Second)
	f.account("FLAG", "", 1, 3)
	f.lookup(3, "FLAG", now, m{"expired": true, "jwt_expires": t0.Add(40 * 24 * time.Hour)})
	f.row("accounts", m{"account": "SYS", "account_name": "SYS", "is_system": true, "first_epoch": 1, "last_epoch": 3})
	f.lookup(3, "SYS", now, m{"is_system": true, "jwt_expires": t0.Add(24 * time.Hour), "limit_connections": int64(2)})
	f.accountStats(3, "S0", "SYS", m{"conns": 2})
	r := f.fires("account-003", 3, "FLAG", "SYS")
	want(t, r["FLAG"], "critical", "FLAG: account JWT expires 2026-11-26 09:00 UTC, in 40 days; the server reports it expired.")
	if v := r["FLAG"].Value; v == nil || *v != 40 {
		t.Errorf("FLAG value %v", v)
	}
	f.fires("account-001", 3, "SYS")

	// A user JWT on an account the accounts table does not know.
	f.row("connections", m{"epoch": 3, "server_id": "S0", "cid": 1, "account": "GHOST", "authorized_user": "",
		"user_jwt": true, "jwt_expires": t0.Add(23*time.Hour + 58*time.Minute)})
	r = f.fires("account-004", 3, "GHOST")
	want(t, r["GHOST"], "critical", "GHOST: user (no user) expires")
}

// A lookup that is the newest at or before the epoch but whose account
// rows were read in a later epoch must not be taken.
func TestAccountLookupFromLaterEpoch(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0"})
	f.epochAt(4, t0.Add(time.Hour), []string{"nats-0"})
	f.account("A", "", 1, 4)
	f.accountStats(2, "S0", "A", m{"conns": 10})
	f.lookup(2, "A", t0.Add(-23*time.Hour+time.Second), m{"limit_connections": int64(100)})
	f.lookup(4, "A", t0.Add(time.Hour+time.Second), m{"limit_connections": int64(10)})
	f.fires("account-001", 2)
}
