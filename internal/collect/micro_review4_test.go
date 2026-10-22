package collect

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// microInstance answers $SRV.STATS after *delay and $SRV.INFO at once as
// instance id of orders; stop unsubscribes both.
func microInstance(t *testing.T, nc *nats.Conn, id string, delay *atomic.Int64) (stop func()) {
	t.Helper()
	s1, err := nc.Subscribe("$SRV.STATS", func(m *nats.Msg) {
		go func() {
			time.Sleep(time.Duration(delay.Load()))
			_ = m.Respond(fmt.Appendf(nil, `{"type":"io.nats.micro.v1.stats_response","name":"orders","id":%q,"version":"1","started":"2026-10-01T00:00:00Z","endpoints":[]}`, id))
		}()
	})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := nc.Subscribe("$SRV.INFO", func(m *nats.Msg) {
		_ = m.Respond(fmt.Appendf(nil, `{"type":"io.nats.micro.v1.info_response","name":"orders","id":%q,"version":"1","endpoints":[]}`, id))
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()
	stop = func() { _ = s1.Unsubscribe(); _ = s2.Unsubscribe(); _ = nc.Flush() }
	t.Cleanup(stop)
	return stop
}

func sortedStatsIDs(m MicroAccount) []string {
	var out []string
	for _, s := range m.Stats {
		out = append(out, s.ID)
	}
	sort.Strings(out)
	return out
}

// A scale-down: 8 of 10 instances stop. At most three epochs wait the
// whole window, and the two left stay in the answers.
func TestMicroScaleDownCost(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var d atomic.Int64
	var stops []func()
	for i := range 10 {
		stops = append(stops, microInstance(t, svc, fmt.Sprintf("I%d", i), &d))
	}
	at := time.Now()
	w := 2 * time.Second
	c := microCollector(w, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	c.Collect(context.Background())
	for _, s := range stops[2:] {
		s()
	}
	slow := 0
	for e := range 5 {
		at = at.Add(30 * time.Second)
		start := time.Now()
		m := c.Collect(context.Background()).Micro[0]
		took := time.Since(start)
		t.Logf("epoch %d took %s: %v %s known=%d", e+2, took, sortedStatsIDs(m), askedOf(m), len(c.microKnown[m.Account]))
		if took > w*3/4 {
			slow++
		}
		if len(m.Stats) != 2 {
			t.Errorf("epoch %d: %v", e+2, sortedStatsIDs(m))
		}
	}
	t.Logf("%d epochs waited the window", slow)
	if slow > 3 {
		t.Errorf("a scale-down held up %d epochs", slow)
	}
}

// A restart: the old ID is gone and a new ID answers; every request of
// the following epochs succeeds, and their durations are logged.
func TestMicroRestartCost(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var d atomic.Int64
	microInstance(t, svc, "KEEP", &d)
	stopOld := microInstance(t, connectAs(t, url, "shop"), "OLD", &d)
	at := time.Now()
	w := 2 * time.Second
	c := microCollector(w, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	c.Collect(context.Background())
	stopOld()
	microInstance(t, connectAs(t, url, "shop"), "NEW", &d)
	for e := range 4 {
		at = at.Add(30 * time.Second)
		start := time.Now()
		m := c.Collect(context.Background()).Micro[0]
		t.Logf("epoch %d took %s: %v %s", e+2, time.Since(start), sortedStatsIDs(m), askedOf(m))
		for _, a := range m.Asked {
			if a.State != MicroOK {
				t.Errorf("epoch %d: %s %s %q", e+2, a.Request, a.State, a.Error)
			}
		}
	}
}

// A known instance stopped and a new instance's STATS reply comes at 60%
// of the window: the new instance's INFO is still asked in time.
func TestMicroLateNewInstanceGetsInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	var d, late atomic.Int64
	microInstance(t, connectAs(t, url, "shop"), "KEEP", &d)
	stopOld := microInstance(t, connectAs(t, url, "shop"), "OLD", &d)
	at := time.Now()
	w := 2 * time.Second
	c := microCollector(w, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	c.Collect(context.Background())
	stopOld()
	late.Store(int64(1200 * time.Millisecond))
	microInstance(t, connectAs(t, url, "shop"), "NEW", &late)
	at = at.Add(30 * time.Second)
	m := c.Collect(context.Background()).Micro[0]
	t.Logf("epoch 2: %v %s", sortedStatsIDs(m), askedOf(m))
	for _, a := range m.Asked {
		if a.Request == MicroInfo && a.State != MicroOK {
			t.Errorf("INFO for the new instance: %s %q", a.State, a.Error)
		}
	}
}
