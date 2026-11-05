package checks

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// authError stores a client.auth_error event of account APP received at
// when, as event capture stores one: the user, address, name and reason of
// the client.
func (f *fx) authError(id int64, when time.Time, user, host, name, server, reason string) {
	f.t.Helper()
	f.authErrorIn(id, when, "APP", user, host, name, server, reason)
}

// authErrorIn is authError in another account; "" stores none.
func (f *fx) authErrorIn(id int64, when time.Time, account, user, host, name, server, reason string) {
	f.t.Helper()
	cols := m{"source": "sys", "type": "io.nats.server.advisory.v1.client_disconnect", "client_host": host,
		"server_name": server, "reason": reason}
	if account != "" {
		cols["account"] = account
	}
	if user != "" {
		cols["client_user"] = user
	}
	if name != "" {
		cols["client_name"] = name
	}
	f.event(id, when, "client.auth_error", cols)
}

// One user's failed logins within the lookback, from two addresses on two
// servers, reach the threshold and fire one finding; another user's one
// short of it does not.
func TestRepeatedAuthErrors(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	// label-printer: 16 failures in the 15 minutes, two pods, two servers.
	for i := range 16 {
		host, srv, pod := "10.20.4.31", "nats-0", "lp-a"
		if i%2 == 1 {
			host, srv, pod = "10.20.4.32", "nats-1", "lp-b"
		}
		f.authError(int64(i+1), at(12).Add(time.Duration(i)*50*time.Second), "label-printer", host, "label-printer@"+pod,
			srv, "Authentication Failure")
	}
	// stock-poller: 14, one short of 15.
	for i := range 14 {
		f.authError(int64(100+i), at(20).Add(time.Duration(i)*30*time.Second), "stock-poller", "10.0.0.6", "stock-poller", "nats-2",
			"Authentication Failure")
	}
	r := f.fires("client-006", 40, "label-printer in APP")
	want(t, r["label-printer in APP"], "warning",
		"16 failed logins in the last 15m (about 64 an hour) as label-printer in account APP from 10.20.4.31, 10.20.4.32 (nats-0, nats-1): "+
			"Authentication Failure; the last at 09:18:00 UTC. Client names: label-printer@lp-a, label-printer@lp-b.")
	if got := r["label-printer in APP"]; got.Value == nil || *got.Value != 16 || got.CauseEventID != 16 {
		t.Errorf("value %v, cause %d; want 16 and the latest event, 16", got.Value, got.CauseEventID)
	}
	if got := f.eval("client-006", 40, map[string]string{"min_failed_logins": "14"}); len(got) != 2 {
		t.Errorf("with min_failed_logins 14: %v", rowsText(got))
	}
}

// Without event capture there are no auth_error events and the check
// returns nothing; a disconnect event with an authentication reason (the
// server sends one for the refused client too) is not a failed login here.
func TestRepeatedAuthErrorsNeedEvents(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.fires("client-006", 40)
	for i := range 20 {
		f.event(int64(i+1), at(30), "client.disconnect", m{"source": "sys", "client_user": "alice", "client_host": "10.0.0.5",
			"reason": "Authentication Failure"})
	}
	f.fires("client-006", 40)
}

// Only events received within the lookback count: one received exactly at
// the window's start does not, one received at the epoch's start does.
func TestRepeatedAuthErrorsLookbackBoundary(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	start := at(40).Add(-15 * time.Minute)
	f.authError(1, start, "alice", "10.0.0.5", "", "nats-0", "Authentication Failure")
	for i := range 14 {
		f.authError(int64(i+2), start.Add(time.Duration(i+1)*time.Minute), "alice", "10.0.0.5", "", "nats-0", "Authentication Failure")
	}
	f.fires("client-006", 40)
	f.authError(20, at(40), "alice", "10.0.0.5", "", "nats-0", "Authentication Failure")
	r := f.fires("client-006", 40, "alice in APP")
	want(t, r["alice in APP"], "warning", "15 failed logins", "as alice in account APP from 10.0.0.5 (nats-0): Authentication Failure; the last at 09:19:30 UTC.")
	// A later event (received after the epoch started) is left for the next epoch.
	f.authError(21, at(40).Add(time.Second), "alice", "10.0.0.5", "", "nats-0", "Authentication Failure")
	if got := f.eval("client-006", 40); got["alice in APP"].Value == nil || *got["alice in APP"].Value != 15 {
		t.Errorf("an event after the epoch counted: %v", rowsText(got))
	}
}

// Tooling is left out: a nats-lens connection and a port-forwarded CLI on a
// loopback address. A client that gave no user is grouped as (no user),
// several reasons are listed, and the addresses beyond five are counted.
func TestRepeatedAuthErrorsToolingAndNoUser(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i := range 20 {
		f.authError(int64(i+1), at(30), "natslens", "10.0.0.1", "natslens-events", "nats-0", "Authentication Failure")
		f.authError(int64(i+101), at(30), "alice", "127.0.0.1", "NATS CLI Version 0.5.0", "nats-0", "Authentication Failure")
	}
	for i := range 16 {
		reason := "Authentication Failure"
		if i == 0 {
			reason = "Proxy Required"
		}
		f.authError(int64(i+201), at(31), "", "10.9.0."+string(rune('a'+i)), "", "nats-2", reason)
	}
	r := evalTooling(t, f, "client-006", 40, config.Checks{})
	if len(r) != 1 {
		t.Fatalf("client-006 returned %v, want (no user) in APP only", rowsText(r))
	}
	want(t, r["(no user) in APP"], "warning", "16 failed logins", "without a user in account APP from ",
		" and 11 more addresses (nats-2): Authentication Failure, Proxy Required;")
	if got := evalTooling(t, f, "client-006", 40, config.Checks{IncludeTooling: true}); len(got) != 3 {
		t.Errorf("with tooling: %v", rowsText(got))
	}
}

// A firing finding keeps firing while the count stays at or above
// resolve_ratio (0.8) of the threshold, 12 of 15, and resolves below it; a
// user that was not firing needs the full threshold, and a finding that
// fired at an older epoch than the previous one does not count as firing.
func TestRepeatedAuthErrorsHysteresis(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	firingAt := func(user string, epoch int64) {
		f.row("findings", m{"id": "client-006:" + user + " in APP", "check_code": "client-006", "entity_type": "client", "entity_key": user + " in APP",
			"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
			"transitioned_at": at(20), "last_seen_epoch": epoch, "updated_epoch": epoch, "detail": "x"})
	}
	firingAt("alice", 39)
	firingAt("bob", 39)
	firingAt("carol", 38)
	id := int64(0)
	add := func(user string, n int) {
		for range n {
			id++
			f.authError(id, at(30), user, "10.0.0.5", "", "nats-0", "Authentication Failure")
		}
	}
	add("alice", 12)
	add("bob", 11)
	add("carol", 14)
	add("dave", 14)
	r := f.fires("client-006", 40, "alice in APP")
	want(t, r["alice in APP"], "warning", "12 failed logins")
}

// A user is counted per account: the same user name failing 8 times in
// each of two accounts fires nothing, 15 times in one fires for that
// account only. An event without an account, or with the $G or N/A that
// nats-server's server-wide event names for a client refused before it
// got an account, is keyed (unknown account).
func TestRepeatedAuthErrorsPerAccount(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	id := int64(0)
	add := func(account, user string, n int) {
		for range n {
			id++
			f.authErrorIn(id, at(30), account, user, "10.1.0.1", "", "nats-0", "Authentication Failure")
		}
	}
	add("A", "app", 8)
	add("B", "app", 8)
	f.fires("client-006", 40)
	add("A", "app", 7)
	add("", "svc", 5)
	add("$G", "svc", 5)
	add("N/A", "svc", 5)
	r := f.fires("client-006", 40, "app in A", "svc in (unknown account)")
	want(t, r["app in A"], "warning", "15 failed logins", "as app in account A from 10.1.0.1")
	want(t, r["svc in (unknown account)"], "warning", "as svc in an unknown account from 10.1.0.1")
}

// nats-server reports a client that logged in with a token as the user
// "[REDACTED]"; the finding names it (token).
func TestRepeatedAuthErrorsToken(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i := range 15 {
		f.authError(int64(i+1), at(30), "[REDACTED]", "10.0.0.9", "", "nats-0", "Authentication Failure")
	}
	r := f.fires("client-006", 40, "(token) in APP")
	want(t, r["(token) in APP"], "warning", "15 failed logins", "with a token in account APP from 10.0.0.9")
}
