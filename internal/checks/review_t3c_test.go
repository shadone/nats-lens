package checks

import (
	"fmt"
	"testing"
	"time"
)

func (f *fx) s3cfg(first, last int64) {
	for i := range 3 {
		f.streamConfig("S"+string(rune('0'+i)), "jobs", first, last, nil)
	}
}

// R1: node upgrade shape: nats-0 was upgraded and is back (restarted 4 min
// ago), then nats-1 and nats-2 go away at once. The survivor has a leaderless R3 stream.
func TestT3C_QuorumLossSurvivorRecentlyRestarted(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.epoch(4, three, "nats-1", "nats-2")
	f.epoch(5, three, "nats-1", "nats-2")
	f.server("OLD0", "nats-0", 1, 1, t0.Add(-time.Hour), "2.15.0")
	f.server("S0", "nats-0", 2, 5, at(2), "2.15.0")
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	for n := int64(2); n <= 5; n++ {
		f.stats(n, "S0", m{"start_time": at(2)})
	}
	f.s3cfg(1, 5)
	for e := int64(4); e <= 5; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "", "is_leader": false, "replicas_reported": 2, "replicas_offline": 2})
	}
	got := f.eval("stream-001", 5)
	t.Logf("stream-001: %s", rowsText(got))
	if len(got) == 0 {
		t.Errorf("quorum loss hidden: survivor restarted <10m ago")
	}
}

// R2: survivor of quorum loss with healthz JETSTREAM error (no meta leader).
func TestT3C_QuorumLossSurvivorJetStreamError(t *testing.T) {
	f := newFx(t)
	f.cluster(5)
	f.epoch(6, three, "nats-1", "nats-2")
	f.epoch(7, three, "nats-1", "nats-2")
	f.server("S0b", "nats-0", 6, 7, t0.Add(-time.Hour), "2.15.0")
	f.s3cfg(1, 7)
	for e := int64(6); e <= 7; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "", "is_leader": false, "replicas_reported": 2, "replicas_offline": 2})
		f.row("health", m{"epoch": e, "server_id": "S0", "status": "unavailable", "status_code": 503, "error_count": 1,
			"errors": `[{"type":"JETSTREAM","error":"JetStream has not established contact with a meta leader"}]`})
	}
	got := f.eval("stream-001", 7)
	t.Logf("stream-001: %s", rowsText(got))
	if len(got) == 0 {
		t.Errorf("quorum loss hidden: survivor has a JETSTREAM healthz error")
	}
}

// R3: lame duck on one + other crashed: survivor stable.
func TestT3C_QuorumLossDuringLameDuck(t *testing.T) {
	f := newFx(t)
	f.cluster(5)
	f.event(1, at(5).Add(2*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-1"})
	f.epoch(6, three, "nats-1", "nats-2")
	f.epoch(7, three, "nats-1", "nats-2")
	f.s3cfg(1, 7)
	for e := int64(6); e <= 7; e++ {
		f.streamStats(e, "S0", "jobs", m{"leader": "", "is_leader": false, "replicas_reported": 2, "replicas_offline": 2})
	}
	got := f.eval("stream-001", 7)
	if len(got) == 0 {
		t.Errorf("not fired")
	}
}

// R4: quorum lost, leader is stale view from a follower while leader is a restarted server.
func TestT3C_CatchingUpServerHidesLeaderView(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	f.s3cfg(1, 6)
	// nats-0 is the leader but catching up on another raft group (stuck); nats-1 down; nats-2 follows nats-0.
	f.epoch(5, three, "nats-1")
	f.epoch(6, three, "nats-1")
	for e := int64(5); e <= 6; e++ {
		f.row("raft_stats", m{"epoch": e, "server_id": "S0", "group_id": "g", "catching_up": true})
		f.streamStats(e, "S0", "jobs", m{"leader": "nats-0", "is_leader": true, "replicas_reported": 2, "replicas_offline": 2})
		f.streamStats(e, "S2", "jobs", m{"leader": "nats-0", "is_leader": false})
	}
	got := f.eval("stream-001", 6)
	t.Logf("stream-001: %s", rowsText(got))
	if len(got) == 0 {
		t.Errorf("leader sees 1 of 3 online, hidden because the leader is 'restarting'")
	}
}

// C1: durable deleted by client while host in lame duck.
func TestT3C_ConsumerDeletedWhileHolderLameDuck(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.streamStats(1, "S0", "jobs", m{"is_leader": true})
	for e := int64(1); e <= 4; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S1", "jobs", "worker", m{"is_leader": true})
	}
	f.event(1, at(2).Add(5*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-1"})
	f.event(2, at(2).Add(8*time.Second), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "worker", "client_name": "w", "client_host": "h"})
	f.epoch(3, three, "nats-1")
	f.epoch(4, three, "nats-1")
	got := f.eval("consumer-013", 4)
	t.Logf("consumer-013 with delete event, holder in lame duck: %s", rowsText(got))
	if len(got) == 0 {
		t.Errorf("delete event ignored while holder lame duck")
	}
	f.inventory("consumer", "worker", "jobs", "worker", nil)
	t.Logf("consumer-010: %s", rowsText(f.eval("consumer-010", 4)))
}

// C2: silent vanish, holder missing for a long time (pod Pending).
func TestT3C_ConsumerVanishedHolderMissingLong(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for e := int64(3); e <= 60; e++ {
		f.epoch(e, three, "nats-1")
	}
	for e := int64(1); e <= 60; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S1", "jobs", "worker", m{"is_leader": true})
	}
	for _, e := range []int64{4, 30, 59, 60} {
		t.Logf("epoch %d consumer-013: %s", e, rowsText(f.eval("consumer-013", e)))
	}
}

// C3: restarted holder; after restart window ends.
func TestT3C_ConsumerVanishedAfterRestart(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 2, three...)
	f.server("S0", "nats-0", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("S1n", "nats-1", 3, 60, at(3), "2.15.0")
	for e := int64(3); e <= 60; e++ {
		f.epoch(e, three)
		f.stats(e, "S1n", m{"start_time": at(3)})
	}
	for e := int64(1); e <= 60; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S1", "jobs", "worker", m{"is_leader": true})
	}
	for _, e := range []int64{4, 10, 30, 59} {
		t.Logf("epoch %d consumer-013: %s", e, rowsText(f.eval("consumer-013", e)))
	}
}

// S1: meta leader flapping after all three restarted within the window.
func TestT3C_MetaFlapAfterRollingRestart(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	// all three restarted at epochs 2,3,4 (new processes)
	for i, e := range []int64{2, 3, 4} {
		f.server(fmt.Sprintf("N%d", i), fmt.Sprintf("nats-%d", i), 1, 1, t0.Add(-time.Hour), "2.15.0")
		f.exec(`UPDATE servers SET start_time = ? WHERE server_id = ?`, at(e), fmt.Sprintf("S%d", i))
	}
	f.exec(`UPDATE servers SET first_epoch = 2 WHERE server_id = 'S0'`)
	f.exec(`UPDATE servers SET first_epoch = 3 WHERE server_id = 'S1'`)
	f.exec(`UPDATE servers SET first_epoch = 4 WHERE server_id = 'S2'`)
	for e := int64(1); e <= 40; e++ {
		l := three[(e/2)%3]
		if e < 12 {
			l = "nats-0"
		}
		for i := range 3 {
			f.stats(e, "S"+string(rune('0'+i)), m{"meta_cluster_size": 3, "meta_leader": l})
		}
	}
	got := f.eval("system-003", 30)
	t.Logf("system-003 ep30: %s", rowsText(got))
	if len(got) == 0 {
		t.Errorf("meta leader rotating every minute for 10 minutes hidden")
	}
}

// W1: server-003 never clears after the restart window.
func TestT3C_Server003StuckGoesCritical(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 60, three...)
	f.restarted(2, 60)
	f.server("S1", "nats-1", 1, 60, t0.Add(-time.Hour), "2.15.0")
	for e := int64(2); e <= 60; e++ {
		f.row("health", m{"epoch": e, "server_id": "S0", "status": "unavailable", "status_code": 503, "error_count": 1,
			"errors": `[{"type":"JETSTREAM","error":"JetStream is still recovering meta layer"}]`})
	}
	for _, e := range []int64{10, 25, 40, 60} {
		t.Logf("epoch %d server-003: %s", e, rowsText(f.eval("server-003", e)))
	}
}

// W2: crash loop: new process each 3 epochs with healthz JETSTREAM error each time.
func TestT3C_CrashLoop(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 40, three...)
	f.server("S1", "nats-1", 1, 40, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 40, t0.Add(-time.Hour), "2.15.0")
	f.server("L0", "nats-0", 1, 1, t0.Add(-time.Hour), "2.15.0")
	for e := int64(2); e <= 40; e++ {
		id := fmt.Sprintf("C%d", (e-2)/3)
		if (e-2)%3 == 0 {
			f.server(id, "nats-0", e, e+2, at(e), "2.15.0")
		}
		f.stats(e, id, m{"start_time": at(((e-2)/3)*3 + 2)})
		f.row("health", m{"epoch": e, "server_id": id, "status": "unavailable", "status_code": 503, "error_count": 1,
			"errors": `[{"type":"JETSTREAM","error":"JetStream is still recovering meta layer"}]`})
	}
	for _, c := range []string{"server-003", "server-004", "server-002"} {
		t.Logf("%s ep40: %s", c, rowsText(f.eval(c, 40)))
	}
}
