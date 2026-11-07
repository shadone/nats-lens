package collect_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// Stored from a 3-node cluster, a client's queue subscription is one row,
// on the server the client is connected to, and joins that server's
// connections row of the same epoch by cid.
func TestSubscriptionsStoredFromCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	worker, err := nats.Connect(c.Servers[2].ClientURL(), nats.UserInfo(testcluster.AppUser, testcluster.AppPass),
		nats.Name("order-service"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	for range 3 {
		if _, err := worker.QueueSubscribe("orders.created", "order-service", func(*nats.Msg) {}); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.Flush(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	col := collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
		Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour),
		SubscriptionsInterval: 5 * time.Minute, SubszLimit: 10000,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	epoch, err := st.WriteSnapshot(ctx, col.Collect(ctx))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.DB().Query(`SELECT sv.server_name, s.account, s.queue, s.subs, c.name, c.account
FROM subscriptions s JOIN servers sv USING (server_id)
LEFT JOIN connections c ON c.epoch = s.epoch AND c.server_id = s.server_id AND c.cid = s.cid
WHERE s.epoch = ? AND s.subject = 'orders.created'`, epoch)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		var server, account, queue, name, connAccount string
		var subs int
		if err := rows.Scan(&server, &account, &queue, &subs, &name, &connAccount); err != nil {
			t.Fatalf("row %d: %v (a NULL name means the cid joins no connection)", n, err)
		}
		if server != "nats-2" || account != "APP" || queue != "order-service" || subs != 3 ||
			name != "order-service" || connAccount != "APP" {
			t.Errorf("row %s %s %s subs=%d, connection %s in %s", server, account, queue, subs, name, connAccount)
		}
	}
	if n != 1 {
		t.Fatalf("orders.created rows = %d, want 1", n)
	}
	var lists int
	if err := st.DB().QueryRow(`SELECT count(*) FROM subscription_lists WHERE epoch = ? AND listed = total`, epoch).Scan(&lists); err != nil || lists != 3 {
		t.Errorf("complete lists = %d, %v; want one per server", lists, err)
	}
}
