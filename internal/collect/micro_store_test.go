package collect_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// A micro service in APP, asked over the account connection in an epoch
// that also collects the system account's endpoints, is stored: its
// request rows, its instance from INFO, its stats and endpoint counters.
// Its $SRV subscriptions are in the same epoch's subscription lists.
func TestMicroStoredWithEpoch(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 1)
	svcConn := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	svc, err := micro.AddService(svcConn, micro.Config{Name: "inventory", Version: "3.1.0",
		Endpoint: &micro.EndpointConfig{Subject: "inventory.reserve",
			Handler: micro.HandlerFunc(func(r micro.Request) { _ = r.Respond([]byte("ok")) })}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Stop() }()
	client := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	for range 4 {
		if _, err := client.Request("inventory.reserve", nil, 2*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	asker := c.Connect(0, testcluster.ScopedAppUser, testcluster.ScopedAppPass)
	col := collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
		Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour),
		SubscriptionsInterval: 5 * time.Minute, SubszLimit: 10000,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	col.SetAccountConns([]collect.AccountConn{{Label: "app", Conn: func() *nats.Conn { return asker }}})
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	snap := col.Collect(ctx)
	if snap.Partial() {
		t.Errorf("partial epoch: %v", snap.Errors)
	}
	epoch, err := st.WriteSnapshot(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	db := st.DB()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM micro_requests WHERE epoch = ? AND label = 'app' AND account = 'APP'
  AND state = 'ok' AND replies = 1`, epoch).Scan(&n); err != nil || n != 2 {
		t.Errorf("ok INFO and STATS rows = %d, %v", n, err)
	}
	var version, endpoints string
	if err := db.QueryRow(`SELECT version, endpoints::VARCHAR FROM micro_instances
WHERE epoch = ? AND account = 'APP' AND service = 'inventory'`, epoch).Scan(&version, &endpoints); err != nil {
		t.Fatal(err)
	}
	if version != "3.1.0" || endpoints != `[{"name":"default","subject":"inventory.reserve","queue_group":"q","metadata":null}]` {
		t.Errorf("instance %s %s", version, endpoints)
	}
	var subject, queue string
	var requests int
	if err := db.QueryRow(`SELECT e.subject, e.queue_group, e.num_requests FROM micro_endpoints e
JOIN micro_stats s USING (epoch, account, service, instance_id)
WHERE e.epoch = ? AND s.started IS NOT NULL`, epoch).Scan(&subject, &queue, &requests); err != nil {
		t.Fatal(err)
	}
	if subject != "inventory.reserve" || queue != "q" || requests != 4 {
		t.Errorf("endpoint %s %s %d", subject, queue, requests)
	}
	if err := db.QueryRow(`SELECT count(*) FROM subscriptions WHERE epoch = ? AND account = 'APP'
  AND subject = '$SRV.STATS.inventory.' || (SELECT instance_id FROM micro_stats WHERE epoch = ?)`, epoch, epoch).Scan(&n); err != nil || n != 1 {
		t.Errorf("the instance's $SRV.STATS subscription = %d, %v", n, err)
	}
}
