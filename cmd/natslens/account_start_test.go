package main

import (
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/natsconn"
)

// With event capture on, an account whose creds file is missing does not
// stop the system: epochs are taken and system events captured, the
// account is recorded as not connected, and another account is asked.
func TestRunWithEventsStartsWithoutABadAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	url := refusalServer(t)
	dir := t.TempDir()
	accs := "  - { name: BROKEN, creds: /nonexistent/shop.creds }\n  - { name: GOOD, user: shop, password: shop-test }\n"
	conf := refusalConfig(t, dir, url, true, accs)
	reg, stop := runWithStatusStop(t, conf)
	waitForEpochs(t, reg, "default", 2)
	// A connection in the account, after the capture started, sends a
	// connect event to the system account.
	nc, err := nats.Connect(url, nats.UserInfo("shop", "shop-test"))
	if err != nil {
		t.Fatal(err)
	}
	nc.Close()
	waitForEpochs(t, reg, "default", 1)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if n := refusalCount(t, data, `SELECT count(*) FROM events WHERE source = 'sys'`); n == 0 {
		t.Error("no system events captured")
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM events WHERE kind = 'collector.not_connected'
  AND json_extract_string(raw, '$.connection') = 'BROKEN' AND reason LIKE '%shop.creds%'`); n != 1 {
		t.Errorf("%d collector.not_connected events for BROKEN, want 1", n)
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

// An account connection whose first dial failed is dialled again while the
// system collects; once it opens, its advisories are captured and its
// micro services asked.
func TestAccountConnectionCapturesOnceItOpens(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	var allow, opened atomic.Bool
	real := dialEvents
	dialEvents = func(c config.NATS, log *slog.Logger, extra ...nats.Option) (*nats.Conn, error) {
		if c.User != "shop" {
			return real(c, log, extra...)
		}
		if !allow.Load() {
			return nil, errTestDial
		}
		nc, err := real(c, log, extra...)
		if err == nil {
			opened.Store(true)
		}
		return nc, err
	}
	t.Cleanup(func() { dialEvents = real })

	url := refusalServer(t)
	dir := t.TempDir()
	conf := refusalConfig(t, dir, url, true, "  - { name: SHOP, user: shop, password: shop-test }\n")
	reg, stop := runWithStatusStop(t, conf)
	waitForEpochs(t, reg, "default", 2)
	allow.Store(true)
	waitFor(t, "the SHOP connection dialled again", 20*time.Second, opened.Load)
	// The subscriptions go out with the connect; give them a moment.
	time.Sleep(500 * time.Millisecond)
	nc, err := natsconn.Connect(config.NATS{URL: url, User: "shop", Password: "shop-test"}, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.CreateStream(t.Context(), jetstream.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}}); err != nil {
		t.Fatal(err)
	}
	waitForEpochs(t, reg, "default", 2)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if n := refusalCount(t, data, `SELECT count(*) FROM events WHERE source = 'jetstream' AND kind LIKE 'stream.%'`); n == 0 {
		t.Error("no stream advisory captured after the account connection opened")
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM events WHERE kind = 'collector.reconnected'
  AND json_extract_string(raw, '$.connection') = 'SHOP'`); n != 1 {
		t.Errorf("%d collector.reconnected events for SHOP, want 1", n)
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'SHOP' AND request = 'STATS' AND state = 'ok'`); n == 0 {
		t.Error("SHOP was not asked after its connection opened")
	}
	if n := refusalCount(t, data, `SELECT count(*) FROM micro_requests WHERE label = 'SHOP' AND error = 'account connection not connected'`); n == 0 {
		t.Error("SHOP was not recorded as not connected before its connection opened")
	}
}

var errTestDial = errors.New("test: dial refused")

// runWithStatusStop is runWithStatus that also returns the stop of the run,
// so that the store can be read after it.
func runWithStatusStop(t *testing.T, conf string) (api.Systems, func() error) {
	t.Helper()
	var mu sync.Mutex
	var reg api.Systems
	stop := startRun(t, conf, func(s api.Systems) {
		mu.Lock()
		defer mu.Unlock()
		reg = s
	})
	waitFor(t, "the systems", 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reg != nil
	})
	mu.Lock()
	defer mu.Unlock()
	return reg, stop
}
