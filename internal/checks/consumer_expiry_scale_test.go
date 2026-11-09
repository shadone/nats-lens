package checks

import (
	"fmt"
	"testing"
	"time"
)

// consumer-014 on 14 days of collections 30 seconds apart with 2000
// filtered consumers whose backlog has waited since before the store
// began, on a stream with max_age 30d, so the whole history lies in the
// stream's window. The backlog's start is found without reading each
// consumer's whole history.
//
// The check's time is compared with one pass over the same history in the
// same store (see scaleRatio), not with a fixed limit, which failed on
// loaded machines. The bounded search takes a median of 2.8 to 4.1
// passes, alone or with two other runs of this test at once; reading each
// consumer's whole history at the start search takes 14.9, and joining it
// before the refinement's epoch bound 25 to 28.
func TestOldestUnreadScale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 80 million rows")
	}
	const n, k = 14 * 24 * 120, 2000
	f := newFx(t)
	f.exec(fmt.Sprintf(`INSERT INTO epochs SELECT e, TIMESTAMP '2026-10-03 09:00:00' + to_seconds(30*(e-1)),
	  TIMESTAMP '2026-10-03 09:00:00' + to_seconds(30*(e-1)), 100, 3, 3, ['nats-0','nats-1','nats-2'],
	  ['nats-0','nats-1','nats-2'], []::VARCHAR[], 0, false, [], NULL FROM range(1, %d) t(e)`, n+1))
	f.streamConfig("S0", "events", 1, n, m{"max_age_ns": int64(30 * 24 * time.Hour)})
	f.exec(`UPDATE stream_config SET subjects = ['events.>'] WHERE stream = 'events'`)
	f.exec(fmt.Sprintf(`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader, last_seq, first_seq, first_ts, created)
	  SELECT e, 'S0', 'APP', 'events', true, 10*e, 1, TIMESTAMP '2026-10-03 09:00:00', TIMESTAMP '2026-10-01 09:00:00'
	  FROM range(1, %d) t(e)`, n+1))
	f.exec(fmt.Sprintf(`INSERT INTO consumer_config (server_id, account, stream, consumer, first_epoch, last_epoch, config_hash,
	  durable_name, filter_subjects, max_ack_pending, max_waiting, replicas)
	  SELECT 'S0', 'APP', 'events', 'c' || i, 1, %d, 'h', 'c' || i, ['events.eu'], 1000, 512, 0 FROM range(0, %d) t(i)`, n, k))
	f.exec(fmt.Sprintf(`INSERT INTO consumer_stats (epoch, server_id, account, stream, consumer, is_leader, created,
	  delivered_stream_seq, ack_floor_stream_seq, num_ack_pending, num_pending)
	  SELECT e, 'S0', 'APP', 'events', 'c' || i, true, TIMESTAMP '2026-10-02 09:00:00', 1, 1, 0, 10*e - 1
	  FROM range(1, %d) t(e), range(0, %d) u(i)`, n+1, k))

	// Lowered thresholds, so that a 14-day backlog on a 30-day stream fires.
	over := map[string]string{"warn_ratio": "0.1", "resolve_ratio": "0.05"}
	// One pass: each consumer's earliest collection with messages waiting
	// at its delivered sequence, what the check's search finds without
	// reading every row.
	f.scaleRatio(fmt.Sprintf("consumer-014 on %d filtered backlogged consumers, %d collections, max_age 30d", k, n),
		`SELECT count(*) FROM (SELECT consumer, min(epoch) FILTER (WHERE num_pending > 0 AND delivered_stream_seq = 1)
		  FROM consumer_leader_stats GROUP BY consumer)`, k,
		func() {
			if got := f.eval("consumer-014", n, over); len(got) != k {
				t.Fatalf("%d findings, want %d", len(got), k)
			}
		})
}
