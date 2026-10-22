package collect

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// Two labels for SHOP: while the first is down the second stands in, and
// once the first is back it alone is kept.
func TestMicroStandInWhenFirstDown(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.0.0")
	a, b := connectAs(t, url, "shop"), connectAs(t, url, "shop-asker")
	var aDown atomic.Bool
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at,
		AccountConn{Label: "A", Conn: func() *nats.Conn {
			if aDown.Load() {
				return nil
			}
			return a
		}}, fixedConn("B", b))
	show := func(name string) []MicroAccount {
		at = at.Add(30 * time.Second)
		ms := c.Collect(context.Background()).Micro
		for _, m := range ms {
			t.Logf("%s: %s account=%s %s stats=%d", name, m.Label, m.Account, askedOf(m), len(m.Stats))
		}
		return ms
	}
	show("1 both up")
	show("2 both up")
	aDown.Store(true)
	if ms := show("3 A down"); len(ms) != 1 || ms[0].Label != "B" || len(ms[0].Stats) != 1 {
		t.Errorf("A down: B does not stand in alone")
	}
	aDown.Store(false)
	if ms := show("4 A up"); len(ms) != 1 || ms[0].Label != "A" || len(ms[0].Stats) != 1 {
		t.Errorf("A back: %d entries", len(ms))
	}
}

// A label equal to the account's name whose lookup is refused, and a
// second label whose lookup resolves to that name: the instance is kept
// once per epoch.
func TestMicroRefusedLabelEqualsAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.0.0")
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at,
		fixedConn("SHOP", connectAs(t, url, "shop-nolookup")), fixedConn("shop-main", connectAs(t, url, "shop")))
	for e := range 2 {
		at = at.Add(30 * time.Second)
		ms := c.Collect(context.Background()).Micro
		stats := map[string]int{}
		for _, m := range ms {
			t.Logf("epoch %d: %s account=%s %s stats=%d", e, m.Label, m.Account, askedOf(m), len(m.Stats))
			stats[m.Account] += len(m.Stats)
		}
		if stats["SHOP"] > 1 {
			t.Errorf("epoch %d: one instance stored %d times under account SHOP", e, stats["SHOP"])
		}
	}
}

// A label standing in for its account waits for the instances that
// answered only under the other label: the known instances are the
// account's, not the label's.
func TestMicroStandInKnowsTheAccountsInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var near, far atomic.Int64
	statsResponder(t, svc, "NEAR", &near)
	a, b := connectAs(t, url, "shop"), connectAs(t, url, "shop-asker")
	var aDown atomic.Bool
	at := time.Now()
	c := microCollector(3*time.Second, 5*time.Minute, &at,
		AccountConn{Label: "A", Conn: func() *nats.Conn {
			if aDown.Load() {
				return nil
			}
			return a
		}}, fixedConn("B", b))
	// Both labels ask once; then only A asks, and FAR starts.
	c.Collect(context.Background())
	at = at.Add(30 * time.Second)
	statsResponder(t, svc, "FAR", &far)
	c.Collect(context.Background())
	at = at.Add(30 * time.Second)
	aDown.Store(true)
	far.Store(int64(400 * time.Millisecond))
	ms := c.Collect(context.Background()).Micro
	if len(ms) != 1 || ms[0].Label != "B" || len(ms[0].Stats) != 2 {
		for _, m := range ms {
			t.Logf("%s: %s stats=%d", m.Label, askedOf(m), len(m.Stats))
		}
		t.Error("B standing in did not wait for FAR, which answered under A")
	}
}
