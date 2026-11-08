package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// microConfig writes a single-system configuration with two entries for
// APP under accounts: shop asks micro services with the scoped user, quiet
// has micro: false.
func microConfig(t *testing.T, dir, url string, events bool) string {
	t.Helper()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`interval: 2s
timeout: 500ms
shutdown_grace: 1s
checks: { timeout: 1s }
data_dir: %s
listen: 127.0.0.1:%d
events: { enabled: %t }
nats: { url: %q, user: %s, password: %s }
accounts:
  - name: shop
    user: %s
    password: %s
  - name: quiet
    user: %s
    password: %s
    micro: false
`, filepath.Join(dir, "data"), testcluster.FreePort(t), events, url, testcluster.SysUser, testcluster.SysPass,
		testcluster.ScopedAppUser, testcluster.ScopedAppPass, testcluster.AppUser, testcluster.AppPass)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

// startInventory runs a micro service in APP until the test ends.
func startInventory(t *testing.T, c *testcluster.Cluster) {
	t.Helper()
	svc, err := micro.AddService(c.Connect(0, testcluster.AppUser, testcluster.AppPass), micro.Config{Name: "inventory",
		Version: "3.1.0", Endpoint: &micro.EndpointConfig{Subject: "inventory.reserve",
			Handler: micro.HandlerFunc(func(r micro.Request) { _ = r.Respond(nil) })}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
}

// microRequests returns the stored requests as "label request state
// replies", oldest epoch first, and the connections named for the account
// shop in the last epoch.
func microRequests(t *testing.T, dataDir string) (requests []string, shopConns int) {
	t.Helper()
	st, err := store.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.DB().QueryRow(`SELECT count(*) FROM connections
WHERE epoch = (SELECT max(epoch) FROM epochs) AND name LIKE '%-shop'`).Scan(&shopConns); err != nil {
		t.Fatal(err)
	}
	rows, err := st.DB().Query(`SELECT label || ' ' || request || ' ' || state || ' ' || replies FROM micro_requests ORDER BY epoch, label, request`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, s)
	}
	return requests, shopConns
}

// natslens once asks the micro services of each account under accounts
// over a connection of its own, except an account with micro: false.
func TestOnceAsksMicroServices(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	c := testcluster.Start(t, 1)
	startInventory(t, c)
	dir := t.TempDir()
	if err := run(context.Background(), []string{"once", "-config", microConfig(t, dir, c.URL(0), true), "-log-level", "error"}); err != nil {
		t.Fatal(err)
	}
	got, _ := microRequests(t, filepath.Join(dir, "data"))
	if s := strings.Join(got, ", "); s != "shop INFO ok 1, shop STATS ok 1" {
		t.Errorf("requests: %s", s)
	}
}

// natslens run asks micro services over the event capture's account
// connections, and without event capture over connections it opens for
// them: one connection per account either way. An account with
// micro: false is not asked.
func TestRunAsksMicroServices(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	c := testcluster.Start(t, 1)
	startInventory(t, c)
	for _, events := range []bool{true, false} {
		t.Run(fmt.Sprintf("events %t", events), func(t *testing.T) {
			dir := t.TempDir()
			st, stop := startRunStore(t, microConfig(t, dir, c.URL(0), events))
			waitForStoredCount(t, st, "shop successful STATS in two epochs", `SELECT count(DISTINCT epoch) FROM micro_requests WHERE label = 'shop' AND request = 'STATS' AND state = 'ok'`, 2)
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			got, shopConns := microRequests(t, filepath.Join(dir, "data"))
			if shopConns != 1 {
				t.Errorf("%d connections for the account shop, want 1", shopConns)
			}
			if len(got) < 3 || got[0] != "shop INFO ok 1" || got[1] != "shop STATS ok 1" {
				t.Fatalf("requests: %q", got)
			}
			for _, r := range got[2:] {
				if r != "shop STATS ok 1" {
					t.Errorf("later request %q, want shop STATS ok 1", r)
				}
			}
		})
	}
}
