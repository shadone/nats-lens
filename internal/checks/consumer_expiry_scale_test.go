package checks

import (
	"fmt"
	"testing"
	"time"
)

// consumer-014 on 14 days of collections 2 minutes apart with 2000
// filtered consumers whose backlog has waited since the store began, on a
// stream with max_age 1h, beside one consumer on a stream with max_age
// 30d. Each stream's history is read back to its own max_age only, so the
// check stays far below checks.timeout; 2 seconds leaves room for a loaded
// machine.
func TestOldestUnreadScale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 20 million rows")
	}
	const n, k = 14 * 24 * 30, 2000
	f := newFx(t)
	f.exec(fmt.Sprintf(`INSERT INTO epochs SELECT e, TIMESTAMP '2026-10-03 09:00:00' + to_seconds(120*(e-1)),
	  TIMESTAMP '2026-10-03 09:00:00' + to_seconds(120*(e-1)), 100, 3, 3, ['nats-0','nats-1','nats-2'],
	  ['nats-0','nats-1','nats-2'], []::VARCHAR[], 0, false, [], NULL FROM range(1, %d) t(e)`, n+1))
	f.streamConfig("S0", "events", 1, n, m{"max_age_ns": int64(time.Hour)})
	f.streamConfig("S0", "archive", 1, n, m{"max_age_ns": int64(30 * 24 * time.Hour)})
	f.exec(`UPDATE stream_config SET subjects = ['events.>'] WHERE stream = 'events'`)
	f.exec(fmt.Sprintf(`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader, last_seq, first_seq, first_ts, created)
	  SELECT e, 'S0', 'APP', s, true, 10*e, 1, TIMESTAMP '2026-10-03 09:00:00', TIMESTAMP '2026-10-01 09:00:00'
	  FROM range(1, %d) t(e), (VALUES ('events'), ('archive')) v(s)`, n+1))
	f.exec(fmt.Sprintf(`INSERT INTO consumer_config (server_id, account, stream, consumer, first_epoch, last_epoch, config_hash,
	  durable_name, filter_subjects, max_ack_pending, max_waiting, replicas)
	  SELECT 'S0', 'APP', 'events', 'c' || i, 1, %d, 'h', 'c' || i, ['events.eu'], 1000, 512, 0 FROM range(0, %d) t(i)`, n, k))
	f.consumerConfig("S0", "archive", "reader", 1, n, nil)
	f.exec(fmt.Sprintf(`INSERT INTO consumer_stats (epoch, server_id, account, stream, consumer, is_leader, created,
	  delivered_stream_seq, ack_floor_stream_seq, num_ack_pending, num_pending)
	  SELECT e, 'S0', 'APP', 'events', 'c' || i, true, TIMESTAMP '2026-10-02 09:00:00', 1, 1, 0, 10*e - 1
	  FROM range(1, %d) t(e), range(0, %d) u(i)`, n+1, k))
	f.exec(fmt.Sprintf(`INSERT INTO consumer_stats (epoch, server_id, account, stream, consumer, is_leader, created,
	  delivered_stream_seq, ack_floor_stream_seq, num_ack_pending, num_pending)
	  SELECT e, 'S0', 'APP', 'archive', 'reader', true, TIMESTAMP '2026-10-02 09:00:00', 10*e, 10*e, 0, 0
	  FROM range(1, %d) t(e)`, n+1))
	start := time.Now()
	got := f.eval("consumer-014", n)
	took := time.Since(start)
	t.Logf("%d filtered backlogged consumers, %d collections: %d findings in %s", k, n, len(got), took)
	if len(got) != k {
		t.Errorf("%d findings, want %d", len(got), k)
	}
	if took > 2*time.Second {
		t.Errorf("consumer-014 took %s, want well under 1s on an idle machine", took)
	}
}
