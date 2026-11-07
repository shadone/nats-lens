package storeprov_test

import (
	"testing"
	"time"
)

// Reviewer case: nats-0 restarted in the collection; the old process
// answered ACCSTATZ with 30 clients of APP, the new process only JSZ (NULL
// conns). nats-server still counts the old process's 30 clients (strack is
// keyed by server ID), so both processes count and the page shows 70, not
// the new process's share alone.
func TestReviewAccountsPageRestartCountsBothProcesses(t *testing.T) {
	last := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	r := rows{t, st}
	r.epoch(1, last)
	r.add("accounts", map[string]any{"account": keyApp, "account_name": "APP", "is_system": false, "first_epoch": 1, "last_epoch": 1})
	for _, p := range []struct {
		id    string
		start time.Time
	}{{"Z-OLD", last.Add(-48 * time.Hour)}, {"A-NEW", last.Add(-time.Minute)}} {
		r.add("servers", map[string]any{"server_id": p.id, "server_name": "nats-0", "first_epoch": 1, "last_epoch": 1,
			"first_seen_at": last, "last_seen_at": last, "start_time": p.start})
		r.add("server_stats", map[string]any{"epoch": 1, "server_id": p.id, "start_time": p.start})
	}
	r.add("servers", map[string]any{"server_id": "S1", "server_name": "nats-1", "first_epoch": 1, "last_epoch": 1,
		"first_seen_at": last, "last_seen_at": last, "start_time": last.Add(-48 * time.Hour)})
	r.add("account_stats", map[string]any{"epoch": 1, "server_id": "Z-OLD", "account": keyApp, "conns": 30, "leafnodes": 0, "subscriptions": 1})
	r.add("account_stats", map[string]any{"epoch": 1, "server_id": "A-NEW", "account": keyApp, "js_storage": 10, "js_memory": 0, "js_streams": 1})
	r.add("account_stats", map[string]any{"epoch": 1, "server_id": "S1", "account": keyApp, "conns": 40, "leafnodes": 0, "subscriptions": 1})
	res := accountsOf(t, st, last.Add(30*time.Second), nil)
	if len(res.GetAccounts()) != 1 {
		t.Fatalf("accounts %v", res.GetAccounts())
	}
	if got := res.GetAccounts()[0].GetConnections(); got != 70 {
		t.Errorf("connections %d, want 70", got)
	}
}
