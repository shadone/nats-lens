package checks

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// authError stores a client.auth_error event received at when, as event
// capture stores one: the user, address, name and reason of the client.
func (f *fx) authError(id int64, when time.Time, user, host, name, server, reason string) {
	f.t.Helper()
	cols := m{"source": "sys", "type": "io.nats.server.advisory.v1.client_disconnect", "client_host": host,
		"server_name": server, "reason": reason}
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
			srv, "User Authentication Revoked")
	}
	// stock-poller: 14, one short of 15.
	for i := range 14 {
		f.authError(int64(100+i), at(20).Add(time.Duration(i)*30*time.Second), "stock-poller", "10.0.0.6", "stock-poller", "nats-2",
			"Authorization Violation")
	}
	r := f.fires("client-006", 40, "label-printer")
	want(t, r["label-printer"], "warning",
		"16 failed logins in the last 15m (about 64 an hour) as label-printer from 10.20.4.31, 10.20.4.32 (nats-0, nats-1): "+
			"User Authentication Revoked; the last at 09:18:00 UTC. Client names: label-printer@lp-a, label-printer@lp-b.")
	if got := r["label-printer"]; got.Value == nil || *got.Value != 16 || got.CauseEventID != 16 {
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
			"reason": "Authorization Violation"})
	}
	f.fires("client-006", 40)
}

// Only events received within the lookback count: one received exactly at
// the window's start does not, one received at the epoch's start does.
func TestRepeatedAuthErrorsLookbackBoundary(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	start := at(40).Add(-15 * time.Minute)
	f.authError(1, start, "alice", "10.0.0.5", "", "nats-0", "Authorization Violation")
	for i := range 14 {
		f.authError(int64(i+2), start.Add(time.Duration(i+1)*time.Minute), "alice", "10.0.0.5", "", "nats-0", "Authorization Violation")
	}
	f.fires("client-006", 40)
	f.authError(20, at(40), "alice", "10.0.0.5", "", "nats-0", "Authorization Violation")
	r := f.fires("client-006", 40, "alice")
	want(t, r["alice"], "warning", "15 failed logins", "as alice from 10.0.0.5 (nats-0): Authorization Violation; the last at 09:19:30 UTC.")
	// A later event (received after the epoch started) is left for the next epoch.
	f.authError(21, at(40).Add(time.Second), "alice", "10.0.0.5", "", "nats-0", "Authorization Violation")
	if got := f.eval("client-006", 40); got["alice"].Value == nil || *got["alice"].Value != 15 {
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
		f.authError(int64(i+1), at(30), "natslens", "10.0.0.1", "natslens-events", "nats-0", "Authorization Violation")
		f.authError(int64(i+101), at(30), "alice", "127.0.0.1", "NATS CLI Version 0.5.0", "nats-0", "Authorization Violation")
	}
	for i := range 16 {
		reason := "Authorization Violation"
		if i == 0 {
			reason = "Authentication Timeout"
		}
		f.authError(int64(i+201), at(31), "", "10.9.0."+string(rune('a'+i)), "", "nats-2", reason)
	}
	r := evalTooling(t, f, "client-006", 40, config.Checks{})
	if len(r) != 1 {
		t.Fatalf("client-006 returned %v, want (no user) only", rowsText(r))
	}
	want(t, r["(no user)"], "warning", "16 failed logins", "without a user from ",
		" and 11 more addresses (nats-2): Authentication Timeout, Authorization Violation;")
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
		f.row("findings", m{"id": "client-006:" + user, "check_code": "client-006", "entity_type": "client", "entity_key": user,
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
			f.authError(id, at(30), user, "10.0.0.5", "", "nats-0", "Authorization Violation")
		}
	}
	add("alice", 12)
	add("bob", 11)
	add("carol", 14)
	add("dave", 14)
	r := f.fires("client-006", 40, "alice")
	want(t, r["alice"], "warning", "12 failed logins")
}
