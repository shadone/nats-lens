package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// twoProcessStore is a collection in which two processes of nats-0 both
// answered (the old one shutting down, the new one up) beside nats-1:
// APP has 30 clients, 5 leaf nodes and 300 subscriptions on the old
// process, 20, 2 and 200 on the new one and 40, 0 and 400 on nats-1. With
// oldFirst the old process's rows are written first; it has the larger
// server ID either way.
func twoProcessStore(t *testing.T, last time.Time, oldFirst bool) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	procs := []struct {
		id                 string
		start              time.Time
		conns, leafs, subs int64
	}{{"Z-OLD", last.Add(-48 * time.Hour), 30, 5, 300}, {"A-NEW", last.Add(-time.Minute), 20, 2, 200}}
	if !oldFirst {
		procs[0], procs[1] = procs[1], procs[0]
	}
	r.epoch(1, last)
	r.add("accounts", map[string]any{"account": keyApp, "account_name": "APP", "is_system": false, "first_epoch": 1, "last_epoch": 1})
	stat := func(id string, conns, leafs, subs int64) {
		r.add("account_stats", map[string]any{"epoch": 1, "server_id": id, "account": keyApp, "conns": conns, "leafnodes": leafs,
			"subscriptions": subs, "js_storage": 0, "js_memory": 0, "js_streams": 0})
	}
	for _, p := range procs {
		r.add("servers", map[string]any{"server_id": p.id, "server_name": "nats-0", "first_epoch": 1, "last_epoch": 1,
			"first_seen_at": last, "last_seen_at": last, "start_time": p.start})
		r.add("server_stats", map[string]any{"epoch": 1, "server_id": p.id, "start_time": p.start})
		stat(p.id, p.conns, p.leafs, p.subs)
	}
	r.add("servers", map[string]any{"server_id": "S1", "server_name": "nats-1", "first_epoch": 1, "last_epoch": 1,
		"first_seen_at": last, "last_seen_at": last, "start_time": last.Add(-48 * time.Hour)})
	stat("S1", 40, 0, 400)
	r.add("account_details", map[string]any{"epoch": 1, "server_id": "S1", "account": keyApp, "looked_up_at": last.Add(time.Second),
		"is_system": false, "expired": false, "imports": 0, "exports": 0, "has_jwt": true, "limit_connections": 62})
	return st
}

// Two processes of one server name in the collection: nats-server keeps
// their connections apart until the old one shuts down, so the Accounts
// page and ListAccounts sum both shares, as account-001 counts, and the old
// process's 5 leaf nodes are the most on one process.
func TestAccountsAndAuthConnectionsOfTwoProcesses(t *testing.T) {
	last := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	for _, oldFirst := range []bool{true, false} {
		st := twoProcessStore(t, last, oldFirst)
		res := accountsOf(t, st, last.Add(30*time.Second), nil)
		if len(res.GetAccounts()) != 1 {
			t.Fatalf("accounts %v", res.GetAccounts())
		}
		a := res.GetAccounts()[0]
		if a.GetLimitLeafnodes() != 5 || a.GetLimitLeafnodesServer() != "nats-0" {
			t.Errorf("old first %v: %d leaf nodes counted on %q, want 5 on nats-0", oldFirst, a.GetLimitLeafnodes(), a.GetLimitLeafnodesServer())
		}
		if a.GetConnections() != 90 || a.GetLeafnodes() != 7 || a.GetSubscriptions() != 900 {
			t.Errorf("old first %v: connections %d, leaf nodes %d, subscriptions %d; want 90, 7, 900", oldFirst,
				a.GetConnections(), a.GetLeafnodes(), a.GetSubscriptions())
		}
		now := last.Add(30 * time.Second)
		_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return now }})
		ls := must(c.ListAccounts(ctx, connect.NewRequest(&v1.ListAccountsRequest{})))
		if len(ls.GetAccounts()) != 1 || ls.GetAccounts()[0].GetConnections() != 90 {
			t.Errorf("old first %v: ListAccounts %v, want 90 connections", oldFirst, ls.GetAccounts())
		}
	}
}
