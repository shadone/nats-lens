package collect_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
)

// A client in the global account: SUBSZ names the account "$G", as
// account STATZ does, while CONNZ leaves it empty. Both are stored as
// reported, so a subscription joins its connection on (epoch, server_id,
// cid) only, never on the account.
func TestSubszGlobalAccountStoredAsReported(t *testing.T) {
	sys := server.NewAccount("SYS")
	opts := &server.Options{
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		Accounts:      []*server.Account{sys},
		SystemAccount: "SYS",
		Users: []*server.User{
			{Username: "sys", Password: "sys", Account: sys},
			{Username: "g", Password: "g"},
		},
	}
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("not ready")
	}
	defer s.Shutdown()
	app, err := nats.Connect(s.ClientURL(), nats.UserInfo("g", "g"), nats.Name("global-client"))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.Subscribe("orders.created", func(*nats.Msg) {}); err != nil {
		t.Fatal(err)
	}
	_ = app.Flush()
	nc, err := nats.Connect(s.ClientURL(), nats.UserInfo("sys", "sys"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	col := collect.New(nc, collect.Options{
		Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour),
		SubscriptionsInterval: 5 * time.Minute, SubszLimit: 10000,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	epoch, err := st.WriteSnapshot(ctx, col.Collect(ctx))
	if err != nil {
		t.Fatal(err)
	}
	var subAcc, connAcc, name string
	if err := st.DB().QueryRow(`SELECT s.account, coalesce(c.account, '<null>'), c.name FROM subscriptions s
JOIN connections c USING (epoch, server_id, cid) WHERE s.epoch = ? AND s.subject = 'orders.created'`, epoch).
		Scan(&subAcc, &connAcc, &name); err != nil {
		t.Fatal(err)
	}
	if subAcc != "$G" || connAcc != "" || name != "global-client" {
		t.Errorf("subscriptions.account %q, connections.account %q (%s); want $G and empty", subAcc, connAcc, name)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM account_stats WHERE epoch = ? AND account = '$G'`, epoch).Scan(&n); err != nil || n != 1 {
		t.Errorf("account_stats rows for $G = %d, %v; want 1, the name SUBSZ uses", n, err)
	}
}
