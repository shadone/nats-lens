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
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
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

const refusalRequests = `SELECT count(*) FROM micro_requests WHERE label = 'SHOP'
 AND request IN ('INFO', 'STATS') AND state = 'error' AND error LIKE '%Permissions Violation%'`
const heldRequests = `SELECT count(*) FROM micro_requests h WHERE label = 'SHOP' AND state = 'held'
 AND EXISTS (SELECT 1 FROM micro_requests r WHERE r.label = h.label AND r.request = h.request
 AND r.state = 'error' AND r.error LIKE '%Permissions Violation%' AND r.epoch < h.epoch)`

// An account user with the grants event capture needs, without $SRV,
// gets its refused $SRV requests recorded in micro_requests and never as
// collector.subscription_error events, which the capture's error handler
// on the shared connection would make of them every epoch.
func TestMicroRefusalIsNoSubscriptionError(t *testing.T) {
	url := refusalServer(t)
	dir := t.TempDir()
	st, stop := startRunStore(t, refusalConfig(t, dir, url, true, "  - { name: SHOP, user: shop-old, password: shop-old-test }\n"))
	waitForStoredCount(t, st, "two persisted collection epochs", `SELECT count(*) FROM epochs`, 2)
	waitForStoredCount(t, st, "SHOP $SRV permission refusals", refusalRequests, 1)
	waitForStoredCount(t, st, "SHOP held requests in a later epoch", heldRequests, 1)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	epochs := refusalCount(t, data, `SELECT count(*) FROM epochs`)
	refused := refusalCount(t, data, refusalRequests)
	held := refusalCount(t, data, heldRequests)
	evs := refusalCount(t, data, `SELECT count(*) FROM events WHERE kind = 'collector.subscription_error' AND reason LIKE '%$SRV%'`)
	t.Logf("epochs %d, refused requests %d, later held requests %d, subscription_error events naming $SRV %d", epochs, refused, held, evs)
	if err := microRefusalEvidence(epochs, refused, held, evs); err != nil {
		t.Fatal(err)
	}
}

// With event capture off, an account whose creds file is missing does not
// stop the system: epochs are taken, the account's requests are recorded
// as not connected, and another account is asked.
func TestMicroBadAccountWithoutEvents(t *testing.T) {
	url := refusalServer(t)
	dir := t.TempDir()
	accs := "  - { name: BROKEN, creds: /nonexistent/shop.creds }\n  - { name: GOOD, user: shop, password: shop-test }\n"
	st, stop := startRunStore(t, refusalConfig(t, dir, url, false, accs))
	waitForStoredCount(t, st, "BROKEN not-connected requests", `SELECT count(*) FROM micro_requests WHERE label = 'BROKEN' AND state = 'error' AND error = 'account connection not connected'`, 1)
	waitForStoredCount(t, st, "GOOD successful STATS", `SELECT count(*) FROM micro_requests WHERE label = 'GOOD' AND request = 'STATS' AND state = 'ok'`, 1)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if n := refusalCount(t, data, `SELECT count(*) FROM epochs`); n == 0 {
		t.Fatal("no epochs: an unusable account (events off) stops the whole system's collection")
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'BROKEN'
  AND (state <> 'error' OR error <> 'account connection not connected')`); n != 0 {
		t.Errorf("%d BROKEN requests not recorded as not connected", n)
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'BROKEN'`); n == 0 {
		t.Error("BROKEN has no requests recorded")
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
