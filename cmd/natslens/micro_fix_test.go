package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// refusalConf is one server: SHOP has a user with the grants of event
// capture only (no $SRV) and a full user that runs the service. These are
// test-only credentials.
const refusalConf = `
server_name: micro-1
listen: 127.0.0.1:%d
accounts {
  SYS { users: [ { user: sys, password: sys-test } ] }
  SHOP { jetstream: enabled, users: [
    { user: shop, password: shop-test },
    { user: shop-old, password: shop-old-test, permissions: {
      publish: { allow: ["$SYS.REQ.USER.INFO"] },
      subscribe: { allow: ["$JS.EVENT.>", "$SYS.ACCOUNT.CLIENT.AUTH.ERR", "_INBOX.>"] } } }
  ] }
}
system_account: SYS
jetstream: { store_dir: %q }
`

func refusalServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.conf")
	if err := os.WriteFile(path, fmt.Appendf(nil, refusalConf, testcluster.FreePort(t), filepath.Join(dir, "js")), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := server.ProcessConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("not ready")
	}
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL(), nats.UserInfo("shop", "shop-test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	svc, err := micro.AddService(nc, micro.Config{Name: "orders", Version: "1.0.0",
		Endpoint: &micro.EndpointConfig{Subject: "orders.get", Handler: micro.HandlerFunc(func(r micro.Request) { _ = r.Respond(nil) })}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	return s.ClientURL()
}

func refusalConfig(t *testing.T, dir, url string, events bool, accounts string) string {
	t.Helper()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`interval: 2s
timeout: 500ms
shutdown_grace: 1s
checks: { timeout: 1s }
data_dir: %s
listen: 127.0.0.1:%d
events: { enabled: %t }
nats: { url: %q, user: sys, password: sys-test }
accounts:
%s`, filepath.Join(dir, "data"), testcluster.FreePort(t), events, url, accounts)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

func refusalCount(t *testing.T, dataDir, q string) int {
	t.Helper()
	st, err := store.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.DB().QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// An account user with the grants event capture needs, without $SRV,
// gets its refused $SRV requests recorded in micro_requests and never as
// collector.subscription_error events, which the capture's error handler
// on the shared connection would make of them every epoch.
func TestMicroRefusalIsNoSubscriptionError(t *testing.T) {
	url := refusalServer(t)
	dir := t.TempDir()
	stop := startRun(t, refusalConfig(t, dir, url, true, "  - { name: SHOP, user: shop-old, password: shop-old-test }\n"), nil)
	time.Sleep(6500 * time.Millisecond)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	epochs := refusalCount(t, data, `SELECT count(*) FROM epochs`)
	refused := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE state = 'error'`)
	evs := refusalCount(t, data, `SELECT count(*) FROM events WHERE kind = 'collector.subscription_error' AND reason LIKE '%$SRV%'`)
	t.Logf("epochs %d, refused requests %d, subscription_error events naming $SRV %d", epochs, refused, evs)
	if evs > 0 {
		t.Errorf("%d collector.subscription_error events for refused $SRV requests", evs)
	}
}

// With event capture off, an account whose creds file is missing does not
// stop the system: epochs are taken, the account's requests are recorded
// as not connected, and another account is asked.
func TestMicroBadAccountWithoutEvents(t *testing.T) {
	url := refusalServer(t)
	dir := t.TempDir()
	accs := "  - { name: SHOP, creds: /nonexistent/shop.creds }\n  - { name: GOOD, user: shop, password: shop-test }\n"
	stop := startRun(t, refusalConfig(t, dir, url, false, accs), nil)
	time.Sleep(4500 * time.Millisecond)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if n := refusalCount(t, data, `SELECT count(*) FROM epochs`); n == 0 {
		t.Fatal("no epochs: an unusable account (events off) stops the whole system's collection")
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'SHOP'
  AND (state <> 'error' OR error <> 'account connection not connected')`); n != 0 {
		t.Errorf("%d SHOP requests not recorded as not connected", n)
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'SHOP'`); n == 0 {
		t.Error("SHOP has no requests recorded")
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'GOOD' AND request = 'STATS' AND state = 'ok'`); n == 0 {
		t.Error("GOOD was not asked")
	}
}

// Two entries under accounts for one account store its instances and
// endpoints once per epoch, under the first entry.
func TestMicroTwoLabelsOneAccount(t *testing.T) {
	url := refusalServer(t)
	dir := t.TempDir()
	accs := "  - { name: SHOP, user: shop, password: shop-test }\n  - { name: SHOP-2, user: shop, password: shop-test }\n"
	if err := run(context.Background(), []string{"once", "-config", refusalConfig(t, dir, url, false, accs), "-log-level", "error"}); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	dups := refusalCount(t, data, `SELECT count(*) FROM (SELECT epoch, account, service, instance_id, endpoint, count(*) n
FROM micro_endpoints GROUP BY ALL HAVING n > 1)`)
	stats := refusalCount(t, data, `SELECT count(*) FROM micro_stats`)
	if stats != 1 {
		t.Errorf("micro_stats rows %d for one instance", stats)
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label <> 'SHOP'`); n != 0 {
		t.Errorf("%d requests stored under the second entry", n)
	}
	if dups > 0 {
		t.Errorf("%d (epoch, account, service, instance, endpoint) keys stored more than once", dups)
	}
}
