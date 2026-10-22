package collect

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// statsResponder answers $SRV.STATS as instance id of orders after
// *delay, as an instance behind a slow gateway or in a GC pause would.
func statsResponder(t *testing.T, nc *nats.Conn, id string, delay *atomic.Int64) {
	t.Helper()
	sub, err := nc.Subscribe("$SRV.STATS", func(m *nats.Msg) {
		go func() {
			time.Sleep(time.Duration(delay.Load()))
			_ = m.Respond(fmt.Appendf(nil, `{"type":"io.nats.micro.v1.stats_response","name":"orders","id":%q,"version":"1","started":"2026-10-01T00:00:00Z","endpoints":[]}`, id))
		}()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	_ = nc.Flush()
}

// An instance that answered the previous STATS is waited for, within the
// window, although its reply comes 400 ms after the other instance's,
// longer than the quiet period. An instance never seen before is not
// waited for.
func TestMicroWaitsForKnownInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var near, far atomic.Int64
	statsResponder(t, svc, "NEAR", &near)
	statsResponder(t, svc, "FAR", &far)
	at := time.Now()
	c := microCollector(3*time.Second, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	ids := func() []string {
		var out []string
		for _, s := range c.Collect(context.Background()).Micro[0].Stats {
			out = append(out, s.ID)
		}
		return out
	}
	if got := ids(); len(got) != 2 {
		t.Fatalf("first epoch, both fast: %v", got)
	}
	far.Store(int64(400 * time.Millisecond))
	at = at.Add(30 * time.Second)
	start := time.Now()
	if got := ids(); len(got) != 2 {
		t.Errorf("FAR answered the previous STATS and 400 ms late now: STATS kept %v", got)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("collect took %s: it waited past the known instances", took)
	}
}

// An instance restarts with a new ID: the epoch that sees it asks INFO
// of the new instance in time, rather than waiting the whole window for
// the old ID first.
func TestMicroRestartAsksInfoInTime(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	a := startOrders(t, url, "1.0.0")
	startOrders(t, url, "1.0.0")
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at, fixedConn("SHOP", connectAs(t, url, "shop")))
	m := c.Collect(context.Background()).Micro[0]
	t.Logf("epoch 1: %s", askedOf(m))
	_ = a.Stop()
	startOrders(t, url, "1.0.1")
	at = at.Add(30 * time.Second)
	start := time.Now()
	m = c.Collect(context.Background()).Micro[0]
	took := time.Since(start)
	t.Logf("epoch 2 (%s): %s", took, askedOf(m))
	if took > 1800*time.Millisecond {
		t.Errorf("epoch 2 took %s of a 2s window: it waited for the old ID", took)
	}
	for _, r := range m.Asked {
		if r.State != MicroOK {
			t.Errorf("after a restart: %s %s %q", r.Request, r.State, r.Error)
		}
	}
	at = at.Add(30 * time.Second)
	m = c.Collect(context.Background()).Micro[0]
	t.Logf("epoch 3: %s", askedOf(m))
}
