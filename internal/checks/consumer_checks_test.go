package checks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

const jobsKey = "APP/jobs/worker"

func TestConsumerQuorumLost(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for i := range 3 {
		id := "S" + string(rune('0'+i))
		f.streamConfig(id, "jobs", 1, 2, nil)
		// replicas 0 takes the stream's 3.
		f.consumerConfig(id, "jobs", "worker", 1, 2, nil)
		f.consumerConfig(id, "jobs", "r1", 1, 2, m{"replicas": 1})
	}
	for i := range 3 {
		id := "S" + string(rune('0'+i))
		f.consumerStats(1, id, "jobs", "worker", m{"leader": "nats-0", "is_leader": i == 0, "replicas_reported": 2, "replicas_offline": 0})
	}
	f.consumerStats(2, "S0", "jobs", "worker", m{"leader": "", "is_leader": false})
	f.consumerStats(2, "S0", "jobs", "r1", m{"leader": "", "is_leader": false})
	f.epoch(3, three)
	f.exec(`UPDATE stream_config SET last_epoch = 3`)
	f.exec(`UPDATE consumer_config SET last_epoch = 3`)
	f.consumerStats(3, "S0", "jobs", "worker", m{"leader": "", "is_leader": false})
	f.consumerStats(3, "S0", "jobs", "r1", m{"leader": "", "is_leader": false})
	f.fires("consumer-001", 1)
	f.fires("consumer-001", 2)
	r := f.fires("consumer-001", 3, jobsKey)
	want(t, r[jobsKey], "critical", "No leader that the reporting servers agree on (nats-0: none), for 2 epochs")
}

func TestConsumerBacklogGrowing(t *testing.T) {
	f := newFx(t)
	f.cluster(80)
	for e := int64(1); e <= 80; e++ {
		// worker: 10 msg/s arrive, 5 msg/s delivered over 30s epochs.
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true, "num_pending": 150 * e, "delivered_consumer_seq": 150 * e})
		// spike: one burst, then flat.
		p := int64(0)
		if e == 79 {
			p = 5000
		}
		f.consumerStats(e, "S0", "jobs", "spike", m{"is_leader": true, "num_pending": p, "delivered_consumer_seq": 10 * e})
		// draining: backlog falls.
		f.consumerStats(e, "S0", "jobs", "draining", m{"is_leader": true, "num_pending": 10000 - 100*e, "delivered_consumer_seq": 100 * e})
		// A follower's row carries no backlog and is not read.
		f.consumerStats(e, "S1", "jobs", "follower-only", m{"is_leader": false, "delivered_consumer_seq": e})
	}
	r := f.fires("consumer-002", 80, jobsKey)
	want(t, r[jobsKey], "warning", "Fill 10.0 msg/s, drain 5.0 msg/s over 00:30:00", "12000 pending, up 9000")
	if v := r[jobsKey].Value; v == nil || *v != 5 {
		t.Errorf("value = %v, want 5 msg/s", v)
	}
	// A consumer recreated within the window is measured from its creation.
	g := newFx(t)
	g.cluster(80)
	for e := int64(1); e <= 80; e++ {
		created := t0.Add(-time.Hour)
		if e >= 70 {
			created = at(70)
		}
		g.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true, "num_pending": 150 * e, "created": created})
	}
	g.fires("consumer-002", 80)
}

func TestAckPendingAndRedeliveries(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	for _, c := range []struct {
		name               string
		ack, pending, prev int64
	}{{"near", 850, 7, 7}, {"full", 1000, 7, 7}, {"busy", 1000, 7, 7}, {"just-full", 1000, 7, 0}, {"saturated", 1000, 0, 0}, {"fine", 10, 7, 7}} {
		f.consumerConfig("S0", "jobs", c.name, 1, 2, nil)
		for e, p := range map[int64]int64{1: c.prev, 2: c.pending} {
			// busy acknowledges and is delivered more between the epochs;
			// the others make no progress.
			seq := int64(100)
			if c.name == "busy" {
				seq += 10 * e
			}
			f.consumerStats(e, "S0", "jobs", c.name, m{"is_leader": true, "num_ack_pending": c.ack, "num_pending": p,
				"num_redelivered": c.ack / 2, "delivered_stream_seq": seq, "ack_floor_stream_seq": seq - 50})
		}
	}
	// saturated: every slot in use and nothing waiting is a busy pool, not
	// a finding; busy: at the limit with messages waiting but acknowledging
	// is a saturated pool, a warning; just-full: at the limit with
	// messages waiting for one epoch only is a warning.
	r := f.fires("consumer-003", 2, "APP/jobs/near", "APP/jobs/full", "APP/jobs/busy", "APP/jobs/just-full")
	want(t, r["APP/jobs/near"], "warning", "850 of max_ack_pending 1000 unacknowledged; 7 waiting")
	want(t, r["APP/jobs/full"], "critical", "no acknowledgement and no delivery for 00:00:30: delivery has stopped")
	want(t, r["APP/jobs/busy"], "warning", "pool saturated")
	if strings.Contains(r["APP/jobs/busy"].Detail, "stopped") {
		t.Errorf("busy: %s", r["APP/jobs/busy"].Detail)
	}
	want(t, r["APP/jobs/just-full"], "warning")
	if r["APP/jobs/full"].RelatedConfig["ack_wait"] != "30s" {
		t.Errorf("related = %v", r["APP/jobs/full"].RelatedConfig)
	}
	r = f.eval("consumer-004", 2)
	want(t, r["APP/jobs/full"], "warning", "500 of 1000 unacknowledged messages were delivered more than once", "ack_wait 30s, max_deliver 3, max_ack_pending 1000")
}

func TestMaxDeliveries(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	f.row("accounts", m{"account": "AXYZ", "account_name": "APP", "is_system": false, "first_epoch": 1, "last_epoch": 200})
	f.event(1, at(190), "consumer.max_deliveries", m{"account": "APP", "stream": "jobs", "consumer": "worker"})
	f.event(2, at(195), "consumer.max_deliveries", m{"account": "APP", "stream": "jobs", "consumer": "worker"})
	f.event(3, at(1), "consumer.max_deliveries", m{"account": "APP", "stream": "jobs", "consumer": "old"})
	r := f.fires("consumer-005", 200, "AXYZ/jobs/worker")
	want(t, r["AXYZ/jobs/worker"], "warning", "2 messages reached max_deliver in the last 1h, the last at 10:37:00 UTC")
	if r["AXYZ/jobs/worker"].CauseEventID != 2 {
		t.Errorf("cause = %d", r["AXYZ/jobs/worker"].CauseEventID)
	}
}

func TestNoDeliveryWhilePending(t *testing.T) {
	f := newFx(t)
	f.cluster(30)
	for e := int64(1); e <= 30; e++ {
		// stuck: 40 waiting since epoch 1, last delivery 20 minutes ago.
		f.consumerStats(e, "S0", "jobs", "stuck", m{"is_leader": true, "num_pending": 40, "num_waiting": 0,
			"delivered_last_active": at(30).Add(-20 * time.Minute)})
		// fresh: the first message after an idle hour arrived in epoch 30.
		p := int64(0)
		if e == 30 {
			p = 1
		}
		f.consumerStats(e, "S0", "jobs", "fresh", m{"is_leader": true, "num_pending": p, "num_waiting": 1,
			"delivered_last_active": at(30).Add(-time.Hour)})
		// busy: waiting for long, but delivering.
		f.consumerStats(e, "S0", "jobs", "busy", m{"is_leader": true, "num_pending": 40, "delivered_last_active": at(e).Add(-time.Minute)})
		f.consumerStats(e, "S0", "jobs", "paused", m{"is_leader": true, "num_pending": 40, "paused": true,
			"delivered_last_active": at(30).Add(-time.Hour)})
	}
	r := f.fires("consumer-006", 30, "APP/jobs/stuck")
	want(t, r["APP/jobs/stuck"], "warning", "40 messages waiting since 2026-10-17 09:00:00 UTC", "0 pull requests waiting")
}

func TestPushConsumerUnbound(t *testing.T) {
	f := newFx(t)
	f.cluster(10)
	f.consumerConfig("S0", "jobs", "push", 1, 10, m{"deliver_subject": "deliver.push"})
	f.consumerConfig("S0", "jobs", "pull", 1, 10, nil)
	f.consumerConfig("S0", "jobs", "watcher", 1, 10, m{"deliver_subject": "_INBOX.x", "durable_name": ""})
	for e := int64(1); e <= 10; e++ {
		f.consumerStats(e, "S0", "jobs", "push", m{"is_leader": true, "push_bound": e < 4})
		f.consumerStats(e, "S0", "jobs", "pull", m{"is_leader": true, "push_bound": false})
		f.consumerStats(e, "S0", "jobs", "watcher", m{"is_leader": true, "push_bound": false})
	}
	f.fires("consumer-007", 6)
	r := f.fires("consumer-007", 10, "APP/jobs/push")
	want(t, r["APP/jobs/push"], "warning", "No subscriber on deliver subject deliver.push since 2026-10-17 09:01:30 UTC")
}

func TestConsumerAgreement(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	old := t0.Add(-48 * time.Hour)
	f.consumerConfig("S0", "jobs", "worker", 1, 1, m{"created": old})
	f.consumerConfig("S1", "jobs", "worker", 1, 1, m{"created": old})
	f.consumerConfig("S2", "jobs", "worker", 1, 1, m{"created": t0.Add(-time.Hour), "max_ack_pending": 500})
	r := f.fires("consumer-008", 1, jobsKey)
	want(t, r[jobsKey], "warning", "2 configurations (nats-0, nats-1 | nats-2)", "differing: max_ack_pending")
	r = f.fires("consumer-009", 1, jobsKey)
	want(t, r[jobsKey], "info", "nats-0, nats-1 2026-10-15T09:00:00Z; nats-2 2026-10-17T08:00:00Z")
}

func TestExpectedConsumerMissing(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.inventory("consumer", "worker", "jobs", "worker", m{"max_ack_pending": 1000, "ack_wait_ns": 30e9})
	f.inventory("consumer", "chunk-pool", "windows", "chunk-pool", nil)
	f.row("consumers", m{"account": "APP", "stream": "jobs", "consumer": "worker", "first_epoch": 1, "last_epoch": 2})
	f.row("consumers", m{"account": "APP", "stream": "windows", "consumer": "chunk-pool", "first_epoch": 1, "last_epoch": 2})
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true})
		f.consumerStats(e, "S0", "windows", "chunk-pool", m{"is_leader": true})
	}
	f.event(5, at(2).Add(5*time.Second), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "worker",
		"client_name": "analytics-worker", "client_host": "10.20.0.12"})
	f.event(6, at(2).Add(6*time.Second), "stream.deleted", m{"account": "APP", "stream": "windows", "client_name": "nack"})
	f.fires("consumer-010", 2)
	r := f.fires("consumer-010", 3, jobsKey, "APP/windows/chunk-pool")
	want(t, r[jobsKey], "critical", "Consumer worker on jobs is in the inventory and no server reports it",
		"Deleted 2026-10-17 09:00:35 UTC by analytics-worker (10.20.0.12)")
	if r[jobsKey].CauseEventID != 5 || r[jobsKey].RelatedConfig["max_ack_pending"] != "1000" || r[jobsKey].RelatedConfig["ack_wait"] != "30s" {
		t.Errorf("row = %+v", r[jobsKey])
	}
	want(t, r["APP/windows/chunk-pool"], "critical", "Its stream was deleted 2026-10-17 09:00:36 UTC by nack")
}

func TestConsumerDiffersFromInventory(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.inventory("consumer", "worker", "jobs", "worker", m{"max_ack_pending": 1000, "ack_wait_ns": 60e9, "replicas": 3})
	f.exec(`UPDATE inventory SET filter_subjects = ['orders.*.*.resize.created']`)
	f.streamConfig("S0", "jobs", 1, 1, nil)
	f.consumerConfig("S0", "jobs", "worker", 1, 1, m{"max_ack_pending": 1000})
	f.exec(`UPDATE consumer_config SET filter_subjects = ['orders.>']`)
	f.consumerStats(1, "S0", "jobs", "worker", m{"is_leader": true})
	r := f.fires("consumer-011", 1, jobsKey)
	want(t, r[jobsKey], "warning", "ack_wait 30s (inventory 1m)", "filter [orders.>] (inventory [orders.*.*.resize.created])")
	if v := r[jobsKey].Value; v == nil || *v != 2 {
		t.Errorf("value = %v, want 2 differences (replicas 0 is the stream's 3)", v)
	}
}

func TestConsumerMissingOnAssignedServer(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.consumerStats(1, "S0", "jobs", "worker", m{"is_leader": true, "leader": "nats-0"})
	f.consumerReplica(1, "S0", "jobs", "worker", "nats-1", nil)
	f.consumerReplica(1, "S0", "jobs", "worker", "nats-2", nil)
	f.consumerStats(1, "S1", "jobs", "worker", m{"leader": "nats-0"})
	r := f.fires("consumer-012", 1, jobsKey)
	want(t, r[jobsKey], "warning", "assigns the consumer to nats-2")
}

func TestClientChecks(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	conn := func(id string, cid int64, name string, rtt, pending int64) {
		for _, e := range []int64{99, 100} {
			f.row("connections", m{"epoch": e, "server_id": id, "cid": cid, "name": name, "rtt_ns": rtt, "pending_bytes": pending})
		}
	}
	conn("S0", 1, "media-worker", 250e6, 0)
	conn("S1", 2, "media-worker", 1e6, 8<<20)
	conn("S1", 3, "", 150e6, 0)
	conn("S2", 4, "ledger-service", 2e6, 100)
	r := f.fires("client-001", 100, "media-worker", "(unnamed)")
	want(t, r["media-worker"], "warning", "1 of 2 connections above 100ms; worst 250.0ms on nats-0 cid 1")
	r = f.fires("client-002", 100, "media-worker")
	want(t, r["media-worker"], "warning", "1 connection has more than 4.0 MiB queued", "nats-1 cid 2")
	f.event(1, at(95), "client.disconnect", m{"source": "sys", "client_name": "media-worker", "server_name": "nats-1",
		"reason": "Slow Consumer (Write Deadline)"})
	f.event(2, at(96), "client.disconnect", m{"source": "sys", "client_name": "media-worker", "server_name": "nats-0",
		"reason": "Slow Consumer (Pending Bytes)"})
	f.event(3, at(97), "client.disconnect", m{"source": "sys", "client_name": "other", "reason": "Client Closed"})
	f.event(4, at(1), "client.disconnect", m{"source": "sys", "client_name": "old", "reason": "Slow Consumer (Write Deadline)"})
	r = f.fires("client-003", 100, "media-worker")
	want(t, r["media-worker"], "warning", "2 slow consumer disconnects in the last 30m (nats-0, nats-1)", "Slow Consumer (Pending Bytes)")
	if r["media-worker"].CauseEventID != 2 {
		t.Errorf("cause = %d", r["media-worker"].CauseEventID)
	}
}

// As for streams, an older epoch uses the consumer's last report before it.
func TestExpectedConsumerMissingAtAnOlderEpoch(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.inventory("consumer", "worker", "jobs", "worker", nil)
	f.row("consumers", m{"account": "APP", "stream": "jobs", "consumer": "worker", "first_epoch": 1, "last_epoch": 3})
	f.consumerStats(1, "S0", "jobs", "worker", m{"is_leader": true})
	f.consumerStats(3, "S0", "jobs", "worker", m{"is_leader": true})
	f.event(4, at(1).Add(10*time.Second), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "worker",
		"client_name": "cleanup"})
	r := f.fires("consumer-010", 2, jobsKey)
	want(t, r[jobsKey], "critical", "Deleted 2026-10-17 09:00:10 UTC by cleanup")
}

func TestConsumerQuorumIgnoresRestartedServer(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	for _, id := range []string{"S0", "S1", "S2"} {
		f.streamConfig(id, "jobs", 1, 3, nil)
		f.consumerConfig(id, "jobs", "worker", 1, 3, nil)
	}
	for e := int64(2); e <= 3; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"leader": "nats-0", "is_leader": true, "replicas_reported": 2, "replicas_offline": 2})
		f.consumerStats(e, "S1", "jobs", "worker", m{"leader": "nats-2"})
		f.consumerStats(e, "S2", "jobs", "worker", m{"leader": "nats-2", "is_leader": true, "replicas_reported": 2, "replicas_offline": 0})
	}
	f.fires("consumer-001", 3)
}

// The gap rule of stream-011 holds for consumers: a holder that did not
// answer makes the finding a warning, a restarting one info.
func TestExpectedConsumerOnServerNotReady(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 2, three...)
	f.epoch(3, three, "nats-2")
	f.restarted(3, 3)
	f.server("S2", "nats-2", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.inventory("consumer", "a", "jobs", "a", nil)
	f.inventory("consumer", "b", "jobs", "b", nil)
	f.inventory("consumer", "c", "jobs", "c", nil)
	f.consumerStats(2, "OLD0", "jobs", "a", m{"is_leader": true})
	f.consumerStats(2, "S2", "jobs", "b", m{"is_leader": true})
	r := f.fires("consumer-010", 3, "APP/jobs/a", "APP/jobs/b", "APP/jobs/c")
	want(t, r["APP/jobs/a"], "info", "may be on nats-0, which is restarting")
	want(t, r["APP/jobs/b"], "warning", "may be on nats-2, which did not answer")
	want(t, r["APP/jobs/c"], "warning", "may be on nats-0, nats-2")
}

// Redeliveries count against max_ack_pending, with an absolute floor: a
// fixed count suits no consumer size.
func TestRedeliveriesRatio(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	for _, c := range []struct {
		name          string
		max, redel, n int64
	}{{"big-ok", 8000, 1500, 8000}, {"big-bad", 8000, 2500, 8000}, {"two", 2, 2, 2}, {"two-ok", 2, 1, 2},
		{"tiny", 8, 5, 8}, {"few", 1000, 9, 20}, {"unlimited", -1, 30, 100}} {
		f.consumerConfig("S0", "jobs", c.name, 1, 1, m{"max_ack_pending": c.max})
		f.consumerStats(1, "S0", "jobs", c.name, m{"is_leader": true, "num_ack_pending": c.n, "num_redelivered": c.redel})
	}
	// The floor is the smaller of min_redelivered (10) and max_ack_pending,
	// so a consumer with two slots can fire; the share is a quarter of
	// max_ack_pending.
	r := f.fires("consumer-004", 1, "APP/jobs/big-bad", "APP/jobs/two", "APP/jobs/unlimited")
	want(t, r["APP/jobs/two"], "warning", "2 of 2 unacknowledged messages")
}

// A durable seen within the lookback that no server reports any more,
// while its stream is there, has disappeared; the delete event names who
// removed it. Internal, ephemeral, inventory and expiring durables are left
// out; one on a server that is not ready is a warning.
func TestDurableDisappeared(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	names := []string{"gone", "JS_MIRROR_x", "ephemeral", "listed", "expires", "on-restarting"}
	for _, n := range names {
		durable := n
		if n == "ephemeral" {
			durable = ""
		}
		threshold := int64(0)
		if n == "expires" {
			threshold = 300e9
		}
		f.consumerConfig("S0", "jobs", n, 1, 2, m{"durable_name": durable, "inactive_threshold_ns": threshold})
		for e := int64(1); e <= 2; e++ {
			f.consumerStats(e, "S0", "jobs", n, m{"is_leader": true})
		}
	}
	f.consumerConfig("S0", "jobs", "still", 1, 4, nil)
	for e := int64(1); e <= 4; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
		f.consumerStats(e, "S0", "jobs", "still", m{"is_leader": true})
	}
	f.inventory("consumer", "listed", "jobs", "listed", nil)
	f.event(5, at(2).Add(5*time.Second), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "gone",
		"client_name": "analytics-worker", "client_host": "10.20.0.12"})
	// on-restarting lives on nats-1, which restarts in epoch 3.
	f.exec(`UPDATE consumer_stats SET server_id = 'S1' WHERE consumer = 'on-restarting'`)
	f.exec(`UPDATE consumer_config SET server_id = 'S1' WHERE consumer = 'on-restarting'`)
	f.row("health", m{"epoch": 3, "server_id": "S1", "status": "unavailable", "error_count": 1,
		"errors": `[{"type":"JETSTREAM","error":"JetStream is still recovering meta layer"}]`})
	// on-restarting's server reports JetStream not ready: the durable may
	// still be there, so it is a warning, not left out.
	r := f.fires("consumer-013", 3, "APP/jobs/gone", "APP/jobs/on-restarting")
	want(t, r["APP/jobs/on-restarting"], "warning", "may be on nats-1, which reports JetStream not ready")
	want(t, r["APP/jobs/gone"], "critical", "Durable gone on jobs was last reported by nats-0 at 2026-10-17 09:00:30 UTC",
		"Deleted 2026-10-17 09:00:35 UTC by analytics-worker (10.20.0.12)")
	if r["APP/jobs/gone"].CauseEventID != 5 {
		t.Errorf("cause = %d", r["APP/jobs/gone"].CauseEventID)
	}
	// Without its stream, the stream is what disappeared.
	f.exec(`DELETE FROM stream_stats WHERE epoch = 4`)
	f.fires("consumer-013", 4)
}

// A burst in the last minutes is not a growing backlog: the backlog must
// grow in both halves of the window. After a leader change the delivered
// counter may restart lower; the drain rate counts only its increases.
func TestBacklogBurstAndLeaderChange(t *testing.T) {
	f := newFx(t)
	f.cluster(80)
	for e := int64(1); e <= 80; e++ {
		p := int64(0)
		if e >= 75 {
			p = 5000
		}
		f.consumerStats(e, "S0", "jobs", "burst", m{"is_leader": true, "num_pending": p, "delivered_consumer_seq": 10 * e})
		d := 10 * e
		if e >= 40 {
			d = 10 * (e - 40)
		}
		f.consumerStats(e, "S0", "jobs", "grow", m{"is_leader": true, "num_pending": 200 * e, "delivered_consumer_seq": d})
	}
	r := f.fires("consumer-002", 80, "APP/jobs/grow")
	want(t, r["APP/jobs/grow"], "warning", "drain 0.33 msg/s", "Fill 6.99 msg/s")
}

// The only server left restarted recently and reports the consumer without
// a leader: that report is evidence of the loss and is not left out.
func TestConsumerQuorumSurvivorRestarted(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.epoch(4, three, "nats-1", "nats-2")
	f.epoch(5, three, "nats-1", "nats-2")
	f.restarted(2, 5)
	for _, id := range []string{"S0", "S1", "S2"} {
		f.streamConfig(id, "jobs", 1, 5, nil)
		f.consumerConfig(id, "jobs", "worker", 1, 5, nil)
	}
	for e := int64(4); e <= 5; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"leader": "", "replicas_reported": 2, "replicas_offline": 2})
	}
	r := f.fires("consumer-001", 5, jobsKey)
	want(t, r[jobsKey], "critical", "No leader")
}

// A durable whose server has been missing since its last report is
// reported at warning however long that lasts; it is not aged out of the
// lookback while the server is gone.
func TestDurableVanishedHolderMissingLong(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 2, three...)
	f.server("S0", "nats-0", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 2, t0.Add(-time.Hour), "2.15.0")
	for e := int64(1); e <= 2; e++ {
		f.row("epoch_endpoints", m{"epoch": e, "server_id": "S1", "endpoint": "JSZ", "state": "ok"})
	}
	for e := int64(3); e <= 6; e++ {
		f.epochAt(e, at(2).Add(time.Duration(e-2)*40*time.Minute), three, "nats-1")
	}
	for e := int64(1); e <= 6; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S1", "jobs", "worker", m{"is_leader": true})
	}
	for _, e := range []int64{3, 6} {
		r := f.fires("consumer-013", e, jobsKey)
		want(t, r[jobsKey], "warning", "may be on nats-1, which did not answer",
			"No delete event recorded; the durable may have been renamed or removed by the server")
	}
}

// Once every server that held it answers and it is still absent, it is
// critical; while its restarted server is within restart_window, info.
func TestDurableVanishedAfterRestart(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 30, three...)
	f.server("S0", "nats-0", 1, 30, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("S1n", "nats-1", 3, 30, at(3), "2.15.0")
	for e := int64(3); e <= 30; e++ {
		f.stats(e, "S1n", m{"start_time": at(3)})
	}
	for e := int64(1); e <= 30; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S1", "jobs", "worker", m{"is_leader": true})
	}
	r := f.fires("consumer-013", 4, jobsKey)
	want(t, r[jobsKey], "info", "may be on nats-1, which is restarting")
	r = f.fires("consumer-013", 30, jobsKey)
	want(t, r[jobsKey], "critical")
}

// A deleted stream takes its consumers with it: when the stream was
// recreated after the delete, the durable's finding names that delete at
// warning, as the stream is back and only the durable is missing.
func TestDurableGoneWithItsStream(t *testing.T) {
	f := newFx(t)
	f.cluster(4)
	f.consumerConfig("S0", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true})
	}
	for _, e := range []int64{1, 2, 4} {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.event(7, at(2).Add(5*time.Second), "stream.deleted", m{"account": "APP", "stream": "jobs", "client_name": "cleanup"})
	r := f.fires("consumer-013", 4, jobsKey)
	want(t, r[jobsKey], "warning", "Its stream was deleted 2026-10-17 09:00:35 UTC by cleanup", "the stream was created again")
	if r[jobsKey].CauseEventID != 7 {
		t.Errorf("cause = %d", r[jobsKey].CauseEventID)
	}
}

// As for streams: a recovering reporter is not part of the healthy
// majority that lets a restarted server's report be left out.
func TestConsumerQuorumRecoveringIsNotHealthy(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 3, three...)
	f.restarted(2, 3)
	f.server("S1", "nats-1", 1, 3, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 3, t0.Add(-time.Hour), "2.15.0")
	for _, id := range []string{"S0", "S1", "S2"} {
		f.streamConfig(id, "jobs", 1, 3, nil)
		f.consumerConfig(id, "jobs", "worker", 1, 3, nil)
	}
	for e := int64(2); e <= 3; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"leader": "nats-2"})
		f.consumerStats(e, "S1", "jobs", "worker", m{"leader": ""})
		f.row("raft_stats", m{"epoch": e, "server_id": "S1", "group_id": "g", "catching_up": true})
		f.consumerStats(e, "S2", "jobs", "worker", m{"leader": "nats-2", "is_leader": true, "replicas_reported": 2, "replicas_offline": 0})
	}
	f.fires("consumer-001", 3)
}

// A holder that is back from a restart, has nothing else wrong and answers
// JSZ reports what it holds: its lacking the durable counts as ready.
func TestDurableVanishedHolderBackAndAnswering(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 4, three...)
	f.server("S0", "nats-0", 1, 4, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("S1n", "nats-1", 3, 4, at(3), "2.15.0")
	for e := int64(3); e <= 4; e++ {
		f.stats(e, "S1n", m{"start_time": at(3)})
		f.row("epoch_endpoints", m{"epoch": e, "server_id": "S1n", "endpoint": "JSZ", "state": "ok"})
	}
	for e := int64(1); e <= 4; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S1", "jobs", "worker", m{"is_leader": true})
	}
	r := f.fires("consumer-013", 4, jobsKey)
	want(t, r[jobsKey], "critical")
}

// When the collector was not running between the last report and now, the
// finding says so: a delete in that time left no event to find.
func TestDurableVanishedDuringCollectorGap(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.epoch(3, three)
	for _, id := range []string{"S0", "S1", "S2"} {
		f.row("epoch_endpoints", m{"epoch": 3, "server_id": id, "endpoint": "JSZ", "state": "ok"})
	}
	for e := int64(1); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.consumerConfig("S0", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true})
	}
	f.event(1, at(2).Add(10*time.Second), "collector.gap", m{"source": "collector", "received_at": at(3).Add(-time.Second)})
	f.event(2, t0.Add(-2*time.Hour), "collector.gap", m{"source": "collector", "received_at": t0.Add(-time.Hour)})
	r := f.fires("consumer-013", 3, jobsKey)
	want(t, r[jobsKey], "critical", "nats-lens was not collecting from 2026-10-17 09:00:40 to 09:00:59 UTC")
	if strings.Contains(r[jobsKey].Detail, "07:00") {
		t.Errorf("a gap before the last report is mentioned: %s", r[jobsKey].Detail)
	}
}

// incidentShape builds a durable lost while its server restarts: the durable
// worker on jobs runs only on nats-2. nats-2 announces lame duck after
// epoch 2 and is missing in 3, is back in 4 with JetStream not ready, and
// answers ready from 5, without the durable.
func (f *fx) incidentShape() {
	f.epochs(1, 2, three...)
	f.epoch(3, three, "nats-2")
	f.epochs(4, 6, three...)
	f.server("S0", "nats-0", 1, 6, t0.Add(-time.Hour), "2.15.0")
	f.server("S1", "nats-1", 1, 6, t0.Add(-time.Hour), "2.15.0")
	f.server("S2", "nats-2", 1, 2, t0.Add(-time.Hour), "2.15.0")
	f.server("N2", "nats-2", 4, 6, at(4).Add(-5*time.Second), "2.15.0")
	f.event(1, at(2).Add(10*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-2"})
	for e := int64(1); e <= 6; e++ {
		ids := []string{"S0", "S1", "S2"}
		if e == 3 {
			ids = ids[:2]
		}
		if e >= 4 {
			ids = []string{"S0", "S1", "N2"}
		}
		for _, id := range ids {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": "JSZ", "state": "ok"})
			if id == "N2" {
				f.stats(e, id, m{"start_time": at(4).Add(-5 * time.Second)})
			}
		}
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	f.row("health", m{"epoch": 4, "server_id": "N2", "status": "unavailable", "error_count": 1,
		"errors": `[{"type":"JETSTREAM","error":"JetStream is not current with the meta leader"}]`})
	f.consumerConfig("S2", "jobs", "worker", 1, 2, m{"replicas": 1})
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S2", "jobs", "worker", m{"is_leader": true})
	}
}

// The durable's only holder restarts: info while it is away or not ready,
// critical once it is back, ready and answers without the durable. The
// detail names the holder and when it last reported the durable.
func TestDurableOnlyHolderRestarts(t *testing.T) {
	for _, code := range []string{"consumer-010", "consumer-013"} {
		t.Run(code, func(t *testing.T) {
			f := newFx(t)
			f.incidentShape()
			if code == "consumer-010" {
				f.inventory("consumer", "worker", "jobs", "worker", nil)
			}
			for e, sev := range map[int64]string{3: "info", 4: "info", 5: "critical", 6: "critical"} {
				r := f.fires(code, e, jobsKey)
				want(t, r[jobsKey], sev, "last reported by nats-2 at 2026-10-17 09:00:30 UTC")
			}
		})
	}
}

// reconnect stores a disconnect of name from host with reason, and its
// connect five seconds later.
func (f *fx) reconnect(id int64, when time.Time, name, host, srv, reason string) {
	f.event(id, when, "client.disconnect", m{"source": "sys", "client_name": name, "client_host": host,
		"server_name": srv, "reason": reason})
	f.event(id+1, when.Add(5*time.Second), "client.connect", m{"source": "sys", "client_name": name,
		"client_host": host, "server_name": srv})
}

// Three disconnects that the client did not ask for, each followed by a
// connect of the same name and host, within ten minutes: the client keeps
// losing its connection. The detail lists the servers and the reasons.
func TestClientReconnecting(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i, srv := range []string{"nats-0", "nats-1", "nats-0"} {
		f.reconnect(int64(10*i+1), at(30).Add(time.Duration(i)*time.Minute), "api-gateway", "10.0.0.5", srv, "Stale Connection")
	}
	// Disconnects the client asked for, or that a server stopping caused,
	// are not reconnects.
	for i := range 3 {
		f.reconnect(int64(100+10*i), at(30).Add(time.Duration(i)*time.Minute), "deployer", "10.0.0.6", "nats-0", "Client Closed")
		f.reconnect(int64(200+10*i), at(30).Add(time.Duration(i)*time.Minute), "worker", "10.0.0.7", "nats-1", "Server Shutdown")
	}
	r := f.fires("client-004", 40, "api-gateway")
	want(t, r["api-gateway"], "warning", "3 reconnects in the last 10m (nats-0, nats-1): Stale Connection")
}

// Pairing is by name only and in either order: a client that comes back
// from another IP (a new pod) reconnects, and a drop the server logs
// after the client already connected again still counts.
func TestClientReconnectingAnyHostAnyOrder(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i := range 3 {
		when := at(30).Add(time.Duration(i) * time.Minute)
		f.event(int64(10*i+1), when, "client.disconnect", m{"source": "sys", "client_name": "media-worker",
			"client_host": fmt.Sprintf("10.2.0.%d", i), "server_name": "nats-0", "reason": "Slow Consumer (Write Deadline)"})
		// The new pod connects from another address; the third time the
		// connect is logged before the drop.
		cw := when.Add(5 * time.Second)
		if i == 2 {
			cw = when.Add(-5 * time.Second)
		}
		f.event(int64(10*i+2), cw, "client.connect", m{"source": "sys", "client_name": "media-worker",
			"client_host": fmt.Sprintf("10.2.1.%d", i), "server_name": "nats-1"})
	}
	r := f.fires("client-004", 40, "media-worker")
	want(t, r["media-worker"], "warning", "3 reconnects in the last 10m")
}

// A service scaling out opens a connection per instance under one name,
// from different hosts and without disconnects: not a reconnect.
func TestClientScaleOutIsNotReconnecting(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i := range 6 {
		f.event(int64(i+1), at(30).Add(time.Duration(i)*20*time.Second), "client.connect", m{"source": "sys",
			"client_name": "api-gateway", "client_host": fmt.Sprintf("10.1.0.%d", i), "server_name": "nats-0"})
	}
	f.fires("client-004", 40)
}

// "after" the epoch that saw them.
func TestEventWindowsUseReceivedAt(t *testing.T) {
	f := newFx(t)
	f.cluster(200)
	ahead := 2 * time.Minute
	f.event(1, at(199).Add(ahead), "consumer.max_deliveries", m{"account": "APP", "stream": "jobs", "consumer": "worker",
		"received_at": at(199).Add(time.Second)})
	f.fires("consumer-005", 200, jobsKey)
	// A delete stamped before the last report by a clock behind, received
	// after it.
	g := newFx(t)
	g.cluster(3)
	g.inventory("consumer", "worker", "jobs", "worker", nil)
	for e := int64(1); e <= 3; e++ {
		g.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	for e := int64(1); e <= 2; e++ {
		g.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true})
	}
	g.event(5, at(2).Add(-ahead), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "worker",
		"client_name": "cleanup", "received_at": at(2).Add(5 * time.Second)})
	r := g.fires("consumer-010", 3, jobsKey)
	if r[jobsKey].CauseEventID != 5 {
		t.Errorf("consumer-010 cause = %d, want the delete received after the last report", r[jobsKey].CauseEventID)
	}
}

// A stuck pool stays critical: no acknowledgement and no delivery for a
// long time does not age the finding out.
func TestStuckPoolStaysCritical(t *testing.T) {
	f := newFx(t)
	f.cluster(60)
	f.consumerConfig("S0", "jobs", "worker", 1, 60, m{"max_ack_pending": 4})
	for e := int64(1); e <= 60; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true, "num_ack_pending": 4, "num_pending": 50,
			"delivered_stream_seq": 104, "ack_floor_stream_seq": 100})
	}
	for _, e := range []int64{3, 10, 30, 60} {
		r := f.fires("consumer-003", e, jobsKey)
		want(t, r[jobsKey], "critical", "delivery has stopped")
	}
}

// Two delete events after the last report: the one nats-lens received last
// is the cause, whatever the senders' clocks say.
func TestCauseRankedByReceivedAt(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.inventory("consumer", "worker", "jobs", "worker", nil)
	f.consumerConfig("S0", "jobs", "worker", 1, 2, nil)
	for e := int64(1); e <= 3; e++ {
		f.streamStats(e, "S0", "jobs", m{"is_leader": true})
	}
	for e := int64(1); e <= 2; e++ {
		f.consumerStats(e, "S0", "jobs", "worker", m{"is_leader": true})
	}
	f.event(7, at(2).Add(20*time.Second), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "worker",
		"client_name": "skewed", "received_at": at(2).Add(5 * time.Second)})
	f.event(8, at(2).Add(10*time.Second), "consumer.deleted", m{"account": "APP", "stream": "jobs", "consumer": "worker",
		"client_name": "latest", "received_at": at(2).Add(15 * time.Second)})
	r := f.fires("consumer-010", 3, jobsKey)
	if r[jobsKey].CauseEventID != 8 {
		t.Errorf("consumer-010 cause = %d, want the one received last", r[jobsKey].CauseEventID)
	}
	f.exec(`DELETE FROM inventory`)
	r = f.fires("consumer-013", 3, jobsKey)
	if r[jobsKey].CauseEventID != 8 {
		t.Errorf("consumer-013 cause = %d, want the one received last", r[jobsKey].CauseEventID)
	}
}

// A replica rebuilt at another time reports another creation time while its
// positions match the others': that is info. Positions that differ in this
// and the previous epoch as well make it a warning.
func TestConsumerCreatedDiffersSeverityFollowsPositions(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	old, rebuilt := t0.Add(-48*time.Hour), t0.Add(-time.Hour)
	for _, c := range []string{"same", "apart"} {
		f.consumerConfig("S0", "jobs", c, 1, 2, m{"created": old})
		f.consumerConfig("S1", "jobs", c, 1, 2, m{"created": old})
		f.consumerConfig("S2", "jobs", c, 1, 2, m{"created": rebuilt})
	}
	for e := int64(1); e <= 2; e++ {
		for i, id := range []string{"S0", "S1", "S2"} {
			f.consumerStats(e, id, "jobs", "same", m{"is_leader": i == 0, "created": old,
				"delivered_stream_seq": 100, "ack_floor_stream_seq": 90})
			f.consumerStats(e, id, "jobs", "apart", m{"is_leader": i == 0, "created": old,
				"delivered_stream_seq": 1000 - 400*int64(i/2), "ack_floor_stream_seq": 90})
		}
	}
	r := f.fires("consumer-009", 2, "APP/jobs/same", "APP/jobs/apart")
	want(t, r["APP/jobs/same"], "info", "nats-0, nats-1 2026-10-15T09:00:00Z; nats-2 2026-10-17T08:00:00Z",
		"positions agree")
	want(t, r["APP/jobs/apart"], "warning", "positions differ", "nats-2 delivered 600")
	// In the first epoch there is no earlier epoch to confirm the difference.
	r = f.fires("consumer-009", 1, "APP/jobs/same", "APP/jobs/apart")
	want(t, r["APP/jobs/apart"], "info")
}

// A connect collapsed into a client.churn summary still counts as a
// connect: three drops and a summary of three connects are three reconnects.
func TestClientReconnectingCountsCollapsedConnects(t *testing.T) {
	f := newFx(t)
	f.cluster(40)
	for i := range 3 {
		f.event(int64(i+1), at(30).Add(time.Duration(i)*time.Minute), "client.disconnect", m{"source": "sys",
			"client_name": "media-worker", "server_name": "nats-0", "reason": "Stale Connection"})
	}
	f.exec(`INSERT INTO events (id, time, received_at, source, kind, client_name, summary, raw)
VALUES (9, ?, ?, 'sys', 'client.churn', 'media-worker', 'churn', '{"connects": 3}')`, at(31), at(31))
	r := f.fires("client-004", 40, "media-worker")
	want(t, r["media-worker"], "warning", "3 reconnects")
}

// client-001 keys by service, the connection name before "@", so a mute
// covers every pod of it, and needs two epochs: a new pod or a laptop CLI
// that is slow once does not fire.
func TestClientRTTByServiceOverTwoEpochs(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	conn := func(epoch int64, id string, cid int64, name string, rtt int64) {
		f.row("connections", m{"epoch": epoch, "server_id": id, "cid": cid, "name": name, "rtt_ns": rtt})
	}
	for _, e := range []int64{99, 100} {
		conn(e, "S0", 1, "api-gateway@api-gateway-5f7d9c4b8e-k2m7p", 250e6)
	}
	conn(100, "S1", 2, "api-gateway@api-gateway-00012-ab1", 300e6)
	conn(99, "S1", 3, "ledger-service@ledger-service-00034-cd2", 1e6)
	conn(100, "S1", 3, "ledger-service@ledger-service-00034-cd2", 400e6)
	r := f.fires("client-001", 100, "api-gateway")
	want(t, r["api-gateway"], "warning", "2 of 2 connections above 100ms", "nats-1 cid 2")
}

// Tooling connections (nats-lens's own, and clients from a loopback
// address such as a port-forwarded CLI) are left out of the client checks
// unless checks.include_tooling is set.
func TestClientChecksLeaveOutTooling(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	for _, e := range []int64{99, 100} {
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": 1, "name": "natslens-events", "rtt_ns": 300e6, "ip": "10.0.0.1"})
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": 2, "name": "NATS CLI Version 0.5.0", "rtt_ns": 300e6, "ip": "127.0.0.1"})
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": 3, "name": "api-gateway@a", "rtt_ns": 300e6, "ip": "10.0.0.3"})
	}
	run := func(include bool) map[string]store.FindingRow {
		set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second), IncludeTooling: include})
		if err != nil {
			t.Fatal(err)
		}
		set.SetToolingNames([]string{"natslens", "natslens-events", "natslens-APP"})
		e := &Engine{Set: set, only: map[string]bool{"client-001": true}}
		runs, err := e.Evaluate(context.Background(), f.st.DB(), 100)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]store.FindingRow{}
		for _, r := range runs {
			if r.Code == "client-001" {
				if r.Status != store.RunOK {
					t.Fatalf("client-001: %s", r.Error)
				}
				for _, row := range r.Rows {
					out[row.EntityKey] = row
				}
			}
		}
		return out
	}
	if got := run(false); len(got) != 1 || got["api-gateway"].EntityKey == "" {
		t.Errorf("without tooling: %v", rowsText(got))
	}
	if got := run(true); len(got) != 3 {
		t.Errorf("with tooling: %v", rowsText(got))
	}
}

// A client without a name is not tooling: its rows stay in the client
// checks, grouped as (unnamed), for connections and for events.
func TestUnnamedClientsAreNotTooling(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	for _, e := range []int64{99, 100} {
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": 1, "rtt_ns": 300e6, "ip": "10.0.0.1"})
	}
	r := f.fires("client-001", 100, "(unnamed)")
	want(t, r["(unnamed)"], "warning", "1 of 1 connection above 100ms")
	g := newFx(t)
	g.cluster(40)
	for i := range 3 {
		g.event(int64(i+1), at(30).Add(time.Duration(i)*time.Minute), "client.disconnect", m{"source": "sys",
			"server_name": "nats-0", "reason": "Stale Connection", "client_host": "10.0.0.9"})
		g.event(int64(i+11), at(30).Add(time.Duration(i)*time.Minute), "client.connect", m{"source": "sys",
			"server_name": "nats-0", "client_host": "10.0.0.9"})
	}
	r = g.fires("client-004", 40, "(unnamed)")
	want(t, r["(unnamed)"], "warning", "3 reconnects")
}

// Two pods of a service each slow once, in different epochs, do not make
// the two epochs: one connection must be slow in all of them.
func TestClientRTTNeedsOneConnectionSlowTwice(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	f.row("connections", m{"epoch": 99, "server_id": "S0", "cid": 1, "name": "svc@a", "rtt_ns": 300e6, "ip": "10.0.0.1"})
	f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": 1, "name": "svc@a", "rtt_ns": 1e6, "ip": "10.0.0.1"})
	f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": 2, "name": "svc@b", "rtt_ns": 300e6, "ip": "10.0.0.2"})
	f.fires("client-001", 100)
}

// evalTooling runs one check with the tooling settings given.
func evalTooling(t *testing.T, f *fx, code string, epoch int64, cfg config.Checks) map[string]store.FindingRow {
	t.Helper()
	cfg.Timeout = config.Duration(10 * time.Second)
	set, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	set.SetToolingNames([]string{"natslens", "natslens-events"})
	e := &Engine{Set: set, only: map[string]bool{code: true}}
	runs, err := e.Evaluate(context.Background(), f.st.DB(), epoch)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.FindingRow{}
	for _, r := range runs {
		if r.Code == code {
			for _, row := range r.Rows {
				out[row.EntityKey] = row
			}
		}
	}
	return out
}

// Loopback as tooling is a setting, and it is dropped for an epoch in
// which loopback connections are more than half of the connections that
// are not nats-lens's own: a sidecar that makes all inbound traffic look
// local would otherwise hide every client.
func TestLoopbackToolingSettingAndGuard(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	conn := func(cid int64, name, ip string) {
		for _, e := range []int64{99, 100} {
			f.row("connections", m{"epoch": e, "server_id": "S0", "cid": cid, "name": name, "rtt_ns": 300e6, "ip": ip})
		}
	}
	conn(1, "natslens", "127.0.0.1")
	conn(2, "nats-cli", "127.0.0.1")
	conn(3, "svc-a@1", "10.0.0.3")
	conn(4, "svc-b@1", "10.0.0.4")
	if got := evalTooling(t, f, "client-001", 100, config.Checks{}); len(got) != 2 {
		t.Errorf("loopback CLI should be tooling: %s", rowsText(got))
	}
	off := false
	if got := evalTooling(t, f, "client-001", 100, config.Checks{LoopbackIsTooling: &off}); len(got) != 3 {
		t.Errorf("loopback_is_tooling false: %s", rowsText(got))
	}
	// Four of six non-own connections loopback (67%, above 60%): loopback
	// no longer counts as tooling, in either epoch once each epoch's
	// decision is stored as the engine stores it.
	g := newFx(t)
	g.cluster(100)
	for _, e := range []int64{99, 100} {
		for cid, c := range map[int64][2]string{1: {"natslens", "127.0.0.1"}, 2: {"nats-cli", "127.0.0.1"}, 3: {"svc-a@1", "10.0.0.3"},
			4: {"svc-b@1", "10.0.0.4"}, 5: {"svc-c@1", "127.0.0.1"}, 6: {"svc-d@1", "::1"}, 7: {"svc-e@1", "127.0.0.1"}} {
			g.row("connections", m{"epoch": e, "server_id": "S0", "cid": cid, "name": c[0], "rtt_ns": 300e6, "ip": c[1]})
		}
	}
	decideUpTo(t, g, 99)
	if got := evalTooling(t, g, "client-001", 100, config.Checks{}); len(got) != 6 {
		t.Errorf("guard: %s", rowsText(got))
	}
}

// decideUpTo stores the loopback decision of every epoch up to last, as
// the engine does after each epoch's checks.
func decideUpTo(t *testing.T, f *fx, last int64) {
	t.Helper()
	ctx := context.Background()
	for ep := int64(1); ep <= last; ep++ {
		on, stored, err := LoopbackDecision(ctx, f.st.DB(), ep, []string{"natslens", "natslens-events"})
		if err != nil {
			t.Fatal(err)
		}
		if !stored {
			if err := f.st.SetLoopbackTooling(ctx, ep, on); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Around the line the guard does not flip from epoch to epoch: the share
// is taken over ten epochs with hysteresis, so a slow loopback CLI stays
// tooling in both epochs, and the client-001 result is the same.
func TestLoopbackGuardDoesNotFlap(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	add := func(e, cid int64, name, ip string) {
		f.row("connections", m{"epoch": e, "server_id": "S0", "cid": cid, "name": name, "rtt_ns": 300e6, "ip": ip})
	}
	for _, e := range []int64{98, 99, 100} {
		add(e, 1, "cli", "127.0.0.1")
		add(e, 2, "svc-a@1", "10.0.0.3")
	}
	add(100, 3, "svc-b@1", "10.0.0.4") // epoch 100: 1 of 3 loopback
	add(99, 4, "x@1", "127.0.0.1")     // epoch 99: 2 of 3 loopback
	r99 := evalTooling(t, f, "client-001", 99, config.Checks{})
	r100 := evalTooling(t, f, "client-001", 100, config.Checks{})
	if rowsText(r99) != rowsText(r100) || len(r99) != 1 || r99["svc-a"].EntityKey == "" {
		t.Errorf("99: %s | 100: %s", rowsText(r99), rowsText(r100))
	}
}

// On a busy consumer a follower trails the leader by a few messages in
// every epoch; within the tolerance (100, or 1% of the leader's position)
// that is agreement, current or not. A replica far behind in both epochs
// disagrees even when the leader lists it as not current.
func TestConsumerCreatedDiffersBusyFollowerStaysInfo(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	old, rebuilt := t0.Add(-48*time.Hour), t0.Add(-time.Hour)
	for _, c := range []string{"busy", "catching"} {
		f.consumerConfig("S0", "jobs", c, 1, 2, m{"created": old})
		f.consumerConfig("S1", "jobs", c, 1, 2, m{"created": old})
		f.consumerConfig("S2", "jobs", c, 1, 2, m{"created": rebuilt})
	}
	for e := int64(1); e <= 2; e++ {
		base := 50000 * e
		for i, id := range []string{"S0", "S1", "S2"} {
			f.consumerStats(e, id, "jobs", "busy", m{"is_leader": i == 0, "delivered_stream_seq": base - 20*int64(i),
				"ack_floor_stream_seq": base - 30 - 20*int64(i)})
			f.consumerStats(e, id, "jobs", "catching", m{"is_leader": i == 0, "delivered_stream_seq": base - 30000*int64(i/2),
				"ack_floor_stream_seq": base - 30000*int64(i/2)})
		}
		for _, c := range []string{"busy", "catching"} {
			f.consumerReplica(e, "S0", "jobs", c, "nats-1", nil)
			f.consumerReplica(e, "S0", "jobs", c, "nats-2", m{"current": c == "busy", "lag": 30000})
		}
	}
	r := f.fires("consumer-009", 2, "APP/jobs/busy", "APP/jobs/catching")
	want(t, r["APP/jobs/busy"], "info", "positions agree")
	want(t, r["APP/jobs/catching"], "warning", "positions differ")
}

// A recreated consumer whose stale replica kept the old copy, far ahead of
// the new leader, disagrees although the leader lists it as not current:
// a follower is never legitimately ahead.
func TestConsumerCreatedDiffersStaleReplicaAhead(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	old, rebuilt := t0.Add(-48*time.Hour), t0.Add(-time.Hour)
	f.consumerConfig("S0", "jobs", "c", 1, 2, m{"created": rebuilt})
	f.consumerConfig("S1", "jobs", "c", 1, 2, m{"created": rebuilt})
	f.consumerConfig("S2", "jobs", "c", 1, 2, m{"created": old})
	for e := int64(1); e <= 2; e++ {
		for i, id := range []string{"S0", "S1", "S2"} {
			d := int64(50)
			if i == 2 {
				d = 900000
			}
			f.consumerStats(e, id, "jobs", "c", m{"is_leader": i == 0, "delivered_stream_seq": d, "ack_floor_stream_seq": d})
		}
		f.consumerReplica(e, "S0", "jobs", "c", "nats-1", nil)
		f.consumerReplica(e, "S0", "jobs", "c", "nats-2", m{"current": false, "lag": 899950})
	}
	r := f.fires("consumer-009", 2, "APP/jobs/c")
	want(t, r["APP/jobs/c"], "warning", "positions differ", "nats-2 delivered 900000")
}

// A pod that reconnects every epoch (a new cid each time) and stays slow
// is the same client: the rule follows server and pod, the part after "@".
func TestClientRTTFollowsThePodAcrossReconnects(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	f.row("connections", m{"epoch": 99, "server_id": "S0", "cid": 1, "name": "svc@a", "rtt_ns": 300e6, "ip": "10.0.0.1"})
	f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": 2, "name": "svc@a", "rtt_ns": 300e6, "ip": "10.0.0.1"})
	f.fires("client-001", 100, "svc")
}

// A name with "@" but nothing after it says nothing about the pod: two
// connections from different addresses, each slow in one epoch, are two
// clients. A pod on two servers is two clients as well.
func TestClientRTTSharedNameIsNotOneClient(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	f.row("connections", m{"epoch": 99, "server_id": "S0", "cid": 1, "name": "svc@", "rtt_ns": 300e6, "ip": "10.0.0.1"})
	f.row("connections", m{"epoch": 99, "server_id": "S0", "cid": 2, "name": "svc@", "rtt_ns": 1e6, "ip": "10.0.0.2"})
	f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": 1, "name": "svc@", "rtt_ns": 1e6, "ip": "10.0.0.1"})
	f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": 2, "name": "svc@", "rtt_ns": 300e6, "ip": "10.0.0.2"})
	f.fires("client-001", 100)
	g := newFx(t)
	g.cluster(100)
	g.row("connections", m{"epoch": 99, "server_id": "S0", "cid": 1, "name": "svc@a", "rtt_ns": 300e6, "ip": "10.0.0.1"})
	g.row("connections", m{"epoch": 100, "server_id": "S1", "cid": 1, "name": "svc@a", "rtt_ns": 300e6, "ip": "10.0.0.1"})
	g.fires("client-001", 100)
}

// Replies gathered a moment apart can put a busy follower a little ahead
// of the leader; the ahead tolerance scales with the position (0.1%), so
// 40 ahead of 10,000,000 agrees.
func TestConsumerCreatedDiffersFollowerAheadBySkew(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.consumerConfig("S0", "jobs", "c", 1, 2, m{"created": t0.Add(-48 * time.Hour)})
	f.consumerConfig("S1", "jobs", "c", 1, 2, m{"created": t0.Add(-time.Hour)})
	for e := int64(1); e <= 2; e++ {
		base := 5_000_000 * e
		f.consumerStats(e, "S0", "jobs", "c", m{"is_leader": true, "delivered_stream_seq": base, "ack_floor_stream_seq": base})
		f.consumerStats(e, "S1", "jobs", "c", m{"delivered_stream_seq": base + 40, "ack_floor_stream_seq": base + 40})
		f.consumerReplica(e, "S0", "jobs", "c", "nats-1", nil)
	}
	r := f.fires("consumer-009", 2, "APP/jobs/c")
	want(t, r["APP/jobs/c"], "info", "positions agree")
}

// A follower behind by more than the tolerance in two epochs warns even
// while it catches up, when the creation times differ: by design, as a
// replica built at another time and far behind may be the stale one.
func TestConsumerCreatedDiffersFollowerFarBehindWarns(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.consumerConfig("S0", "jobs", "c", 1, 2, m{"created": t0.Add(-48 * time.Hour)})
	f.consumerConfig("S1", "jobs", "c", 1, 2, m{"created": t0.Add(-time.Hour)})
	for e := int64(1); e <= 2; e++ {
		base := 5_000_000 * e
		f.consumerStats(e, "S0", "jobs", "c", m{"is_leader": true, "delivered_stream_seq": base, "ack_floor_stream_seq": base})
		f.consumerStats(e, "S1", "jobs", "c", m{"delivered_stream_seq": base * 97 / 100, "ack_floor_stream_seq": base * 97 / 100})
		f.consumerReplica(e, "S0", "jobs", "c", "nats-1", m{"current": true, "lag": 10})
	}
	r := f.fires("consumer-009", 2, "APP/jobs/c")
	want(t, r["APP/jobs/c"], "warning", "positions differ")
}

// Each epoch's rows are judged by that epoch's own loopback decision:
// in epoch 99 the rule holds (the CLI is tooling), in epoch 100 loopback
// clients are most of the connections and it does not. A loopback CLI slow
// in both epochs is tooling in one of them, so client-001 does not fire.
func TestLoopbackJudgedPerEpoch(t *testing.T) {
	f := newFx(t)
	f.cluster(100)
	cid := int64(10)
	add := func(e int64, loop bool, n int) {
		for range n {
			cid++
			ip := "10.0.0.9"
			if loop {
				ip = "127.0.0.1"
			}
			f.row("connections", m{"epoch": e, "server_id": "S0", "cid": cid, "name": fmt.Sprintf("x@%d", cid), "rtt_ns": 1e6, "ip": ip})
		}
	}
	for e := int64(91); e <= 99; e++ {
		add(e, true, 2)
		add(e, false, 8)
	}
	add(100, true, 100)
	f.row("connections", m{"epoch": 99, "server_id": "S0", "cid": 1, "name": "cli", "rtt_ns": 300e6, "ip": "127.0.0.1"})
	f.row("connections", m{"epoch": 100, "server_id": "S0", "cid": 1, "name": "cli", "rtt_ns": 300e6, "ip": "127.0.0.1"})
	f.fires("client-001", 100)
}

// The decision is stored with each epoch the engine runs for, from the
// previous epoch's stored decision and the trailing share, so asking about
// an epoch later gives the answer it had.
func TestLoopbackDecisionStoredPerEpoch(t *testing.T) {
	f := newFx(t)
	f.cluster(30)
	for e := int64(1); e <= 30; e++ {
		loop := int64(7)
		if e > 10 {
			loop = 5 // 50%: hysteresis keeps the rule off
		}
		for i := int64(0); i < 10; i++ {
			ip := "10.0.0.9"
			if i < loop {
				ip = "127.0.0.1"
			}
			f.row("connections", m{"epoch": e, "server_id": "S0", "cid": i + 1, "name": fmt.Sprintf("x@%d", i), "rtt_ns": 1e6, "ip": ip})
		}
	}
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Set: set, only: map[string]bool{"client-001": true}}
	for ep := int64(1); ep <= 30; ep++ {
		if _, err := e.Run(context.Background(), f.st, ep); err != nil {
			t.Fatal(err)
		}
	}
	var on, off, unset int
	if err := f.st.DB().QueryRow(`SELECT count(*) FILTER (WHERE loopback_tooling), count(*) FILTER (WHERE NOT loopback_tooling),
  count(*) FILTER (WHERE loopback_tooling IS NULL) FROM epochs`).Scan(&on, &off, &unset); err != nil {
		t.Fatal(err)
	}
	if on != 0 || off != 30 || unset != 0 {
		t.Fatalf("decisions on %d, off %d, unset %d; want all 30 off (70%%, then 50%% held by hysteresis)", on, off, unset)
	}
	got, err := StoredLoopback(context.Background(), f.st.DB(), 20)
	if err != nil || got {
		t.Errorf("stored decision of epoch 20 = %v, %v; want off", got, err)
	}
	if got, err := StoredLoopback(context.Background(), f.st.DB(), 999); err != nil || !got {
		t.Errorf("an epoch without a decision reads as on: %v, %v", got, err)
	}
}
