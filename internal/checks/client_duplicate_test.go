package checks

import "testing"

// dupSub stores a subscription row of epoch.
func (f *fx) dupSub(epoch int64, id string, cid int64, account, subject string, queue any, subs int64) {
	f.t.Helper()
	f.row("subscriptions", m{"epoch": epoch, "server_id": id, "account": account, "subject": subject, "queue": queue, "cid": cid,
		"subs": subs, "msgs": 0})
}

// dupCollection stores a subscription collection at epoch 40: search-api's
// pods hold search.query in their queue group twice, one of them
// search.reindex three times; order-service holds each subscription once;
// api-gateway's reply inbox is folded from three subscriptions; and a
// server's own client (cid 99, not in the connection list) holds $JS.API.>
// five times.
func dupCollection(f *fx) {
	for _, id := range []string{"S0", "S1"} {
		f.row("subscription_lists", m{"epoch": 40, "server_id": id, "total": 10, "listed": 10})
	}
	f.row("connections", m{"epoch": 40, "server_id": "S0", "cid": 1, "name": "search-api@pod-1", "ip": "10.0.0.1", "account": "APP"})
	f.row("connections", m{"epoch": 40, "server_id": "S1", "cid": 2, "name": "search-api@pod-2", "ip": "10.0.0.2", "account": "APP"})
	f.row("connections", m{"epoch": 40, "server_id": "S0", "cid": 3, "name": "order-service@pod-1", "ip": "10.0.0.3", "account": "APP"})
	f.row("connections", m{"epoch": 40, "server_id": "S0", "cid": 4, "name": "api-gateway", "ip": "10.0.0.4", "account": "APP"})
	f.dupSub(40, "S0", 1, "APP", "search.query", "search-api", 2)
	f.dupSub(40, "S1", 2, "APP", "search.query", "search-api", 2)
	f.dupSub(40, "S1", 2, "APP", "search.reindex", nil, 3)
	f.dupSub(40, "S0", 3, "APP", "orders.place", "order-service", 1)
	f.dupSub(40, "S0", 3, "APP", "orders.cancel", nil, 1)
	f.dupSub(40, "S0", 4, "APP", "_INBOX.abc.*", nil, 3)
	f.dupSub(40, "S0", 99, "APP", "$JS.API.>", nil, 5)
}

// A connection holding a subject and queue more than once fires, by
// application; one holding each once, a folded inbox and a server's own
// subscriptions do not.
func TestDuplicateSubscription(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	dupCollection(f)
	r := f.fires("client-009", 40, "search-api")
	want(t, r["search-api"], "warning", "2 connections hold a subscription more than once in the subscription lists of 09:19:30 UTC: "+
		"search.query in queue group search-api 2 times on 2 connections; search.reindex 3 times on 1 connection (nats-0, nats-1).")
	if v := r["search-api"].Value; v == nil || *v != 2 {
		t.Errorf("value %v, want 2", v)
	}
}

// The newest collection within the lookback counts: epochs after it keep
// reading it, a newer collection without the duplicate resolves it, and
// one older than the lookback does not count.
func TestDuplicateSubscriptionCollection(t *testing.T) {
	f := newFx(t)
	f.cluster(110)
	dupCollection(f)
	f.fires("client-009", 41, "search-api")
	// 60 epochs of 30 seconds later the collection lies outside the 30m lookback.
	f.fires("client-009", 101)
	// A newer collection where each is held once.
	f.row("subscription_lists", m{"epoch": 45, "server_id": "S0", "total": 1, "listed": 1})
	f.row("connections", m{"epoch": 45, "server_id": "S0", "cid": 1, "name": "search-api@pod-1", "ip": "10.0.0.1", "account": "APP"})
	f.dupSub(45, "S0", 1, "APP", "search.query", "search-api", 1)
	f.fires("client-009", 46)
}
