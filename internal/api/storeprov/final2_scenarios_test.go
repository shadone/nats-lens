package storeprov_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/config"
)

// A busy pool of two slots whose workers are alive and finish a pair of
// messages every 6 seconds is saturated, not stopped: consumer-003 never
// goes critical. After the review's TestFinal2_BusyPool.
func TestFinal2_BusyPool(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	g := newRig(t, "", false, config.Checks{})
	g.stream("order-jobs", 3, "oj.>", nil)
	cn, err := g.js.CreateOrUpdateConsumer(g.ctx, "order-jobs", jetstream.ConsumerConfig{Durable: "resize-worker",
		AckPolicy: jetstream.AckExplicitPolicy, MaxAckPending: 2, AckWait: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for range 40 {
		_, _ = g.js.Publish(g.ctx, "oj.x", []byte("job"))
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			b, err := cn.FetchNoWait(2)
			if err == nil {
				var ms []jetstream.Msg
				for m := range b.Messages() {
					ms = append(ms, m)
				}
				select {
				case <-stop:
					return
				case <-time.After(6 * time.Second):
				}
				for _, m := range ms {
					_ = m.Ack()
				}
			} else {
				time.Sleep(time.Second)
			}
		}
	}()
	for range 20 {
		g.epoch("busy pool")
		if f, ok := g.last["consumer-003:APP/order-jobs/resize-worker"]; ok && f[:8] == "critical" {
			t.Fatalf("a pool that acknowledges is critical: %s", f)
		}
		time.Sleep(700 * time.Millisecond)
	}
	g.detail("consumer-003:APP/order-jobs/resize-worker")
}

const final2Inv = `accounts:
  - name: APP
    streams:
      - name: order-archive
        replicas: 3
        consumers:
          - name: analytics-aggregator
            max_ack_pending: 8000
          - name: active-jobs
      - name: audit-archive
        replicas: 3
        consumers:
          - name: usage-aggregator
      - name: order-jobs
        replicas: 3
        consumers:
          - name: job-cache-projector
          - name: resize-worker
            max_ack_pending: 2
`

func (g *rig) incidentAssets(r1 bool) {
	g.t.Helper()
	for _, a := range []struct{ stream, subj string }{{"order-archive", "oa.>"}, {"audit-archive", "aa.>"}, {"order-jobs", "oj.>"}} {
		g.stream(a.stream, 3, a.subj, nil)
	}
	cons := []struct{ stream, name string }{
		{"order-archive", "analytics-aggregator"}, {"order-archive", "active-jobs"},
		{"audit-archive", "usage-aggregator"}, {"order-jobs", "job-cache-projector"},
		{"order-jobs", "resize-worker"}, {"order-jobs", "legacy-worker"},
	}
	for _, c := range cons {
		cc := jetstream.ConsumerConfig{Durable: c.name, AckPolicy: jetstream.AckExplicitPolicy, MaxAckPending: 8000}
		if c.name == "resize-worker" {
			cc.MaxAckPending = 2
		}
		if r1 && c.name != "resize-worker" {
			cc.Replicas = 1
			for try := 0; try < 30; try++ {
				cn, err := g.js.CreateOrUpdateConsumer(g.ctx, c.stream, cc)
				if err != nil {
					g.t.Fatal(err)
				}
				if cn.CachedInfo().Cluster != nil && cn.CachedInfo().Cluster.Leader == "nats-2" {
					break
				}
				_ = g.js.DeleteConsumer(g.ctx, c.stream, c.name)
			}
			continue
		}
		if _, err := g.js.CreateOrUpdateConsumer(g.ctx, c.stream, cc); err != nil {
			g.t.Fatal(err)
		}
	}
	for _, s := range []string{"oa.x", "aa.x", "oj.x"} {
		for range 5 {
			if _, err := g.js.Publish(g.ctx, s, []byte("x")); err != nil {
				g.t.Fatal(err)
			}
		}
	}
}

// Durables lost while their server restarts: durables whose only replica is on
// nats-2 vanish while nats-2 restarts, with no delete event. While nats-2 is
// away they are info; once it is back and answers without them, critical,
// naming nats-2 as the last holder. After the review's TestFinal2_IncidentVanish.
func TestFinal2_IncidentVanish(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	g := newRig(t, final2Inv, false, config.Checks{RestartWindow: config.Duration(20 * time.Second)})
	g.incidentAssets(true)
	g.epoch("healthy 1")
	g.epoch("healthy 2")
	for _, d := range [][2]string{{"order-archive", "analytics-aggregator"}, {"order-archive", "active-jobs"},
		{"audit-archive", "usage-aggregator"}, {"order-jobs", "job-cache-projector"}, {"order-jobs", "legacy-worker"}} {
		if err := g.js.DeleteConsumer(g.ctx, d[0], d[1]); err != nil {
			t.Fatal(err)
		}
	}
	g.c.LameDuck(2)
	g.epoch("nats-2 down (lame duck), durables gone")
	for _, id := range []string{"consumer-010:APP/order-jobs/job-cache-projector", "consumer-013:APP/order-jobs/legacy-worker"} {
		if f := g.last[id]; !strings.HasPrefix(f, "info ") {
			t.Errorf("%s while its holder is away: %q, want info", id, f)
		}
	}
	g.c.RestartAsync(2)
	for range 8 {
		g.epoch("nats-2 restarting")
		time.Sleep(700 * time.Millisecond)
	}
	g.c.WaitMeta()
	g.epoch("meta back")
	time.Sleep(25 * time.Second)
	g.epoch("after restart window")
	for _, id := range []string{"consumer-010:APP/order-jobs/job-cache-projector",
		"consumer-010:APP/order-archive/analytics-aggregator", "consumer-013:APP/order-jobs/legacy-worker"} {
		f := g.last[id]
		if !strings.HasPrefix(f, "critical ") || !strings.Contains(f, "last reported by nats-2") {
			t.Errorf("%s once nats-2 is back: %q, want critical naming nats-2", id, f)
		}
	}
}
