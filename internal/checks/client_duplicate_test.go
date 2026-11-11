package checks

import (
	"testing"

	"github.com/shadone/nats-lens/internal/config"
)

// dupSub stores a subscription row of epoch.
func (f *fx) dupSub(epoch int64, id string, cid int64, account, subject string, queue any, subs int64) {
	f.t.Helper()
	f.row("subscriptions", m{"epoch": epoch, "server_id": id, "account": account, "subject": subject, "queue": queue, "cid": cid,
		"subs": subs, "msgs": 0})
}

// dupList stores a server's subscription list of epoch.
func (f *fx) dupList(epoch int64, id string) {
	f.t.Helper()
	f.row("subscription_lists", m{"epoch": epoch, "server_id": id, "total": 10, "listed": 10})
}

// dupConn stores a client connection of epoch.
func (f *fx) dupConn(epoch int64, id string, cid int64, name, ip string) {
	f.t.Helper()
	f.row("connections", m{"epoch": epoch, "server_id": id, "cid": cid, "name": name, "ip": ip, "account": "APP"})
}

// dupCollection stores a subscription collection at epoch 40: search-api's
// pods hold search.query twice, one of them search.reindex three times;
// order-service holds each subscription once, and orders.place four times
// in its queue group for concurrency; pricing's micro service holds
// $SRV.PING twice; api-gateway's reply inbox is folded from three
// subscriptions; and a server's own client (cid 99, not in the connection
// list) holds $JS.API.> five times.
func dupCollection(f *fx) {
	f.dupList(40, "S0")
	f.dupList(40, "S1")
	f.dupConn(40, "S0", 1, "search-api@pod-1", "10.0.0.1")
	f.dupConn(40, "S1", 2, "search-api@pod-2", "10.0.0.2")
	f.dupConn(40, "S0", 3, "order-service@pod-1", "10.0.0.3")
	f.dupConn(40, "S0", 4, "api-gateway", "10.0.0.4")
	f.dupConn(40, "S0", 5, "pricing", "10.0.0.5")
	f.dupSub(40, "S0", 1, "APP", "search.query", nil, 2)
	f.dupSub(40, "S1", 2, "APP", "search.query", nil, 2)
	f.dupSub(40, "S1", 2, "APP", "search.reindex", nil, 3)
	f.dupSub(40, "S0", 3, "APP", "orders.place", "order-service", 4)
	f.dupSub(40, "S0", 3, "APP", "orders.cancel", nil, 1)
	f.dupSub(40, "S0", 5, "APP", "$SRV.PING", nil, 2)
	f.dupSub(40, "S0", 4, "APP", "_INBOX.abc.*", nil, 3)
	f.dupSub(40, "S0", 99, "APP", "$JS.API.>", nil, 5)
}

// A connection holding a plain subscription more than once fires, by
// application; one holding each once, a queue group's members on one
// connection, a micro service's $SRV. subjects, a folded inbox and a
// server's own subscriptions do not.
func TestDuplicateSubscription(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	dupCollection(f)
	r := f.fires("client-009", 40, "search-api")
	want(t, r["search-api"], "warning", "2 connections hold a subscription more than once in the subscription lists of 09:19:30 UTC: "+
		"search.query 2 times on 2 connections; search.reindex 3 times on 1 connection (nats-0, nats-1).")
	if v := r["search-api"].Value; v == nil || *v != 2 {
		t.Errorf("value %v, want 2", v)
	}
}

// Each server's newest list within the lookback counts: epochs after it
// keep reading it, a server that misses a later collection keeps its
// duplicates, a newer list of that server without them resolves it, and a
// list older than the lookback does not count.
func TestDuplicateSubscriptionCollection(t *testing.T) {
	f := newFx(t)
	f.cluster(110)
	dupCollection(f)
	f.fires("client-009", 41, "search-api")
	// At 45 only nats-0 answers, without the duplicate; pod-2 on nats-1
	// keeps it.
	f.dupList(45, "S0")
	f.dupConn(45, "S0", 1, "search-api@pod-1", "10.0.0.1")
	f.dupSub(45, "S0", 1, "APP", "search.query", nil, 1)
	r := f.fires("client-009", 46, "search-api")
	want(t, r["search-api"], "warning", "1 connection holds a subscription more than once in the subscription lists of 09:19:30 UTC: "+
		"search.query 2 times on 1 connection; search.reindex 3 times on 1 connection (nats-1).")
	// 60 epochs of 30 seconds after 40 nats-1's list lies outside the 30m lookback.
	f.fires("client-009", 101)
	// nats-1's newer list without the duplicate resolves it.
	f.dupList(47, "S1")
	f.dupConn(47, "S1", 2, "search-api@pod-2", "10.0.0.2")
	f.dupSub(47, "S1", 2, "APP", "search.query", nil, 1)
	f.fires("client-009", 48)
}

// A tooling connection, nats-lens's own or one from a loopback address,
// does not fire unless checks.include_tooling is set.
func TestDuplicateSubscriptionTooling(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	f.dupList(40, "S0")
	f.dupConn(40, "S0", 1, "natslens", "10.0.0.9")
	f.dupConn(40, "S0", 2, "nats-cli", "127.0.0.1")
	f.dupSub(40, "S0", 1, "SYS", "$SYS.SERVER.>", nil, 2)
	f.dupSub(40, "S0", 2, "APP", "orders.created", nil, 2)
	if got := evalTooling(t, f, "client-009", 40, config.Checks{}); len(got) != 0 {
		t.Errorf("tooling counted: %v", rowsText(got))
	}
	if got := evalTooling(t, f, "client-009", 40, config.Checks{IncludeTooling: true}); len(got) != 2 {
		t.Errorf("with tooling: %v", rowsText(got))
	}
}
