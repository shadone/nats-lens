package store

import (
	"context"
	"strings"
	"testing"
)

// Only a consumer's leader tracks num_pending, num_waiting and push_bound;
// followers report zero or false, which reads as a real value. Follower
// rows store NULL instead.
func TestFollowerConsumerCountersAreNull(t *testing.T) {
	s := openMem(t)
	if _, err := s.WriteSnapshot(context.Background(), fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	// resize-worker is led by nats-1; nats-0's row is a follower's.
	if n := count(t, db, `SELECT count(*) FROM consumer_stats JOIN servers USING (server_id)
WHERE consumer = 'resize-worker' AND server_name = 'nats-0'
  AND NOT is_leader AND num_pending IS NULL AND num_waiting IS NULL AND push_bound IS NULL`); n != 1 {
		t.Errorf("follower row should have NULL leader-only counters, matched %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM consumer_stats JOIN servers USING (server_id)
WHERE consumer = 'resize-worker' AND server_name = 'nats-1'
  AND is_leader AND num_pending = 0 AND num_waiting = 2 AND push_bound IS NOT NULL`); n != 1 {
		t.Errorf("leader row should keep its counters, matched %d", n)
	}
	// Replicated state stays on every row.
	if n := count(t, db, `SELECT count(*) FROM consumer_stats WHERE consumer = 'resize-worker' AND num_ack_pending IS NOT NULL`); n != 2 {
		t.Errorf("num_ack_pending rows = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM consumer_leader_stats WHERE consumer = 'resize-worker'`); n != 1 {
		t.Errorf("consumer_leader_stats rows = %d", n)
	}
}

// Old unclustered rows have is_leader NULL; the view's comment must say
// that they are not in it.
func TestLeaderViewCommentNamesOldNullRows(t *testing.T) {
	s := openMem(t)
	var comment string
	if err := s.DB().QueryRow(`SELECT comment FROM duckdb_views() WHERE view_name = 'consumer_leader_stats'`).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(comment, "is_leader NULL") {
		t.Fatalf("view comment does not mention old NULL rows: %s", comment)
	}
}
