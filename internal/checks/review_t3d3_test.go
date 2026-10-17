package checks

import (
	"fmt"
	"testing"
	"time"
)

// vanish builds 3 servers; a durable (or the whole stream) held by all three
// is last reported in epoch 5. nats-1 is away in 6-7, restarted and not
// ready in 8-10, fine from 11. nothing is reported from epoch 6.
func vanish(f *fx, wholeStream bool, holdersDownIdx ...int) {
	f.server("S0", "nats-0", 1, 12, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 12, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 5, t0.Add(-time.Hour), "2.15.0")
	f.server("N1", "nats-1", 8, 12, at(8).Add(-5*time.Second), "2.15.0")
	for e := int64(1); e <= 12; e++ {
		var miss []string
		if e == 6 || e == 7 {
			miss = []string{"nats-1"}
		}
		f.epoch(e, three, miss...)
		ids := []string{"S0", "S2"}
		if e <= 5 {
			ids = append(ids, "S1")
		}
		if e >= 8 {
			ids = append(ids, "N1")
		}
		for _, id := range ids {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			cols := m{}
			if id == "N1" {
				cols["start_time"] = at(8).Add(-5 * time.Second)
			}
			f.stats(e, id, cols)
			if id == "N1" && e <= 10 {
				f.row("health", m{"epoch": e, "server_id": id, "status": "unavailable", "status_code": 503, "error_count": 1,
					"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
				f.row("raft_stats", m{"epoch": e, "server_id": id, "group_id": "g", "catching_up": true})
			}
			if wholeStream && e > 5 {
				continue
			}
			f.streamStats(e, id, "jobs", m{"leader": "nats-0", "is_leader": id == "S0"})
		}
	}
	if !wholeStream {
		// the stream stays, reported by S0 and S2 (and N1 when not whole)
	}
	for _, id := range []string{"S0", "S1", "S2"} {
		f.streamConfig(id, "jobs", 1, 5, nil)
		if !wholeStream {
			f.consumerConfig(id, "jobs", "worker", 1, 5, nil)
			for e := int64(1); e <= 5; e++ {
				f.consumerStats(e, id, "jobs", "worker", m{"is_leader": id == "S0", "leader": "nats-0"})
			}
		}
	}
}

func TestT3D_DurableVanishesHolderRestarting(t *testing.T) {
	f := newFx(t)
	vanish(f, false)
	// Two of three holders are ready and lack it from the start: critical,
	// whatever nats-1 does.
	for _, e := range []int64{6, 8, 9, 10, 11, 12} {
		r := f.fires("consumer-013", e, "APP/jobs/worker")
		want(t, r["APP/jobs/worker"], "critical")
	}
}

func TestT3D_StreamVanishesHolderRestarting(t *testing.T) {
	f := newFx(t)
	vanish(f, true)
	for _, e := range []int64{6, 8, 10, 11, 12} {
		r := f.fires("stream-014", e, "APP/jobs")
		want(t, r["APP/jobs"], "critical")
	}
}

// durable deleted while the collector was down: epochs 6..15 absent, a
// collector.gap event, no consumer.deleted. All servers up at 16.
func TestT3D_DurableDeletedDuringCollectorGap(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 5, three...)
	for e := int64(16); e <= 17; e++ {
		f.epoch(e, three)
	}
	for i := range 3 {
		id := fmt.Sprintf("S%d", i)
		f.server(id, three[i], 1, 17, t0.Add(-time.Hour), "2.15.0")
		f.streamConfig(id, "jobs", 1, 17, nil)
		f.consumerConfig(id, "jobs", "worker", 1, 5, nil)
		for e := int64(1); e <= 17; e++ {
			if e > 5 && e < 16 {
				continue
			}
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			f.streamStats(e, id, "jobs", m{"leader": "nats-0", "is_leader": i == 0})
			if e <= 5 {
				f.consumerStats(e, id, "jobs", "worker", m{"leader": "nats-0", "is_leader": i == 0})
			}
		}
	}
	f.event(1, at(5).Add(30*time.Second), "collector.gap", m{"source": "collector"})
	for _, e := range []int64{16, 17} {
		r := f.fires("consumer-013", e, "APP/jobs/worker")
		want(t, r["APP/jobs/worker"], "critical", "nats-lens was not collecting from 2026-10-17 09:02:30")
	}
}
