package checks

import (
	"testing"
	"time"
)

// Reviewer case: nats-0 restarted during the collection. The old process
// answered ACCSTATZ (30 clients of the account) and then went away; the new
// process answered only JSZ, so its account_stats row has NULL conns and
// leafnodes. nats-server's other servers hold the old process's 30 clients
// in their remote count until its shutdown event, so both processes count
// and account-001 reads 30 + 40 = 70 of 70, not 40 from the new process
// alone.
func TestReviewRestartMidCollectionCountsBothProcesses(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0", "nats-1"})
	f.server("Z-OLD", "nats-0", 3, 3, t0.Add(-48*time.Hour), "2.15.0")
	f.server("A-NEW", "nats-0", 3, 3, t0.Add(-time.Minute), "2.15.0")
	f.server("S1", "nats-1", 1, 3, t0.Add(-48*time.Hour), "2.15.0")
	f.stats(3, "Z-OLD", m{"start_time": t0.Add(-48 * time.Hour)})
	f.stats(3, "A-NEW", m{"start_time": t0.Add(-time.Minute)})
	f.account("APP", "", 1, 3)
	f.accountStats(3, "Z-OLD", "APP", m{"conns": 30, "leafnodes": 0})
	f.accountStats(3, "A-NEW", "APP", m{"js_storage": 10, "js_streams": 1}) // JSZ only
	f.accountStats(3, "S1", "APP", m{"conns": 40, "leafnodes": 0})
	f.lookup(3, "APP", t0.Add(time.Second), m{"limit_connections": int64(70)})
	r := f.fires("account-001", 3, "APP")
	want(t, r["APP"], "critical", "70 of 70 client connections")
}

// Reviewer case: the rule against nats-server 2.15 accounts.go
// MaxTotalConnectionsReached: len(a.clients)-sysclients (local clients plus
// local leaf nodes) + nrclients (other servers' STATZ Conns). nats-1 holds 3
// leaf nodes, nats-2 holds 5; clients 10+10+10. nats-2 refuses first at
// 30+5 = 35. A server whose share carries leafnodes but NULL conns (JSZ
// merged with nothing) is still read for its leaf nodes.
func TestReviewLeafsOnSeveralServers(t *testing.T) {
	f := newFx(t)
	f.lookupEpochs([]string{"nats-0", "nats-1", "nats-2"})
	for i, n := range []string{"nats-0", "nats-1", "nats-2"} {
		id := []string{"S0", "S1", "S2"}[i]
		f.server(id, n, 1, 3, t0.Add(-48*time.Hour), "2.15.0")
	}
	f.account("L", "", 1, 3)
	f.accountStats(3, "S0", "L", m{"conns": 10, "leafnodes": 0})
	f.accountStats(3, "S1", "L", m{"conns": 10, "leafnodes": 3})
	f.accountStats(3, "S2", "L", m{"conns": 10, "leafnodes": 5})
	f.lookup(3, "L", t0.Add(time.Second), m{"limit_connections": int64(35)})
	r := f.fires("account-001", 3, "L")
	want(t, r["L"], "critical", "35 of 35 connections", "5 leaf node connections on nats-2")
}
